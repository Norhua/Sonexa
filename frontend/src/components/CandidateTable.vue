<script setup lang="ts">
/**
 * 全应用唯一的候选结果表格组件。
 *
 * 上游搜索结果与复核队列候选列表共用本组件，列定义完全一致；
 * 动作按上下文不同（加入曲库 / 已在库中 / 接受 / 合并），而列不变。
 *
 * 表格本体与曲目表格同为 Volt 的 DataTable——两张表的行高、表头、
 * 边框因此天然一致，不靠各写一遍 class 去对齐。
 */
import { computed } from 'vue'
import Column from 'primevue/column'
import DataTable from '@/volt/DataTable.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import Tag from '@/volt/Tag.vue'
import { display, duration, trackPosition, DASH } from '@/api'
import CoverArt from '@/components/CoverArt.vue'
import type { Models } from '@/api'

/**
 * 候选行的封面来源。
 *
 * MusicBrainz 的候选有 release 级标识，iTunes 没有——它只给一个基准图地址，
 * 降级链由那个地址构造，因此把它当作来源标识。这与后端 BuildCoverRefs
 * 算缓存键时的取源必须一致，否则预取暖好的图对不上号、会被重下一遍。
 */
function coverSourceOf(row: Models.main.CandidateRow) {
  return {
    provider: row.provider,
    ref: row.releaseRef || row.artworkUrl,
    rg: row.releaseGroupRef,
  }
}

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
    <DataTable :value="visibleRows" :loading="loading" row-hover>
      <template #empty>
        <div class="px-3 py-8 text-center text-muted-color">
          <slot name="empty">没有候选结果</slot>
        </div>
      </template>

      <Column header="来源" class="w-24">
        <template #body="{ data }">
          <!-- 来源徽章可叠加：[本地][MB] -->
          <div class="flex flex-wrap gap-1">
            <Tag
              v-for="badge in data.badges"
              :key="badge"
              severity="secondary"
              :value="badge"
              class="px-1.5 py-0.5 text-[10px] font-normal"
            />
          </div>
          <div v-if="data.note" class="mt-0.5 text-[10px] text-muted-color">{{ data.note }}</div>
        </template>
      </Column>

      <Column class="w-12">
        <template #body="{ data }">
          <!-- 取图失败只影响该行：显示状态图标，标题等其余列照常渲染 -->
          <CoverArt :source="coverSourceOf(data)" class="size-8 rounded" />
        </template>
      </Column>

      <Column header="标题" class="max-w-0">
        <template #body="{ data }">
          <div class="truncate" :title="data.title ?? ''">{{ display(data.title) }}</div>
        </template>
      </Column>

      <Column header="艺术家" class="max-w-0">
        <template #body="{ data }">
          <div class="truncate" :title="data.artist ?? ''">{{ display(data.artist) }}</div>
        </template>
      </Column>

      <Column header="歌集名" class="max-w-0">
        <template #body="{ data }">
          <div class="truncate">
            {{ display(data.album) }}
            <span v-if="showPrimaryType && data.primaryType" class="text-xs text-muted-color">
              ({{ data.primaryType }})
            </span>
          </div>
        </template>
      </Column>

      <Column header="时长" class="w-16">
        <template #body="{ data }">
          <span class="tabular-nums">{{ duration(data.durationMs) }}</span>
        </template>
      </Column>

      <Column header="第几曲" class="w-20">
        <template #body="{ data }">
          <span class="tabular-nums">{{ trackPosition(data.trackNumber, data.trackTotal) }}</span>
        </template>
      </Column>

      <Column header="年份" class="w-16">
        <template #body="{ data }">
          <span class="tabular-nums">{{ display(data.year) }}</span>
        </template>
      </Column>

      <Column v-if="showMedium" header="载体" class="w-20">
        <template #body="{ data }">{{ cell(data, 'MediumFormat', data.mediumFormat) }}</template>
      </Column>

      <Column v-if="showCountry" header="国家/地区" class="w-20">
        <template #body="{ data }">{{ cell(data, 'Country', data.country) }}</template>
      </Column>

      <Column v-if="showLabel" header="厂牌" class="max-w-0">
        <template #body="{ data }">
          <div class="truncate">{{ cell(data, 'Label', data.label) }}</div>
        </template>
      </Column>

      <Column v-if="showCatalogNumber" header="目录号" class="max-w-0">
        <template #body="{ data }">
          <div class="truncate">{{ cell(data, 'CatalogNumber', data.catalogNumber) }}</div>
        </template>
      </Column>

      <Column header="置信度" class="w-16">
        <template #body="{ data }">
          <span class="tabular-nums">{{ Math.round(data.confidence) }}</span>
        </template>
      </Column>

      <Column header="动作" class="w-24">
        <template #body="{ data }">
          <SecondaryButton
            variant="outlined"
            size="small"
            :disabled="data.action === '已在库中'"
            :title="data.action === '已在库中' ? '点击跳转到该曲目' : ''"
            @click="onAct(data)"
          >
            {{ data.action }}
          </SecondaryButton>
        </template>
      </Column>
    </DataTable>

    <!-- 截断提示与展开 -->
    <div
      v-if="truncated"
      class="flex items-center gap-2 border-t px-3 py-2 text-xs text-muted-color"
    >
      <span>返回 {{ total || rows.length }} 条，已按置信度排序，显示前 {{ limit }} 条</span>
      <SecondaryButton variant="outlined" size="small" @click="expanded = true">
        显示全部
      </SecondaryButton>
      <slot name="footer-actions" />
    </div>
    <div v-else-if="$slots['footer-actions']" class="flex items-center gap-2 border-t px-3 py-2 text-xs">
      <div class="flex-1" />
      <slot name="footer-actions" />
    </div>
  </div>
</template>
