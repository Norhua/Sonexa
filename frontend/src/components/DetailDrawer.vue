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
import { computed } from 'vue'
import { X, Pencil } from 'lucide-vue-next'
import { display, duration, coverUrl, DASH } from '@/api'
import type { Models } from '@/api'

const props = defineProps<{
  detail: Models.main.TrackDetail | null
  open: boolean
}>()

const emit = defineEmits<{
  close: []
  edit: [trackId: number]
  viewArtist: [artistId: number]
}>()

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
    class="flex w-[390px] shrink-0 flex-col border-l bg-card"
  >
    <header class="flex shrink-0 items-center justify-between border-b px-4 py-2">
      <span class="text-xs text-muted-foreground">详情</span>
      <button class="rounded p-1 hover:bg-accent" title="关闭" @click="emit('close')">
        <X class="size-4" />
      </button>
    </header>

    <div class="min-h-0 flex-1 space-y-4 overflow-auto p-4">
      <!-- 封面 -->
      <div class="flex justify-center">
        <img
          v-if="detail.coverKey"
          :src="coverUrl(detail.coverKey, 500)"
          class="aspect-square w-56 rounded-md object-cover shadow"
          alt=""
        />
        <div v-else class="flex aspect-square w-56 items-center justify-center rounded-md bg-muted text-muted-foreground">
          无封面
        </div>
      </div>

      <!-- 歌名与艺术家 -->
      <div>
        <h2 class="text-lg font-semibold leading-tight">{{ detail.track.title }}</h2>
        <p class="mt-1 text-sm text-muted-foreground">{{ display(detail.track.artist) }}</p>
      </div>

      <!-- 参与者 -->
      <section>
        <h3 class="mb-2 text-xs font-medium text-muted-foreground">参与者</h3>
        <dl class="space-y-1.5 text-sm">
          <!-- 演唱：原串 + 可点击区间。括号与 CV: 部分不可点击 -->
          <div v-if="detail.creditText" class="flex gap-3">
            <dt class="w-16 shrink-0 text-xs text-muted-foreground">演唱</dt>
            <dd class="flex-1">
              <template v-for="(part, i) in creditParts" :key="i">
                <button
                  v-if="part.artistId"
                  class="text-primary underline-offset-2 hover:underline"
                  @click="emit('viewArtist', part.artistId)"
                >{{ part.text }}</button>
                <span v-else>{{ part.text }}</span>
              </template>
            </dd>
          </div>

          <!-- 其余角色按 role 分组，组内以顿号连接，各自可点击 -->
          <div v-for="group in detail.participants" :key="group.role" class="flex gap-3">
            <dt class="w-16 shrink-0 text-xs text-muted-foreground">{{ group.label }}</dt>
            <dd class="flex-1">
              <template v-for="(artist, i) in group.artists" :key="artist.artistId">
                <span v-if="i > 0">、</span>
                <button
                  class="text-primary underline-offset-2 hover:underline"
                  @click="emit('viewArtist', artist.artistId)"
                >{{ artist.name }}</button>
                <span v-if="artist.attribute" class="text-xs text-muted-foreground">
                  ({{ artist.attribute }})
                </span>
              </template>
            </dd>
          </div>

          <div v-if="!detail.creditText && detail.participants.length === 0" class="text-muted-foreground">
            {{ DASH }}
          </div>
        </dl>
      </section>

      <!-- 发行信息 -->
      <section>
        <h3 class="mb-2 text-xs font-medium text-muted-foreground">发行信息</h3>
        <dl class="grid grid-cols-2 gap-x-3 gap-y-1.5 text-sm">
          <div v-for="item in releaseInfo" :key="item.label" class="flex gap-2">
            <dt class="w-16 shrink-0 text-xs text-muted-foreground">{{ item.label }}</dt>
            <dd class="truncate">{{ item.value }}</dd>
          </div>
        </dl>
      </section>

      <!-- 厂牌与目录号 -->
      <section>
        <h3 class="mb-2 text-xs font-medium text-muted-foreground">厂牌与目录号</h3>
        <dl class="space-y-1.5 text-sm">
          <div class="flex gap-2">
            <dt class="w-16 shrink-0 text-xs text-muted-foreground">厂牌</dt>
            <dd>{{ display(meta?.label) }}</dd>
          </div>
          <div class="flex gap-2">
            <dt class="w-16 shrink-0 text-xs text-muted-foreground">目录号</dt>
            <dd>{{ display(meta?.catalogNumber) }}</dd>
          </div>
        </dl>
      </section>

      <!-- 逐字段来源徽标 -->
      <section v-if="sourceBadges.length > 0">
        <h3 class="mb-2 text-xs font-medium text-muted-foreground">字段来源</h3>
        <div class="flex flex-wrap gap-1">
          <span
            v-for="s in sourceBadges"
            :key="s.key"
            class="rounded border px-1.5 py-0.5 text-[10px] text-muted-foreground"
            :title="`${s.label} 来自 ${sourceLabel(s.source)}`"
          >
            {{ s.label }}·{{ sourceLabel(s.source) }}
          </span>
        </div>
      </section>

      <!-- 标签 chips。歌单不在抽屉显示 -->
      <section>
        <h3 class="mb-2 text-xs font-medium text-muted-foreground">标签</h3>
        <div v-if="detail.tags.length > 0" class="flex flex-wrap gap-1.5">
          <span
            v-for="tag in detail.tags"
            :key="tag.id"
            class="rounded-full bg-secondary px-2 py-0.5 text-xs text-secondary-foreground"
          >
            #{{ tag.name }}
          </span>
        </div>
        <p v-else class="text-sm text-muted-foreground">{{ DASH }}</p>
      </section>
    </div>

    <!-- 底部明确的编辑按钮——右键不应是进入编辑的唯一入口 -->
    <footer class="shrink-0 border-t p-3">
      <button
        class="flex w-full items-center justify-center gap-2 rounded-md bg-primary px-3 py-2 text-sm text-primary-foreground hover:bg-primary/90"
        @click="emit('edit', detail.track.id)"
      >
        <Pencil class="size-4" />
        编辑
      </button>
    </footer>
  </aside>
</template>
