<script setup lang="ts">
/** Independent review problems; original rows and displayed action scope stay explicit. */
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { App, display, duration, Events, EventsOn, type Models } from '@/api'
import { candidateActionEnabled, invalidateCandidateOwnership } from '@/lib/candidateActions'
import { createRefreshQueue } from '@/lib/refreshQueue'
import CandidateTable from '@/components/CandidateTable.vue'
import ReviewReasons from '@/components/ReviewReasons.vue'
import EmptyState from '@/components/EmptyState.vue'
import Button from '@/volt/Button.vue'
import InputText from '@/volt/InputText.vue'
import Message from '@/volt/Message.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import Select from '@/volt/Select.vue'
import Tag from '@/volt/Tag.vue'
import { useSettingsStore } from '@/stores/settings'
import { useLibraryStore } from '@/stores/library'
import { useSearchStore } from '@/stores/search'
import { useTasksStore } from '@/stores/tasks'
import { useReviewStore } from '@/stores/review'
import { useAdmissionStore } from '@/stores/admission'

const settings = useSettingsStore(), tasks = useTasksStore(), library = useLibraryStore()
const search = useSearchStore(), review = useReviewStore(), admission = useAdmissionStore()
const emit = defineEmits<{ jumpLocal: [trackId: number]; goSettings: [] }>()
const problem = ref<Models.app.ReviewProblemView | null>(null)
const sourceId = ref<number | null>(null)
const source = computed(() => problem.value?.sources.find(row => row.id === sourceId.value) ?? null)
const candidates = ref<Models.app.CandidateRow[]>([])
const error = ref(''), localError = ref('')
const busy = ref(false), researching = ref(false), refreshingLocal = ref(false)
const expanded = ref(false), sourcesExpanded = ref(false), stale = ref(false)
const research = ref({ title: '', artist: '', album: '' })
const researchProvider = ref(''), country = ref('')
const countries = ref<Models.app.CountryOption[]>([])
const hint = ref<Models.app.CountryHint | null>(null)
const searched = ref(false), draftDirty = ref(false)
let restoring = false, disposed = false, requestRevision = 0, localRevision = 0

const countryOptions = computed(() => [{ code: '', name: '跟随默认' }, ...countries.value])
const showCountry = computed(() => settings.anyNeedsCountry([researchProvider.value]))
const providerCaps = computed(() => Object.fromEntries(settings.providers.map(p => [p.id, p.capabilities ?? []])))
const activeCaps = computed(() => [...new Set([
  ...(providerCaps.value[researchProvider.value] ?? []),
  ...candidates.value.flatMap(row => providerCaps.value[row.provider] ?? []),
])])
const sourceOptions = computed(() => problem.value?.sources.map(row => ({ id: row.id, label: `${row.batchTag} · 第 ${row.rowIndex + 1} 行 · ${display(row.inTitle)}` })) ?? [])
const blocked = computed(() => busy.value || researching.value || stale.value)
const failedIds = computed(() => problem.value?.sources.filter(row => row.status === 'failed').map(row => row.id) ?? [])
const scopeKey = (p: Models.app.ReviewProblemView) => JSON.stringify(p.scope)
const memberKey = (p: Models.app.ReviewProblemView) => JSON.stringify(p.scope.members.map(m => m.itemId))

watch(() => settings.providers, providers => {
  if (!providers.some(p => p.id === researchProvider.value)) researchProvider.value = providers[0]?.id ?? ''
}, { immediate: true })
watch([research, country], () => { if (!restoring) draftDirty.value = true }, { deep: true, flush: 'sync' })
watch(researchProvider, () => {
  if (restoring) return
  draftDirty.value = true
  if (searched.value && !blocked.value && settings.outboundAllowed) void doResearch()
}, { flush: 'sync' })

function adopt(next: Models.app.ReviewProblemView | null, reset = false) {
  problem.value = next
  stale.value = false
  const row = next?.sources.find(row => row.id === sourceId.value)
    ?? next?.sources.find(row => row.status === 'review' && row.candidates.length)
    ?? next?.sources[0]
  sourceId.value = row?.id ?? null
  candidates.value = row?.candidates ?? []
  if (!reset) return
  requestRevision++
  researching.value = false
  expanded.value = false; sourcesExpanded.value = false
  error.value = ''; localError.value = ''; searched.value = false
  restoring = true
  research.value = next?.query
    ? { title: next.query.title, artist: next.query.artist, album: next.query.album }
    : { title: row?.inTitle ?? '', artist: row?.inArtist ?? '', album: row?.inAlbum ?? '' }
  researchProvider.value = next?.query?.provider || settings.providers[0]?.id || ''
  country.value = next?.query?.country ?? ''
  restoring = false; draftDirty.value = false
  void updateHint()
}

watch(() => review.current, next => {
  if (disposed) return
  const old = problem.value
  if (old?.id !== next?.id || !old) { adopt(next, true); return }
  if (scopeKey(old) !== scopeKey(next!) && (busy.value || researching.value || searched.value || draftDirty.value)) {
    // Do not silently broaden a displayed search/approval. Reopening retains
    // the draft but explicitly replaces the visible source range.
    stale.value = true
    return
  }
  if (!busy.value && !researching.value) adopt(next)
}, { immediate: true })
watch(sourceId, () => {
  candidates.value = source.value?.candidates ?? []
  expanded.value = false; localError.value = ''
  void refreshLocal()
})

const localRefresh = createRefreshQueue(async latest => {
  const rows = candidates.value, id = problem.value?.id, selected = sourceId.value, revision = requestRevision
  if (!id || !rows.length) return
  const owns = () => latest() && !disposed && id === problem.value?.id && selected === sourceId.value && rows === candidates.value && revision === requestRevision
  try {
    const next = await App.RefreshReviewCandidates(rows)
    if (owns()) { candidates.value = next ?? []; localError.value = '' }
  } catch (e) {
    if (owns()) { candidates.value = invalidateCandidateOwnership(rows, '本地身份刷新失败，请重试'); localError.value = String(e) }
  }
})
async function refreshLocal() {
  localRevision++
  refreshingLocal.value = true
  try { await localRefresh.request() }
  finally { if (!disposed) refreshingLocal.value = false }
}
const offLibrary = EventsOn(Events.LibraryChanged, () => { void refreshLocal() })
onMounted(() => {
  review.subscribe()
  void Promise.all([review.refresh(), tasks.refresh()])
  void App.ListCountries().then(rows => { if (!disposed) countries.value = rows ?? [] }).catch(e => { if (!disposed) error.value = String(e) })
})
onBeforeUnmount(() => {
  disposed = true; requestRevision++
  review.unsubscribeAll(); offLibrary(); localRefresh.dispose()
})

async function updateHint() {
  const owner = requestRevision
  const text = `${research.value.title} ${research.value.artist}`.trim()
  hint.value = null
  if (!text) return
  try {
    const next = await App.SuggestCountry(text)
    if (disposed || owner !== requestRevision) return
    hint.value = next
    if (next?.detected && !country.value) country.value = next.country
  } catch { /* Optional hint does not prevent review. */ }
}
function restoreOriginal() {
  if (!source.value) return
  research.value = { title: source.value.inTitle ?? '', artist: source.value.inArtist ?? '', album: source.value.inAlbum ?? '' }
}
async function reopen() {
  if (busy.value || researching.value) return
  await review.refresh()
  if (review.current?.id === problem.value?.id) { requestRevision++; adopt(review.current); void refreshLocal() }
}
async function reloadAfter(scope: Models.app.ReviewProblemView) {
  await Promise.all([review.refresh(), tasks.refresh()])
  if (disposed || problem.value?.id !== scope.id || review.current?.id !== scope.id) return
  // Own successful query can advance revisions, not membership.
  if (memberKey(scope) !== memberKey(review.current)) { stale.value = true; return }
  adopt(review.current)
}
async function doResearch() {
  const p = problem.value
  if (!p || blocked.value || !settings.outboundAllowed) return
  const owner = ++requestRevision, localAtStart = localRevision
  searched.value = true; researching.value = true; error.value = ''
  try {
    const rows = await App.ResearchReviewProblem(p.scope, { version: 1, ...research.value, provider: researchProvider.value, country: country.value } as Models.store.ReviewQuery)
    if (disposed || owner !== requestRevision || problem.value?.id !== p.id) return
    candidates.value = rows ?? []
    await reloadAfter(p)
    if (localAtStart !== localRevision) void refreshLocal()
  } catch (e) { if (!disposed && owner === requestRevision) { error.value = String(e); void review.refresh() } }
  finally { if (owner === requestRevision) researching.value = false }
}
async function act(action: (p: Models.app.ReviewProblemView) => Promise<void | boolean>) {
  const p = problem.value
  if (!p || blocked.value) return
  busy.value = true; error.value = ''
  try {
    if (await action(p) !== false) {
      requestRevision++
      await reloadAfter(p)
    }
  } catch (e) {
    if (!disposed && problem.value?.id === p.id) { error.value = String(e); void review.refresh(); void refreshLocal() }
  } finally { if (!disposed) busy.value = false }
}
function accept(row: Models.app.CandidateRow) {
  if (!candidateActionEnabled(row) || !['accept', 'skip'].includes(row.action)) return
  void act(async p => {
    const scope = await App.GetReviewAdmissionScope(p.scope)
    if (row.action === 'skip') {
      const plan = await App.PrepareProblemSkipExisting(row, scope)
      await App.CommitAdmission(plan.id, plan.hash, {} as Models.domain.AdmissionApproval)
      return
    }
    const result = await admission.run(() => App.PrepareProblemAdmission(row, scope))
    if (admission.resolution && problem.value?.id === p.id) {
      candidates.value = admission.resolution.candidates; expanded.value = true
      error.value = '目标有多个位置或资料不足，请选择具体曲目位置'
    }
    return result !== null
  })
}
function noMatchNeeded() {
  const id = sourceId.value
  if (id) void act(async () => (await admission.run(() => App.PrepareNoMatchNeeded(id))) !== null)
}
function discard() {
  const id = sourceId.value
  if (id) void act(p => App.DiscardReviewProblemMembers(p.scope, [id]))
}
function split() {
  const id = sourceId.value
  if (id) void act(async p => { await App.SplitReviewProblemItem(p.scope, id) })
}
function retry() {
  const ids = [...failedIds.value]
  if (ids.length) void act(p => App.RetryReviewProblemMembers(p.scope, ids))
}
function move(delta: number) {
  if (busy.value) return
  const next = review.items[review.index + delta]
  if (next) review.select(next.id)
}
async function onEmptyAction(key: string) {
  if (key !== 'upstream') { await library.goTo('all'); return }
  search.setScope(settings.providers[0]?.id ?? 'library'); await library.goTo('search')
}
function numberText(row: Models.app.ReviewItem, key: 'year' | 'disc' | 'track' | 'duration', value: number | null | undefined) {
  const field = row.profile?.[key]
  if (field?.state === 'invalid') return `${field.raw}（无法解析）`
  if (field?.raw) return field.raw
  return key === 'duration' ? duration(value) : display(value)
}
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <header class="flex h-12 shrink-0 items-center gap-3 px-4" style="--wails-draggable: drag">
      <h2 class="text-xl font-bold">复核队列</h2>
      <span v-if="review.queue" class="text-sm text-muted-color">
        {{ review.problems.reviewProblems }} 个待处理问题 · 待选 {{ review.problems.pendingProblems }} · 失败 {{ review.problems.failedProblems }} · 涉及 {{ review.problems.sourceRows }} 行
      </span>
      <div class="flex-1" />
      <SecondaryButton size="small" :disabled="review.loading || busy || researching" @click="reopen">刷新队列</SecondaryButton>
      <SecondaryButton v-if="candidates.length" size="small" :disabled="refreshingLocal" @click="refreshLocal">刷新本地状态</SecondaryButton>
    </header>
    <EmptyState v-if="review.queue && !review.items.length && !review.loading" title="复核队列是空的"
      description="导入时无法自动判断的独立问题会出现在这里，可展开查看每一行来源。"
      :actions="[{ label: '到全部曲目', key: 'all' }, { label: '到上游搜索', key: 'upstream' }]" @action="onEmptyAction" />
    <div v-else-if="problem && source" class="min-h-0 flex-1 overflow-auto">
      <section class="border-b p-4">
        <div class="flex items-start gap-3">
          <div class="min-w-0 flex-1">
            <p class="text-base font-medium" data-testid="problem-title">{{ display(problem.sources[0]?.inTitle) }}</p>
            <p class="mt-0.5 text-xs text-muted-color">问题 #{{ problem.id }} · 涉及 {{ problem.sources.length }} 行 · {{ new Set(problem.sources.map(row => row.batchId)).size }} 个批次</p>
          </div>
          <Tag :severity="problem.status === 'failed' ? 'danger' : 'secondary'" :value="problem.status === 'failed' ? '失败' : '待选'" />
        </div>
        <div class="mt-3 flex items-center gap-2 text-sm">
          <SecondaryButton size="small" :disabled="review.index <= 0 || busy" @click="move(-1)">← 上一条</SecondaryButton>
          <span class="tabular-nums text-muted-color">{{ review.index + 1 }} / {{ review.items.length }}</span>
          <SecondaryButton size="small" :disabled="review.index >= review.items.length - 1 || busy" @click="move(1)">下一条 →</SecondaryButton>
          <SecondaryButton size="small" :aria-expanded="sourcesExpanded" @click="sourcesExpanded = !sourcesExpanded">{{ sourcesExpanded ? '收起来源' : '展开来源' }}（{{ problem.sources.length }} 行）</SecondaryButton>
        </div>
        <div v-if="sourcesExpanded" class="mt-3 space-y-3" data-testid="problem-sources">
          <article v-for="row in problem.sources" :key="row.id" class="space-y-2 rounded border p-3 text-sm" :data-source-id="row.id">
            <p class="font-medium">{{ row.batchTag }} · 批次 #{{ row.batchId }} · 第 {{ row.rowIndex + 1 }} 行 · {{ ({ review: '待选', failed: '失败', queued: '排队中', matching: '匹配中' } as Record<string, string>)[row.status] || row.status }}</p>
            <dl class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 break-words">
              <dt>原始曲名</dt><dd>{{ display(row.inTitle) }}</dd>
              <dt>原始艺术家</dt><dd>{{ display(row.inArtist) }}</dd>
              <dt>原始专辑</dt><dd>{{ display(row.inAlbum) }}</dd>
              <dt>年份 / 碟号 / 轨号 / 时长</dt><dd>{{ numberText(row, 'year', row.inYear) }} / {{ numberText(row, 'disc', row.inDiscNo) }} / {{ numberText(row, 'track', row.inTrackNo) }} / {{ numberText(row, 'duration', row.inDurationMs) }}</dd>
            </dl>
            <details><summary class="cursor-pointer">完整原始行</summary><pre class="mt-1 whitespace-pre-wrap break-all text-xs">{{ row.rawRow }}</pre></details>
            <ReviewReasons :source="row" />
            <p v-if="row.status === 'failed'" class="text-xs text-danger-color">已重试 {{ row.attemptCount }} 次 · 最后错误：{{ display(row.lastError) }} · {{ row.lastErrorAt ? new Date(row.lastErrorAt).toLocaleString() : '—' }}</p>
          </article>
        </div>
        <p class="mt-3 rounded border p-3 text-xs" data-testid="problem-scope">接受 / 跳过 / 改搜 / 无一匹配作用于上述 {{ problem.sources.length }} 行；一次接受最多新增一个 track，其余精确重复行记已存在跳过。</p>
        <Message v-if="stale" severity="warn" class="mt-2">问题范围或证据已变化，旧操作已禁用。草稿仍保留，请刷新队列查看新范围后重新确认。</Message>
      </section>
      <section class="space-y-3 border-b p-4">
        <label class="block text-xs">来源行（候选事实及单行操作）
          <Select v-model="sourceId" :options="sourceOptions" option-label="label" option-value="id" size="small" :disabled="busy || researching" class="mt-1 w-full" />
        </label>
        <ReviewReasons :source="source" />
        <div class="flex flex-wrap items-end gap-2">
          <label class="text-xs">曲名<InputText v-model="research.title" size="small" class="mt-1 block w-48" :disabled="busy" /></label>
          <label class="text-xs">艺术家<InputText v-model="research.artist" size="small" class="mt-1 block w-36" :disabled="busy" /></label>
          <label class="text-xs">专辑<InputText v-model="research.album" size="small" class="mt-1 block w-36" :disabled="busy" /></label>
          <label class="text-xs">上游<Select v-model="researchProvider" :options="settings.providers" option-label="displayName" option-value="id" size="small" class="mt-1 block" :disabled="busy" /></label>
          <label v-if="showCountry" class="text-xs">地区<Select v-model="country" :options="countryOptions" option-label="name" option-value="code" size="small" class="mt-1 block" :disabled="busy" /></label>
          <Button size="small" :disabled="blocked || !settings.outboundAllowed" :title="!settings.outboundAllowed ? settings.outboundReason : ''" @click="doResearch">{{ researching ? '搜索中…' : '重新搜索' }}</Button>
        </div>
        <p v-if="showCountry && hint?.detected" class="text-xs text-muted-color">{{ hint.message }}</p>
        <p class="text-xs text-muted-color">单行操作仅作用于 {{ source.batchTag }} 第 {{ source.rowIndex + 1 }} 行（输入 #{{ source.id }}），不影响其他来源。</p>
        <div class="flex flex-wrap gap-2">
          <SecondaryButton size="small" :disabled="busy" @click="restoreOriginal">恢复原始信息</SecondaryButton>
          <SecondaryButton size="small" :disabled="blocked" @click="noMatchNeeded">无需匹配</SecondaryButton>
          <SecondaryButton size="small" :disabled="blocked" @click="discard">移出队列</SecondaryButton>
          <SecondaryButton v-if="problem.sources.length > 1" size="small" :disabled="blocked" @click="split">拆出此行</SecondaryButton>
        </div>
        <p v-if="!source.inTitle || !source.inArtist || !source.inAlbum" class="text-xs text-muted-color">部分基本信息缺失，仍可选择候选或按选定行的原始信息无需匹配保存。</p>
      </section>
      <section :inert="blocked || undefined">
        <CandidateTable v-model:expanded="expanded" :rows="candidates" :capabilities="activeCaps" :provider-capabilities="providerCaps" :limit="10" :total="candidates.length" @act="accept" @jump-local="id => emit('jumpLocal', id)">
          <template #empty>还没有候选结果，可改写条件或换上游再试</template>
          <template #footer-actions><SecondaryButton size="small" :disabled="blocked" @click="act(p => App.NoneMatchedProblem(p.scope))">无一匹配</SecondaryButton></template>
        </CandidateTable>
      </section>
      <section v-if="failedIds.length" class="space-y-2 border-t p-4 text-sm">
        <h3 class="font-medium">失败来源（{{ failedIds.length }} 行）</h3>
        <p v-for="row in problem.sources.filter(row => row.status === 'failed')" :key="row.id">{{ row.batchTag }} 第 {{ row.rowIndex + 1 }} 行 · 已重试 {{ row.attemptCount }} 次 · {{ display(row.lastError) }} · {{ row.lastErrorAt ? new Date(row.lastErrorAt).toLocaleString() : '—' }}</p>
        <p class="text-xs text-muted-color">立即重试仅处理输入 #{{ failedIds.join('、#') }}。</p>
        <SecondaryButton size="small" :disabled="blocked" @click="retry">立即重试</SecondaryButton>
        <SecondaryButton size="small" @click="library.goTo('importQueue')">查看日志</SecondaryButton>
      </section>
    </div>
    <Message v-if="error || review.error || localError" severity="error" size="small" class="shrink-0 rounded-none">{{ error || review.error || localError }}</Message>
  </div>
</template>
