<script setup lang="ts">
/**
 * 全应用唯一的候选结果表格组件。
 *
 * 上游搜索结果与复核队列候选列表共用本组件，列定义完全一致；
 * 动作按上下文不同（加入曲库 / 已在库中 / 接受 / 合并），而列不变。
 */
import { computed, ref } from 'vue'
import { display, duration, trackPosition, coverUrl, DASH } from '@/api'
import type { Models } from '@/api'

/** 取图失败的行——显示占位而不是破图。 */
const brokenCovers = ref(new Set<string>())

/** 封面键的尺寸必须与注册降级链时一致。 */
const coverThumbSize = 250

const props = withDefaults(
  defineProps<{
    rows: Models.main.CandidateRow[]
    /**
     * 当次结果涉及的上游集合的能力并集。
     *
     * 列策略以它为准：该集合中所有上游都不支持某列时隐藏该列；
     * 仅部分支持时显示该列并在不支持的行填占位短横线。
     */
    capabilities: string[]
    /** 各上游各自声明的能力，用于判断某一行是否支持某列。 */
    providerCapabilities?: Record<string, string[]>
    loading?: boolean
    /** 截断显示的条数；0 表示不截断。 */
    limit?: number
    total?: number
  }>(),
  { providerCapabilities: () => ({}), loading: false, limit: 0, total: 0 },
)

const emit = defineEmits<{
  act: [row: Models.main.CandidateRow]
  jumpLocal: [trackId: number]
  showAll: []
}>()

const expanded = defineModel<boolean>('expanded', { default: false })

/** 能力驱动的列显示：集合内全不支持则隐藏该列。 */
function columnVisible(cap: string): boolean {
  return props.capabilities.includes(cap)
}

const showMedium = computed(() => columnVisible('MediumFormat'))
const showCountry = computed(() => columnVisible('Country'))
const showPrimaryType = computed(() => columnVisible('PrimaryType'))
// 厂牌列由 Capabilities 驱动：只搜不支持它的上游时隐藏，部分支持时填占位
const showLabel = computed(() => columnVisible('Label'))
const showCatalogNumber = computed(() => columnVisible('CatalogNumber'))

/** 某一行是否支持某列——不支持时填占位短横线。 */
function rowSupports(row: Models.main.CandidateRow, cap: string): boolean {
  const caps = props.providerCapabilities[row.provider]
  if (!caps) return true
  return caps.includes(cap)
}

function cell(row: Models.main.CandidateRow, cap: string, value: unknown): string {
  if (!rowSupports(row, cap)) return DASH
  return display(value as string | number | null | undefined)
}

const visibleRows = computed(() => {
  if (props.limit > 0 && !expanded.value) return props.rows.slice(0, props.limit)
  return props.rows
})

const truncated = computed(() => props.limit > 0 && !expanded.value && props.rows.length > props.limit)

function onAct(row: Models.main.CandidateRow) {
  // 「已在库中」为禁用态，但点击可跳转到该曲目
  if (row.action === '已在库中' && row.localTrackId) {
    emit('jumpLocal', row.localTrackId)
    return
  }
  emit('act', row)
}
</script>

<template>
  <div class="flex flex-col">
    <div class="overflow-x-auto">
      <table class="w-full border-collapse text-sm">
        <thead>
          <tr class="border-b text-left text-xs text-muted-foreground">
            <th class="w-20 px-2 py-2">来源</th>
            <th class="w-12 px-2 py-2"></th>
            <th class="px-2 py-2">标题</th>
            <th class="px-2 py-2">艺术家</th>
            <th class="px-2 py-2">歌集名</th>
            <th class="w-16 px-2 py-2">时长</th>
            <th class="w-20 px-2 py-2">第几曲</th>
            <th class="w-16 px-2 py-2">年份</th>
            <th v-if="showMedium" class="w-20 px-2 py-2">载体</th>
            <th v-if="showCountry" class="w-20 px-2 py-2">国家/地区</th>
            <th v-if="showLabel" class="px-2 py-2">厂牌</th>
            <th v-if="showCatalogNumber" class="px-2 py-2">目录号</th>
            <th class="w-16 px-2 py-2">置信度</th>
            <th class="w-24 px-2 py-2">动作</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="(row, i) in visibleRows"
            :key="`${row.provider}-${row.matchRef}-${i}`"
            class="border-b hover:bg-accent/40"
          >
            <td class="px-2 py-1.5">
              <!-- 来源徽章可叠加：[本地][MB] -->
              <div class="flex flex-wrap gap-1">
                <span
                  v-for="badge in row.badges"
                  :key="badge"
                  class="rounded bg-secondary px-1.5 py-0.5 text-[10px] text-secondary-foreground"
                >
                  {{ badge }}
                </span>
              </div>
              <div v-if="row.note" class="mt-0.5 text-[10px] text-muted-foreground">
                {{ row.note }}
              </div>
            </td>
            <td class="px-2 py-1.5">
              <img
                v-if="row.thumbKey && !brokenCovers.has(row.thumbKey)"
                :src="coverUrl(row.thumbKey, coverThumbSize)"
                loading="lazy"
                class="size-8 rounded object-cover"
                alt=""
                @error="brokenCovers.add(row.thumbKey)"
              />
              <!-- 取图失败只影响该行：显示占位，标题等其余列照常渲染 -->
              <div v-else class="size-8 rounded bg-muted" />
            </td>
            <td class="max-w-0 truncate px-2 py-1.5" :title="row.title ?? ''">
              {{ display(row.title) }}
            </td>
            <td class="max-w-0 truncate px-2 py-1.5" :title="row.artist ?? ''">
              {{ display(row.artist) }}
            </td>
            <td class="max-w-0 truncate px-2 py-1.5">
              {{ display(row.album) }}
              <span v-if="showPrimaryType && row.primaryType" class="text-xs text-muted-foreground">
                ({{ row.primaryType }})
              </span>
            </td>
            <td class="px-2 py-1.5 tabular-nums">{{ duration(row.durationMs) }}</td>
            <td class="px-2 py-1.5 tabular-nums">
              {{ trackPosition(row.trackNumber, row.trackTotal) }}
            </td>
            <td class="px-2 py-1.5 tabular-nums">{{ display(row.year) }}</td>
            <td v-if="showMedium" class="px-2 py-1.5">
              {{ cell(row, 'MediumFormat', row.mediumFormat) }}
            </td>
            <td v-if="showCountry" class="px-2 py-1.5">
              {{ cell(row, 'Country', row.country) }}
            </td>
            <td v-if="showLabel" class="max-w-0 truncate px-2 py-1.5">
              {{ cell(row, 'Label', row.label) }}
            </td>
            <td v-if="showCatalogNumber" class="max-w-0 truncate px-2 py-1.5">
              {{ cell(row, 'CatalogNumber', row.catalogNumber) }}
            </td>
            <td class="px-2 py-1.5 tabular-nums">{{ Math.round(row.confidence) }}</td>
            <td class="px-2 py-1.5">
              <button
                class="rounded border px-2 py-0.5 text-xs hover:bg-accent disabled:cursor-pointer disabled:opacity-60"
                :disabled="row.action === '已在库中'"
                :title="row.action === '已在库中' ? '点击跳转到该曲目' : ''"
                @click="onAct(row)"
              >
                {{ row.action }}
              </button>
            </td>
          </tr>

          <tr v-if="rows.length === 0 && !loading">
            <td :colspan="14" class="px-3 py-8 text-center text-muted-foreground">
              <slot name="empty">没有候选结果</slot>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- 截断提示与展开 -->
    <div
      v-if="truncated"
      class="flex items-center gap-2 border-t px-3 py-2 text-xs text-muted-foreground"
    >
      <span>返回 {{ total || rows.length }} 条，已按置信度排序，显示前 {{ limit }} 条</span>
      <button class="rounded border px-2 py-0.5 hover:bg-accent" @click="expanded = true">
        显示全部
      </button>
      <slot name="footer-actions" />
    </div>
    <div v-else-if="$slots['footer-actions']" class="flex items-center gap-2 border-t px-3 py-2 text-xs">
      <div class="flex-1" />
      <slot name="footer-actions" />
    </div>
  </div>
</template>
