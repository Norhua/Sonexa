<script setup lang="ts">
/**
 * 详情抽屉。宽度 340px。
 *
 * 纵向顺序：封面 → 歌名 → 艺术家 → 参与者 → 发行信息 → 厂牌与目录号
 * → 标签 chips → 编辑按钮。
 *
 * 必须优雅处理丰富信息为空的情况（「无需匹配」入库的曲目）：
 * 缺失字段显示占位短横线，不报错、不出现空白区域。
 */
import { computed, ref, watch } from 'vue'
import { X, Pencil, ImagePlus } from 'lucide-vue-next'
import { App, display, duration, DASH } from '@/api'
import CoverArt from '@/components/CoverArt.vue'
import { creditParts } from '@/lib/creditParts'
import Button from '@/volt/Button.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import Tag from '@/volt/Tag.vue'
import type { Models } from '@/api'

const props = defineProps<{
  detail: Models.app.TrackDetail | null
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
const coverCandidates = ref<Models.app.CoverCandidate[]>([])
const coverLoading = ref(false)
const coverError = ref('')

let coverRequest = 0
watch(() => props.open, open => {
  if (!open) {
    coverRequest++
    coverPickerOpen.value = false
    coverCandidates.value = []
    coverLoading.value = false
    coverError.value = ''
  }
})

/**
 * 向**所有**上游查询封面，结果按置信度排序。
 *
 * 某首来自 MusicBrainz 的曲目没有封面时，iTunes 的结果也应出现在候选中——
 * 自动获取跟随来源，手动重选则跨源。
 */
async function openCoverPicker() {
  if (!props.detail) return
  const request = ++coverRequest
  coverPickerOpen.value = true
  coverLoading.value = true
  coverError.value = ''
  try {
    const t = props.detail.track
    const candidates = await App.SearchCovers(t.title, t.artist ?? '', t.album ?? '')
    if (request === coverRequest) coverCandidates.value = candidates
  } catch (e) {
    if (request === coverRequest) coverError.value = String(e)
  } finally {
    if (request === coverRequest) coverLoading.value = false
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
const fallbackCredit = computed(() => creditParts(
  props.detail?.creditText || props.detail?.track.artist || '',
  props.detail?.creditSpans ?? [],
))

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
    class="flex w-[340px] shrink-0 flex-col border-l bg-app-chrome"
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
          class="aspect-square w-full max-w-[280px] rounded-md shadow"
        />
      </div>
      <!-- 歌名与艺术家，重新获取封面的图标放在文字块右侧 -->
      <div class="flex items-center gap-3">
        <div class="min-w-0 flex-1 break-words">
          <h2 class="text-lg font-semibold leading-tight">{{ detail.track.title }}</h2>
          <p class="mt-1 text-sm text-muted-color">{{ display(detail.track.artist) }}</p>
        </div>
        <SecondaryButton variant="text" rounded size="small" class="shrink-0"
          title="重新获取封面" aria-label="重新获取封面" @click="openCoverPicker">
          <ImagePlus class="size-4" aria-hidden="true" />
        </SecondaryButton>
      </div>

      <!-- 参与者 -->
      <section>
        <h3 class="mb-2 text-xs font-medium text-muted-color">参与者</h3>
        <dl class="detail-fields text-sm">
          <!-- 全部参与角色统一分组，不把署名另列为演唱 -->
          <div v-for="group in detail.participants" :key="group.role" class="contents">
            <dt class="text-xs text-muted-color">{{ group.label }}</dt>
            <dd class="min-w-0 break-words">
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

          <template v-if="detail.participants.length === 0">
            <!-- 署名说明谁被署名，不推断演唱。仅在无角色时兜底，避免完整列表重复。 -->
            <div v-if="fallbackCredit.length" class="contents">
              <dt class="text-xs text-muted-color">署名</dt>
              <dd class="min-w-0 break-words">
                <template v-for="(part, i) in fallbackCredit" :key="i">
                  <button v-if="part.artistId"
                    class="underline decoration-dotted underline-offset-2 hover:decoration-solid"
                    @click="emit('viewArtist', part.artistId)"
                  >{{ part.text }}</button><span v-else>{{ part.text }}</span>
                </template>
              </dd>
            </div>
          </template>
        </dl>
        <p v-if="detail.participants.length === 0" class="mt-1.5 text-xs text-muted-color">暂无参与角色信息</p>
      </section>

      <!-- 发行信息 -->
      <section>
        <h3 class="mb-2 text-xs font-medium text-muted-color">发行信息</h3>
        <dl class="detail-fields text-sm">
          <div v-for="item in releaseInfo" :key="item.label" class="contents">
            <dt class="text-xs text-muted-color">{{ item.label }}</dt>
            <dd class="min-w-0 break-words">{{ item.value }}</dd>
          </div>
        </dl>
      </section>

      <!-- 厂牌与目录号 -->
      <section>
        <h3 class="mb-2 text-xs font-medium text-muted-color">厂牌与目录号</h3>
        <dl class="detail-fields text-sm">
          <div class="contents">
            <dt class="text-xs text-muted-color">厂牌</dt>
            <dd class="min-w-0 break-words">{{ display(meta?.label) }}</dd>
          </div>
          <div class="contents">
            <dt class="text-xs text-muted-color">目录号</dt>
            <dd class="min-w-0 break-words">{{ display(meta?.catalogNumber) }}</dd>
          </div>
        </dl>
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

<style scoped>
/* 每个小标题独立计算标签宽度，不再用固定宽度强迫所有段落对齐。 */
.detail-fields {
  display: grid;
  grid-template-columns: max-content minmax(0, 1fr);
  align-items: baseline;
  gap: 0.375rem 0.5rem;
}
.detail-fields button { text-align: left; overflow-wrap: anywhere; }
</style>
