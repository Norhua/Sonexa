<script setup lang="ts">
import { computed } from 'vue'
import { display, type Models } from '@/api'

const props = defineProps<{ source: Models.app.ReviewItem }>()
const reason = computed(() => ({
  new_release: '新增发行加入已有专辑组，接受后还需确认成员变化',
  group_insufficient: '判组资料不足，可按已知信息保存',
  group_conflict: '专辑组证据冲突，需要明确合组方案',
  unknown_total: '发行总曲数未知，需要人工选版',
  no_match: '没有足够证据自动确定目标', ambiguous: '候选存在歧义', duplicate: '相关收藏不足以证明精确身份',
}[props.source.reviewReason ?? ''] ?? props.source.reviewReason ?? ''))
const evidence = computed(() => {
  try {
    return JSON.parse(props.source.reviewEvidence || 'null') as (Models.domain.AdmissionBaseline & {
      proposed?: Models.app.PlanReleaseView; relatedTrackId?: number; detail?: string
      reasons?: { code: string; field: string; detail?: string }[]
    }) | null
  } catch { return null }
})
const fields: Record<string, string> = { title: '曲名', credit: '署名', artist: '艺术家', album: '专辑', disc: '碟号', track: '轨号', duration: '时长', year: '年份' }
</script>

<template>
  <div v-if="reason || evidence" class="space-y-2 rounded border p-3 text-xs" data-testid="review-reasons">
    <p v-if="reason" class="font-medium">{{ reason }}</p>
    <p v-for="(item, i) in evidence?.reasons ?? []" :key="i">
      <span v-if="item.field">{{ fields[item.field] || item.field }}： </span>{{ item.detail || item.code }}
      <span v-if="item.detail" class="text-muted-color">（{{ item.code }}）</span>
    </p>
    <p v-if="evidence?.relatedTrackId">相关收藏（不是精确拥有）：{{ evidence.detail }}。可改搜选择上游 track，或在编辑器处理原收藏。</p>
    <div v-for="group in evidence?.resolution?.groups ?? []" :key="group.id">
      <p>已有专辑组：{{ group.title || '未命名专辑' }} · {{ display(group.credit) }}</p>
      <p v-for="member in group.members" :key="member.id" class="text-muted-color">已有发行：{{ member.title || '未命名发行' }} · {{ member.identity?.provider || '本地' }} · {{ display(member.identity?.ref) }} · 收藏 {{ member.trackIds?.length || 0 }} 首</p>
    </div>
    <p v-if="evidence?.proposed">拟加入发行：{{ evidence.proposed.title?.value || '未命名发行' }} · {{ evidence.proposed.identity?.provider }} · {{ evidence.proposed.identity?.ref }}</p>
    <p v-for="(item, i) in evidence?.resolution?.evidence ?? []" :key="i">判组依据：{{ item.kind === 'native' ? '相同上游组身份' : '专辑名与专辑艺术家相同' }} · {{ item.title }} · {{ item.credit }}</p>
  </div>
</template>
