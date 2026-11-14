<script setup lang="ts">
/**
 * 全应用唯一的候选结果表格组件。
 *
 * 上游搜索结果与复核队列候选列表共用本组件，列定义完全一致；
 * 动作按上下文不同（加入曲库 / 已在库中 / 接受 / 跳过），而列不变。
 *
 * 表格本体与曲目表格同为 Volt 的 DataTable——两张表的行高、表头、
 * 边框因此天然一致，不靠各写一遍 class 去对齐。
 */
import { computed, toRef } from 'vue'
import Column from 'primevue/column'
import DataTable from '@/volt/DataTable.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import Tag from '@/volt/Tag.vue'
import { display, duration, trackPosition, DASH } from '@/api'
import CoverArt from '@/components/CoverArt.vue'
import CandidateAlbumName from '@/components/CandidateAlbumName.vue'
import { useCandidateCovers } from '@/lib/useCandidateCovers'
import type { Models } from '@/api'

/**
 * 候选行的封面来源。
 *
 * 候选以发行身份读取已存在的本地图；只有 MusicBrainz 发行允许自动查询 CAA。
 */
function coverSourceOf(row: Models.app.CandidateRow) {
  return {
    releaseId: row.releaseId,
    revision: row.coverRevision,
    provider: row.provider,
    ref: row.releaseRef,
    rg: row.releaseGroupRef,
  }
}

const props = withDefaults(
  defineProps<{
    rows: Models.app.CandidateRow[]
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
  }>(),
  { providerCapabilities: () => ({}), loading: false },
)

const emit = defineEmits<{
  act: [row: Models.app.CandidateRow]
  jumpLocal: [trackId: number]
}>()

/** 能力驱动的列显示：集合内全不支持则隐藏该列。 */
function columnVisible(cap: string): boolean {
  return props.capabilities.includes(cap)
}

const showMedium = computed(() => columnVisible('MediumFormat'))
const showCountry = computed(() => columnVisible('Country'))
const showPrimaryType = computed(() => columnVisible('PrimaryType'))

/** 某一行是否支持某列——不支持时填占位短横线。 */
function rowSupports(row: Models.app.CandidateRow, cap: string): boolean {
  const caps = props.providerCapabilities[row.provider]
  if (!caps) return true
  return caps.includes(cap)
}

function cell(row: Models.app.CandidateRow, cap: string, value: unknown): string {
  if (!rowSupports(row, cap)) return DASH
  return display(value as string | number | null | undefined)
}

useCandidateCovers(toRef(props, 'rows'))

function onAct(row: Models.app.CandidateRow) {
  // 「已在库中」为禁用态，但点击可跳转到该曲目
  if (row.action === '已在库中') {
    if (row.localTrackId) emit('jumpLocal', row.localTrackId)
    return
  }
  emit('act', row)
}
</script>

<template>
  <div class="flex min-w-0 flex-col">
    <DataTable :value="rows" :loading="loading" row-hover scrollable resizable-columns column-resize-mode="expand" :table-style="{ minWidth: '1280px', tableLayout: 'fixed' }" :pt="{ table: { 'aria-label': '候选列表' } }">
      <template #empty>
        <div class="px-3 py-8 text-center text-muted-color">
          <slot name="empty">没有候选结果</slot>
        </div>
      </template>

      <Column header="来源" style="width: 96px">
        <template #body="{ data }">
          <!-- 来源徽章可叠加：[本地][MB] -->
          <div class="flex flex-wrap gap-1">
            <Tag
              v-for="badge in data.badges"
              :key="badge"
              severity="secondary"
              :title="badge === '本地' ? '精确本地绑定；不代表已加入音乐库' : badge"
              :value="badge"
              class="px-1.5 py-0.5 text-[10px] font-normal"
            />
          </div>
        </template>
      </Column>

      <Column class="w-12">
        <template #body="{ data }">
          <!-- 取图失败只影响该行：显示状态图标，标题等其余列照常渲染 -->
          <CoverArt :source="coverSourceOf(data)" usage="thumbnail" class="size-8 rounded" />
        </template>
      </Column>

      <Column field="title" header="标题" style="width: 240px; min-width: 100px">
        <template #body="{ data }">
          <div class="truncate" :title="data.title ?? ''">{{ display(data.title) }}</div>
        </template>
      </Column>

      <Column field="artist" header="艺术家" style="width: 220px; min-width: 100px">
        <template #body="{ data }">
          <div class="truncate" :title="data.artist ?? ''">{{ display(data.artist) }}</div>
        </template>
      </Column>

      <Column field="album" header="歌集名" style="width: 300px; min-width: 100px">
        <template #body="{ data }">
          <div class="truncate select-text" :title="[data.album, showPrimaryType && data.primaryType ? `(${data.primaryType})` : ''].filter(Boolean).join(' ')">
            <CandidateAlbumName
              :album="data.album"
              :fallback="DASH"
              :release-exists="data.releaseExists"
              :release-group-exists="data.releaseGroupExists"
              :detail="showPrimaryType && data.primaryType ? `(${data.primaryType})` : ''"
            />
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

      <Column header="音轨" class="w-20">
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
        <template #body="{ data }"><span :title="cell(data, 'MediumFormat', data.mediumFormat)">{{ cell(data, 'MediumFormat', data.mediumFormat) }}</span></template>
      </Column>

      <Column v-if="showCountry" header="国家/地区" class="w-20">
        <template #body="{ data }">{{ cell(data, 'Country', data.country) }}</template>
      </Column>

      <Column header="置信度" class="w-16">
        <template #body="{ data }">
          <span class="tabular-nums">{{ Math.round(data.confidence) }}</span>
        </template>
      </Column>

      <Column class="w-24">
        <template #body="{ data }">
          <SecondaryButton
            variant="outlined"
            size="small"
            :disabled="data.action === '已在库中' && !data.localTrackId"
            :aria-label="data.action === '已在库中' ? `查看库中曲目：${data.title}` : undefined"
            :title="data.action === '已在库中' ? '点击跳转到该曲目' : ''"
            @click="onAct(data)"
          >
            {{ data.action }}
          </SecondaryButton>
        </template>
      </Column>
    </DataTable>
  </div>
</template>
