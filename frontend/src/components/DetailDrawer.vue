<script setup lang="ts">
/**
 * 详情抽屉。宽度 360–420px。
 *
 * 纵向顺序：封面 → 歌名 → 艺术家 → 参与者 → 发行信息 → 厂牌与目录号
 * → 标签 chips → 编辑按钮。
 *
 * 必须优雅处理丰富信息为空的情况（「无需匹配」入库的曲目）：
 * 缺失字段显示占位短横线，不报错、不出现空白区域。
 */
import { computed, ref } from 'vue'
import { X, Pencil, ImagePlus } from 'lucide-vue-next'
import { App, display, duration, DASH } from '@/api'
import CoverArt from '@/components/CoverArt.vue'
import Button from '@/volt/Button.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import Tag from '@/volt/Tag.vue'
import type { Models } from '@/api'

const props = defineProps<{
  detail: Models.main.TrackDetail | null
  open: boolean
}>()

const emit = defineEmits<{
  close: []
  edit: [trackId: number]
  viewArtist: [artistId: number]
  coverAdopted: []
}>()

/** 手动重新获取封面的状态。 */
const coverPickerOpen = ref(false)
const coverCandidates = ref<Models.main.CoverCandidate[]>([])
const coverLoading = ref(false)
const coverError = ref('')

/**
 * 向**所有**上游查询封面，结果按置信度排序。
 *
 * 某首来自 MusicBrainz 的曲目没有封面时，iTunes 的结果也应出现在候选中——
 * 自动获取跟随来源，手动重选则跨源。
 */
async function openCoverPicker() {
  if (!props.detail) return
  coverPickerOpen.value = true
  coverLoading.value = true
  coverError.value = ''
  try {
    const t = props.detail.track
    coverCandidates.value = await App.SearchCovers(t.title, t.artist ?? '', t.album ?? '')
  } catch (e) {
    coverError.value = String(e)
  } finally {
    coverLoading.value = false
  }
}

/**
 * 采用某档规格。
 *
 * 构造型来源（iTunes）的档位可能失效，因此把同一结果的其他档位
 * 一并作为退档链交给后端，不可用时自动退到下一档。
 */
async function adopt(provider: string, variant: Models.covers.Variant,
  all: Models.covers.Variant[]) {
  if (!props.detail) return
  coverError.value = ''
  try {
    const fallback = all.filter((v) => v.url !== variant.url).map((v) => v.url)
    await App.AdoptCover(props.detail.track.id, provider, variant.url, fallback)
    coverPickerOpen.value = false
    emit('coverAdopted')
  } catch (e) {
    coverError.value = String(e)
  }
}

const meta = computed(() => props.detail?.metadata ?? null)

/**
 * 把署名原串切成可点击与纯文本两种片段。
 *
 * 演唱区优先渲染原串，按可点击区间生成链接；
 * 对不上 artist 的片段保持纯文本，不猜测、不隐藏。
 */
const creditParts = computed(() => {
  const text = props.detail?.creditText ?? ''
  const spans = [...(props.detail?.creditSpans ?? [])].sort((a, b) => a.start - b.start)
  const parts: { text: string; artistId?: number }[] = []
  let cursor = 0

  for (const span of spans) {
    if (span.start > cursor) {
      parts.push({ text: text.slice(cursor, span.start) })
    }
    parts.push({ text: text.slice(span.start, span.end), artistId: span.artistId })
    cursor = span.end
  }
  if (cursor < text.length) {
    parts.push({ text: text.slice(cursor) })
  }
  return parts
})

/** 逐字段来源：把有值的字段按来源分组显示徽标。 */
const sourceBadges = computed(() => props.detail?.sources ?? [])

/** 来源的中文名；provider id 直接显示。 */
function sourceLabel(src: string): string {
  if (src === 'user') return '用户'
  if (src === 'import') return '导入'
  return src
}

/** 发行信息：缺失字段显示占位短横线。 */
const releaseInfo = computed(() => [
  { label: '类型', value: display(meta.value?.primaryType) },
  { label: '年份', value: display(meta.value?.year) },
  { label: '载体', value: display(meta.value?.mediumFormat) },
  { label: '国家/地区', value: display(meta.value?.country) },
  { label: '音轨', value: display(meta.value?.trackNumber) },
  { label: '碟号', value: display(meta.value?.discNumber) },
  { label: '时长', value: duration(meta.value?.durationMs) },
  { label: '风格', value: display(meta.value?.genre) },
])
</script>

<template>
  <aside
    v-if="open && detail"
    class="flex w-[390px] shrink-0 flex-col border-l bg-app-chrome"
  >
    <header class="flex shrink-0 items-center justify-between border-b px-4 py-2">
      <span class="text-xs text-muted-color">详情</span>
      <SecondaryButton variant="text" rounded size="small" title="关闭" @click="emit('close')">
        <X class="size-4" />
      </SecondaryButton>
    </header>

    <div class="min-h-0 flex-1 space-y-4 overflow-auto p-4">
      <!-- 封面 -->
      <div class="flex justify-center">
        <CoverArt
          :source="{
            provider: detail.coverProvider,
            ref: detail.coverRef,
            rg: detail.coverGroupRef,
          }"
          :adopted-path="detail.adoptedCoverPath"
          icon-class="size-10"
          label-class="text-sm"
          class="aspect-square w-56 rounded-md shadow"
        />
      </div>
      <div class="flex justify-center">
        <SecondaryButton variant="outlined" size="small" @click="openCoverPicker">
          <ImagePlus class="size-3.5" />
          重新获取封面
        </SecondaryButton>
      </div>

      <!-- 歌名与艺术家 -->
      <div>
        <h2 class="text-lg font-semibold leading-tight">{{ detail.track.title }}</h2>
        <p class="mt-1 text-sm text-muted-color">{{ display(detail.track.artist) }}</p>
      </div>

      <!-- 参与者 -->
      <section>
        <h3 class="mb-2 text-xs font-medium text-muted-color">参与者</h3>
        <dl class="space-y-1.5 text-sm">
          <!-- 演唱：原串 + 可点击区间。括号与 CV: 部分不可点击 -->
          <div v-if="detail.creditText" class="flex gap-3">
            <dt class="w-16 shrink-0 text-xs text-muted-color">演唱</dt>
            <dd class="flex-1">
              <template v-for="(part, i) in creditParts" :key="i">
                <button
                  v-if="part.artistId"
                  class="underline decoration-dotted underline-offset-2 hover:decoration-solid"
                  @click="emit('viewArtist', part.artistId)"
                >{{ part.text }}</button>
                <span v-else>{{ part.text }}</span>
              </template>
            </dd>
          </div>

          <!-- 其余角色按 role 分组，组内以顿号连接，各自可点击 -->
          <div v-for="group in detail.participants" :key="group.role" class="flex gap-3">
            <dt class="w-16 shrink-0 text-xs text-muted-color">{{ group.label }}</dt>
            <dd class="flex-1">
              <template v-for="(artist, i) in group.artists" :key="artist.artistId">
                <span v-if="i > 0">、</span>
                <button
                  class="underline decoration-dotted underline-offset-2 hover:decoration-solid"
                  @click="emit('viewArtist', artist.artistId)"
                >{{ artist.name }}</button>
                <span v-if="artist.attribute" class="text-xs text-muted-color">
                  ({{ artist.attribute }})
                </span>
              </template>
            </dd>
          </div>

          <div v-if="!detail.creditText && detail.participants.length === 0" class="text-muted-color">
            {{ DASH }}
          </div>
        </dl>
      </section>

      <!-- 发行信息 -->
      <section>
        <h3 class="mb-2 text-xs font-medium text-muted-color">发行信息</h3>
        <dl class="grid grid-cols-2 gap-x-3 gap-y-1.5 text-sm">
          <div v-for="item in releaseInfo" :key="item.label" class="flex gap-2">
            <dt class="w-16 shrink-0 text-xs text-muted-color">{{ item.label }}</dt>
            <dd class="truncate">{{ item.value }}</dd>
          </div>
        </dl>
      </section>

      <!-- 厂牌与目录号 -->
      <section>
        <h3 class="mb-2 text-xs font-medium text-muted-color">厂牌与目录号</h3>
        <dl class="space-y-1.5 text-sm">
          <div class="flex gap-2">
            <dt class="w-16 shrink-0 text-xs text-muted-color">厂牌</dt>
            <dd>{{ display(meta?.label) }}</dd>
          </div>
          <div class="flex gap-2">
            <dt class="w-16 shrink-0 text-xs text-muted-color">目录号</dt>
            <dd>{{ display(meta?.catalogNumber) }}</dd>
          </div>
        </dl>
      </section>

      <!-- 逐字段来源徽标 -->
      <section v-if="sourceBadges.length > 0">
        <h3 class="mb-2 text-xs font-medium text-muted-color">字段来源</h3>
        <div class="flex flex-wrap gap-1">
          <Tag
            v-for="s in sourceBadges"
            :key="s.key"
            severity="secondary"
            :value="`${s.label}·${sourceLabel(s.source)}`"
            class="px-1.5 py-0.5 text-[10px] font-normal"
            :title="`${s.label} 来自 ${sourceLabel(s.source)}`"
          />
        </div>
      </section>

      <!-- 标签 chips。歌单不在抽屉显示 -->
      <section>
        <h3 class="mb-2 text-xs font-medium text-muted-color">标签</h3>
        <div v-if="detail.tags.length > 0" class="flex flex-wrap gap-1.5">
          <Tag
            v-for="tag in detail.tags"
            :key="tag.id"
            severity="secondary"
            rounded
            :value="`#${tag.name}`"
            class="text-xs font-normal"
          />
        </div>
        <p v-else class="text-sm text-muted-color">{{ DASH }}</p>
      </section>
    </div>

    <!-- 封面候选：跨源结果，同一结果的多规格收拢为一条 -->
    <div v-if="coverPickerOpen" class="shrink-0 border-t p-3">
      <div class="mb-2 flex items-center justify-between">
        <h3 class="text-xs font-medium">选择封面</h3>
        <SecondaryButton variant="text" rounded size="small" @click="coverPickerOpen = false">
          <X class="size-3.5" />
        </SecondaryButton>
      </div>
      <p v-if="coverLoading" class="text-xs text-muted-color">正在向全部上游查询…</p>
      <p v-else-if="coverError" class="text-xs text-danger-color">{{ coverError }}</p>
      <p v-else-if="coverCandidates.length === 0" class="text-xs text-muted-color">
        所有上游都没有返回封面
      </p>
      <div v-else class="max-h-64 space-y-2 overflow-auto">
        <!-- 一条候选 = 一个结果；其可选规格在同一行内标注，不重复成行 -->
        <div v-for="c in coverCandidates" :key="`${c.provider}-${c.sourceRef}`"
          class="rounded border p-2">
          <p class="truncate text-xs">
            <Tag
              severity="secondary"
              :value="c.provider"
              class="px-1 py-0 text-[10px] font-normal"
            />
            {{ c.title }}
          </p>
          <div class="mt-1.5 flex flex-wrap gap-1">
            <SecondaryButton
              v-for="v in c.variants"
              :key="v.url"
              variant="outlined"
              size="small"
              class="text-[10px]"
              :title="c.constructed ? '构造型来源，采用时先验证，不可用自动退档' : v.type"
              @click="adopt(c.provider, v, c.variants)"
            >
              {{ v.label }}
            </SecondaryButton>
          </div>
        </div>
      </div>
    </div>

    <!-- 底部明确的编辑按钮——右键不应是进入编辑的唯一入口 -->
    <footer class="shrink-0 border-t p-3">
      <Button fluid @click="emit('edit', detail.track.id)">
        <Pencil class="size-4" />
        编辑
      </Button>
    </footer>
  </aside>
</template>
