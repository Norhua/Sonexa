<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { CircleAlert, ArrowLeft, RotateCcw, ArrowRight, Search, RefreshCw, ChevronDown, Music2, Disc3, Link2, ImageIcon, Check, LoaderCircle } from 'lucide-vue-next'
import { App, type Models } from '@/api'
import { useSettingsStore } from '@/stores/settings'
import { useAdmissionStore } from '@/stores/admission'
import { useEditDraft, valueOf, copy, editableKeys } from '@/lib/useEditDraft'
import { editorLabels as labels, editorFactLabels, editorFieldText as fieldText, displayEditorValue as display, sameEditorValue } from '@/lib/editorDisplay'
import { lyricsText } from '@/lib/lyrics'
import EditCompareField from '@/components/EditCompareField.vue'
import CoverArt from '@/components/CoverArt.vue'
import Dialog from '@/volt/Dialog.vue'
import Button from '@/volt/Button.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import InputText from '@/volt/InputText.vue'
import Select from '@/volt/Select.vue'

const props = defineProps<{ trackId: number }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const settings = useSettingsStore(), admission = useAdmissionStore(), edit = useEditDraft()
const loading = ref(false), error = ref('')
const preparingUse = ref(false)
const source = ref(''), region = ref(''), searchTitle = ref(''), searchArtist = ref(''), searchAlbum = ref('')
const candidates = ref<Models.app.CandidateRow[]>([]), searched = ref(false), showCandidates = ref(true), selectedCandidate = ref('')
const versions = ref<Models.app.EditVersion[]>([]), versionTotal = ref<number | null>(null), showVersions = ref(false), versionsLoaded = ref(false)
const showTracklist = ref(false), lyricsOpen = ref(false), lyricsPlain = ref(false)
const useConfirmation = ref<Models.domain.EditPreview | null>(null), useValues = ref(true)
const bindingOpen = ref(false), bindingSource = ref(''), bindingRegion = ref(''), trackRef = ref(''), releaseRef = ref('')
const bindingParsed = ref<Models.domain.EditPreview | null>(null)
const albumPrompt = ref(false), impactOpen = ref(false), exitPrompt = ref(false), impactPending = ref(false)
const albumPlan = ref<Models.domain.AlbumChangePlan | null>(null), focusId = ref(0), coverURL = ref('')
const saveImpact = ref<Models.domain.EditImpact | null>(null)
const albumUpdateReason = computed(() => edit.bindingAction.value === 'clear_release' ? '已选择解除专辑绑定，请新建专辑信息并确认迁入歌曲。' : !edit.baseSession.value?.releaseId && !saveImpact.value?.linksNewUpstream ? '当前歌曲尚无专辑，请先新建专辑信息。' : '')
const previewCovers = ref<Record<string, Models.app.EditCoverDraft>>({})
let alive = true, impactGeneration = 0
const tokens = new Set<string>()
const groups = [
  { label: '歌曲信息', icon: Music2, keys: ['track.title', 'track.credit', 'track.year', 'track.discNumber', 'track.trackNumber', 'track.genre', 'track.roles.vocal', 'track.roles.lyricist', 'track.roles.composer', 'track.lyrics'] },
  { label: '专辑信息', icon: Disc3, keys: editableKeys.filter(key => key.startsWith('release.')) },
]
const previewTrackKeys = ['track.credit', 'track.year', 'track.discNumber', 'track.trackNumber', 'track.genre', 'track.roles.vocal', 'track.roles.lyricist', 'track.roles.composer']
const previewReleaseKeys = ['release.primaryType', 'release.discTotal', 'release.media']
type SnapshotField<T> = { state: 'value' | 'empty' | 'unknown'; value?: T }
type PreviewTrack = { discNumber: number; trackNumber: number; track: SnapshotField<Models.domain.Identity>; title: SnapshotField<string> }
type PreviewSnapshot = { group?: { identity?: Models.domain.Identity; title?: SnapshotField<string> }; release?: { group?: Models.domain.Identity; tracklist?: SnapshotField<PreviewTrack[]>; tracklistCompleteness?: string } }
const snapshot = computed<PreviewSnapshot | null>(() => edit.preview.value ? JSON.parse(edit.preview.value.snapshotJson) : null)
const previewGroup = computed(() => snapshot.value?.group?.identity ?? snapshot.value?.release?.group)
const tracklist = computed(() => snapshot.value?.release?.tracklist?.value ?? [])
const tracklistDiscs = computed(() => [...new Set(tracklist.value.map(row => row.discNumber))].map(number => ({ number, rows: tracklist.value.filter(row => row.discNumber === number) })))
const capabilities = computed(() => settings.providerById(source.value)?.capabilities ?? [])
const boundTrack = computed(() => ['clear_track', 'clear_release'].includes(edit.bindingAction.value) ? null : edit.bindingPreview.value?.trackIdentity ?? edit.baseSession.value?.trackIdentity)
const boundRelease = computed(() => edit.bindingAction.value === 'clear_release' ? null : edit.bindingPreview.value?.releaseIdentity ?? edit.baseSession.value?.releaseIdentity)
const selectedRow = computed(() => albumPlan.value?.rows.find(row => row.trackId === focusId.value))
const readonlyFields = computed(() => {
  const fields = { ...edit.upstream.value }
  if (edit.bindingAction.value !== 'clear_release') {
    const data = JSON.parse(edit.bindingPreview.value?.snapshotJson ?? edit.baseSession.value?.snapshotJson ?? '{}')
    const date = data.group?.firstReleaseDate
    if (date) fields['group.firstReleaseDate'] = { state: date.state, valueJson: JSON.stringify(date.value), mode: 'follow', source: '' } as Models.domain.EditValue
  }
  return Object.entries(fields).filter(([key, field]) => editorFactLabels[key] && field.state !== 'unknown')
})
const bindingStatus = computed(() => ({ set: '已更换绑定 · 未保存', clear_track: '将清除歌曲绑定', clear_release: '将解除发行绑定' })[edit.bindingAction.value as 'set' | 'clear_track' | 'clear_release'] ?? '')
const currentTitle = computed(() => valueOf(edit.baseSession.value?.current['track.title']) || '未命名歌曲')
const currentCover = computed(() => edit.baseSession.value?.releaseId && !valueOf(edit.baseSession.value?.current['release.cover'])?.cleared)
const upstreamArtwork = computed(() => edit.upstream.value['release.artwork'])
function providerName(id?: string) { return settings.providerById(id ?? '')?.displayName ?? (id ? '上游' : '本地') }
function identityLabel(identity?: Models.domain.Identity | null) { return identity?.ref ? `${providerName(identity.provider)} · 已绑定` : '未绑定' }
function identityChange(row: Models.domain.AlbumChangeRow) {
  const before = row.beforeIdentity, after = row.afterIdentity
  if (before?.provider === after?.provider && before?.ref === after?.ref) return '绑定保持不变'
  if (!after?.ref) return '解除歌曲绑定，保留当前值'
  return `重新关联 ${providerName(after.provider)} 中的对应歌曲`
}
function candidateKey(row: Models.app.CandidateRow) { return `${row.provider}:${row.matchRef}` }
function trackState(row: PreviewTrack) {
  if (row.track?.state === 'value' && row.track.value?.ref === edit.preview.value?.trackIdentity?.ref) return '本曲'
  return edit.preview.value?.trackStates?.[`${row.discNumber}:${row.trackNumber}`] ?? '未收藏'
}
function applyField(key: string) { const field = edit.upstream.value[key]; if (field && field.state !== 'unknown') edit.fields.value[key] = copy(field) }
async function run(action: () => Promise<void>) {
  loading.value = true; error.value = ''
  try { await action() } catch (e) { if (alive) error.value = String(e) } finally { if (alive) loading.value = false }
}
onMounted(() => run(async () => {
  const session = await App.GetEditSession(props.trackId)
  if (!alive) return
  edit.open(session.editor!)
  source.value = session.editor!.trackIdentity?.provider ?? session.editor!.releaseIdentity?.provider ?? settings.providers[0]?.id ?? ''
  searchTitle.value = valueOf(session.editor!.current['track.title']) ?? ''
  searchArtist.value = valueOf(session.editor!.current['track.credit']) ?? ''
  searchAlbum.value = valueOf(session.editor!.current['release.title']) ?? ''
  const owner = edit.requestOwner()
  const preview = await App.PreviewCurrentEditUpstream(props.trackId)
  if (preview && alive) edit.acceptPreview(owner, preview)
}))
onBeforeUnmount(() => { alive = false; edit.invalidateRequests(); impactGeneration++; for (const token of tokens) void App.DiscardEditCover(token) })
watch(source, () => { edit.invalidateRequests(); candidates.value = []; searched.value = false; selectedCandidate.value = ''; edit.preview.value = null }, { flush: 'sync' })
watch(() => `${previewGroup.value?.provider}:${previewGroup.value?.ref}`, () => { versions.value = []; versionTotal.value = null; showVersions.value = false; versionsLoaded.value = false })
watch([bindingSource, bindingRegion, trackRef, releaseRef], () => { bindingParsed.value = null }, { flush: 'sync' })
function close() { if (loading.value) return; if (edit.dirty.value) exitPrompt.value = true; else emit('close') }
function previewExpired(preview: Models.domain.EditPreview) { return !!preview.expiresAt && Date.parse(preview.expiresAt) <= Date.now() }
async function confirmUse(fill = true) {
  const current = edit.preview.value
  if (!current || loading.value) return
  const owner = edit.requestOwner()
  preparingUse.value = true
  await run(async () => {
    let preview = current
    const expired = previewExpired(current)
    if (expired || current.releaseIdentity && snapshot.value?.release?.tracklistCompleteness !== 'complete') {
      if (!settings.outboundAllowed) throw new Error(`${settings.outboundReason}；获取完整预览后才能使用此上游。`)
      preview = await App.LoadEditUpstream(current.releaseIdentity?.provider ?? current.trackIdentity!.provider, current.releaseIdentity?.ref ?? '', current.trackIdentity?.ref ?? '', region.value, expired)
      if (!alive || !edit.acceptPreview(owner, preview)) return
      if (preview.releaseIdentity && JSON.parse(preview.snapshotJson).release?.tracklistCompleteness !== 'complete') {
        throw new Error('上游暂未提供完整发行清单，请重试获取完整预览后再使用。')
      }
    }
    if (!alive || !edit.ownsRequest(owner)) return
    useValues.value = fill
    useConfirmation.value = copy(preview)
  })
  preparingUse.value = false
}
async function stageCover(preview: Models.domain.EditPreview) {
  const existing = previewCovers.value[preview.id]
  if (existing) return existing
  const cover = await App.StageEditUpstreamCover(preview.id)
  if (cover) { tokens.add(cover.token); previewCovers.value[preview.id] = cover }
  return cover
}
async function viewPreviewCover() {
  const preview = edit.preview.value
  if (preview) await run(async () => { await stageCover(preview) })
}
async function applyUse() {
  if (!useConfirmation.value) return
  // A long-lived confirmation must show the refreshed facts before accepting them.
  if (previewExpired(useConfirmation.value)) { await confirmUse(useValues.value); return }
  const frozen = copy(useConfirmation.value)
  await run(async () => {
    let cover: Models.app.EditCoverDraft | null = null
    const artwork = frozen.fields['release.artwork']
    if (useValues.value && artwork?.state === 'value') cover = await stageCover(frozen)
    if (!alive || useConfirmation.value?.id !== frozen.id) return
    edit.usePreview(frozen, useValues.value)
    if (cover) { edit.coverToken.value = cover.token; edit.coverAction.value = 'replace'; coverURL.value = cover.previewUrl }
    else if (useValues.value && artwork?.state === 'empty') { edit.coverAction.value = 'clear'; edit.coverToken.value = '' }
    useConfirmation.value = null; bindingOpen.value = false
  })
}
async function search() {
  const owner = edit.requestOwner()
  await run(async () => {
    const rows = await App.SearchEditCandidates(props.trackId, source.value, searchTitle.value, searchArtist.value, searchAlbum.value, region.value)
    if (!alive || !edit.ownsRequest(owner)) return
    candidates.value = rows ?? []; searched.value = true; showCandidates.value = true; selectedCandidate.value = ''
  })
}
async function choose(row: Models.app.CandidateRow) {
  const owner = edit.requestOwner()
  await run(async () => { const preview = await App.PreviewEditCandidate(row); if (alive && edit.acceptPreview(owner, preview)) { selectedCandidate.value = candidateKey(row); showTracklist.value = false } })
}
async function fetchPreview() {
  const current = edit.preview.value
  if (!current?.releaseIdentity) return
  const owner = edit.requestOwner()
  await run(async () => { const preview = await App.LoadEditUpstream(current.releaseIdentity!.provider, current.releaseIdentity!.ref, current.trackIdentity?.ref ?? '', region.value, true); if (alive) edit.acceptPreview(owner, preview) })
}
async function refreshBoundUpstream() {
  const track = boundTrack.value, release = boundRelease.value
  if (!track || !release) return
  const owner = edit.requestOwner()
  await run(async () => {
    const preview = await App.LoadEditUpstream(release.provider, release.ref, track.ref, region.value, true)
    if (!alive || !edit.ownsRequest(owner)) return
    edit.bindingAction.value = 'set'
    edit.refreshBinding(preview)
    edit.mappings.value = []
  })
}
function openBinding() {
  bindingSource.value = boundTrack.value?.provider ?? boundRelease.value?.provider ?? source.value
  bindingRegion.value = region.value; trackRef.value = ''; releaseRef.value = ''; bindingParsed.value = null; error.value = ''; bindingOpen.value = true
}
async function parseBinding() {
  source.value = bindingSource.value
  const owner = edit.requestOwner()
  await run(async () => {
    const preview = await App.LoadEditUpstream(bindingSource.value, releaseRef.value.trim(), trackRef.value.trim(), bindingRegion.value, true)
    if (alive && edit.acceptPreview(owner, preview)) { bindingParsed.value = preview; selectedCandidate.value = '' }
  })
}
function undoBinding() {
  edit.bindingAction.value = 'keep'; edit.bindingPreview.value = null; edit.previewId.value = ''; edit.upstream.value = copy(edit.baseSession.value?.upstream ?? {}); edit.mappings.value = []
}
async function listVersions() {
  const group = previewGroup.value
  if (!group) return
  const owner = edit.requestOwner()
  await run(async () => {
    const page = await App.ListEditVersions(group.provider, group.ref, versions.value.length)
    if (alive && edit.ownsRequest(owner)) { versions.value.push(...(page.items ?? [])); versionTotal.value = page.total ?? null; versionsLoaded.value = true }
  })
}
async function toggleVersions() { showVersions.value = !showVersions.value; if (showVersions.value && !versionsLoaded.value) await listVersions() }
async function chooseVersion(ref: string) {
  const owner = edit.requestOwner()
  await run(async () => { const preview = await App.LoadEditUpstream(edit.preview.value?.releaseIdentity?.provider ?? source.value, ref, '', region.value, false); if (alive && edit.acceptPreview(owner, preview)) { showTracklist.value = true; selectedCandidate.value = '' } })
}
async function chooseTrack(ref: string) {
  const owner = edit.requestOwner()
  await run(async () => { const preview = await App.LoadEditUpstream(edit.preview.value?.releaseIdentity?.provider ?? source.value, edit.preview.value?.releaseIdentity?.ref ?? '', ref, region.value, false); if (alive) edit.acceptPreview(owner, preview) })
}
async function pickCover() {
  await run(async () => { const picked = await App.PickEditCover(); if (!picked) return; tokens.add(picked.token); edit.coverToken.value = picked.token; edit.coverAction.value = 'replace'; coverURL.value = picked.previewUrl })
}
async function applyUpstreamCover() {
  await run(async () => {
    if (upstreamArtwork.value?.state === 'empty') { edit.coverAction.value = 'clear'; edit.coverToken.value = ''; return }
    const preview = edit.bindingPreview.value ?? await App.PreviewCurrentEditUpstream(props.trackId)
    if (!preview) return
    const cover = await stageCover(preview)
    if (cover) { edit.coverToken.value = cover.token; edit.coverAction.value = 'replace'; coverURL.value = cover.previewUrl }
  })
}
async function refreshImpact() {
  const owner = ++impactGeneration; impactPending.value = true
  try { const plan = await App.PlanAlbumChange(edit.draft()); if (owner !== impactGeneration || !alive) return; albumPlan.value = plan; if (!focusId.value) focusId.value = plan.rows[0]?.trackId ?? 0 }
  catch (e) { if (owner === impactGeneration) { error.value = String(e); albumPlan.value = null } }
  finally { if (owner === impactGeneration) impactPending.value = false }
}
async function chooseAlbumAction(action: 'create' | 'update') {
  edit.albumAction.value = action
  albumPrompt.value = false
  await run(async () => {
    const impact = await App.PreviewEditSave(edit.draft())
    if (impact.needsAlbumConfirmation) { impactOpen.value = true; await refreshImpact() }
    else await commit()
  })
}
function mapping(id: number) { return edit.mappings.value.find(row => row.trackId === id) }
function updateMapping(id: number, patch: Partial<Models.domain.EditMapping>) {
  const row = albumPlan.value?.rows.find(row => row.trackId === id)
  if (!row) return
  const existing = mapping(id) ?? { trackId: id, selected: false, targetRef: row.choices.length === 1 ? row.choices[0]!.ref : '', discNumber: valueOf(row.before['track.discNumber']), trackNumber: valueOf(row.before['track.trackNumber']) } as Models.domain.EditMapping
  edit.mappings.value = [...edit.mappings.value.filter(row => row.trackId !== id), { ...existing, ...patch }]; void refreshImpact()
}
watch(() => JSON.stringify([edit.fields.value, edit.bindingAction.value, edit.coverAction.value, edit.coverToken.value]), () => { albumPlan.value = null; if (impactOpen.value) void refreshImpact() })
async function save() { exitPrompt.value = false; edit.albumAction.value = 'create'; await run(async () => { const impact = await App.PreviewEditSave(edit.draft()); saveImpact.value = impact; if (impact.sharedChange) { albumPrompt.value = true; return } await commit() }) }
async function commit() { const saved = await admission.run(() => App.PlanEditSave(edit.draft()), App.SaveEdit); if (saved) { emit('saved'); emit('close') } }
async function confirmImpact() { if (albumPlan.value?.canSubmit) await run(commit) }
async function rebase() {
  await run(async () => { const session = await App.GetEditSession(props.trackId); edit.rebase(session.editor!); const binding = edit.bindingPreview.value; if (edit.bindingAction.value === 'set' && binding) { const next = await App.LoadEditUpstream(binding.trackIdentity?.provider ?? source.value, binding.releaseIdentity?.ref ?? '', binding.trackIdentity?.ref ?? '', region.value, true); edit.refreshBinding(next) } if (impactOpen.value) await refreshImpact() })
}
</script>
<template>
  <Dialog :visible="true" modal :closable="!loading" :close-on-escape="!loading" header="编辑曲目" aria-label="编辑曲目" :dismissable-mask="false" class="track-editor" :style="{ width: 'min(1280px,96vw)', height: 'min(860px,92vh)' }" @update:visible="close">
    <template #header><div class="flex min-w-0 items-center gap-3"><span class="font-semibold">编辑曲目</span><span class="truncate text-sm text-muted-color">{{ currentTitle }}</span></div></template>
    <div v-if="edit.baseSession.value" class="editor-layout">
      <section class="upstream-pane editor-pane" aria-label="上游信息">
        <div class="pane-heading"><div><h2>上游信息</h2><p>预览候选，确认使用后填入右侧</p></div><span class="status-chip">只读预览</span></div>
        <form class="space-y-3" @submit.prevent="search">
          <div class="flex items-center gap-2">
            <Select v-model="source" :disabled="loading" :options="settings.providers" option-label="displayName" option-value="id" aria-label="上游来源" class="min-w-0 flex-1" />
            <Button class="upstream-action" type="submit" aria-label="搜索候选" :disabled="loading || !settings.outboundAllowed"><Search class="size-4" />搜索</Button>
            <Button class="upstream-action" type="button" aria-label="使用" :aria-busy="preparingUse" :disabled="!edit.preview.value?.trackIdentity || loading" @click="confirmUse()"><LoaderCircle v-if="preparingUse" class="size-4 shrink-0 animate-spin" /><Check v-else class="size-4 shrink-0" aria-hidden="true" /><span>{{ preparingUse ? '获取中…' : '使用' }}</span></Button>
          </div>
          <div class="search-fields grid grid-cols-3 gap-2">
            <label>曲名<InputText v-model="searchTitle" aria-label="搜索曲名" placeholder="曲名" class="mt-1 w-full min-w-0" /></label>
            <label>艺术家<InputText v-model="searchArtist" aria-label="搜索艺术家" placeholder="艺术家" class="mt-1 w-full min-w-0" /></label>
            <label>专辑名<InputText v-model="searchAlbum" aria-label="搜索专辑" placeholder="专辑名" class="mt-1 w-full min-w-0" /></label>
          </div>
          <label v-if="settings.providerById(source)?.needsCountry" class="flex items-center gap-2 text-xs text-muted-color">地区<InputText v-model="region" aria-label="地区" placeholder="地区" class="min-w-0 flex-1" /></label>
        </form>
        <p v-if="!settings.outboundAllowed" class="rounded-md border px-3 py-2 text-xs text-muted-color">{{ settings.outboundReason }}；仍可离线编辑。</p>
        <section v-if="searched" class="preview-card">
          <button class="disclosure" :aria-expanded="showCandidates" @click="showCandidates = !showCandidates"><span>搜索结果 <span class="text-muted-color">{{ candidates.length }}</span></span><ChevronDown class="size-4" :class="{ '-rotate-90': !showCandidates }" /></button>
          <div v-if="showCandidates" class="max-h-56 overflow-y-auto border-t p-1">
            <p v-if="!candidates.length" class="px-3 py-5 text-center text-sm text-muted-color">未找到候选，请调整搜索条件。</p>
            <button v-for="(row, i) in candidates" :key="candidateKey(row) + i" :disabled="loading" :aria-pressed="selectedCandidate === candidateKey(row)" class="candidate-row" :class="{ selected: selectedCandidate === candidateKey(row) }" @click="choose(row)">
              <span class="min-w-0 flex-1"><span class="block truncate font-medium">{{ row.title || '未命名歌曲' }} · {{ row.artist || '未知署名' }}</span><span class="mt-1 block text-xs text-muted-color">{{ row.album || '未知专辑' }}<template v-if="capabilities.includes('Country') && row.country"> · {{ row.country }}</template><template v-if="capabilities.includes('MediumFormat') && row.mediumFormat"> · {{ row.mediumFormat }}</template><template v-if="row.releaseTrackTotal"> · {{ row.releaseTrackTotal }} 首</template></span><span v-if="row.localTrackId" class="mt-1 block text-xs text-muted-color">已在库中</span></span>
              <span class="flex shrink-0 items-center gap-1 text-xs text-muted-color"><template v-if="Number.isFinite(row.confidence)">{{ Math.round(row.confidence) }} 分</template><Check v-if="selectedCandidate === candidateKey(row)" class="size-4" /></span>
            </button>
          </div>
        </section>
        <template v-if="edit.preview.value">
          <div class="flex items-center justify-between gap-2 text-xs text-muted-color"><span>{{ providerName(edit.preview.value.releaseIdentity?.provider) }} · {{ selectedCandidate ? '所选候选' : '上游预览' }}</span><button :disabled="loading || !settings.outboundAllowed || !edit.preview.value.releaseIdentity" class="inline-flex items-center gap-1 disabled:opacity-40" @click="fetchPreview"><RefreshCw class="size-3.5" />获取完整预览</button></div>
          <section class="preview-card p-4" aria-label="上游歌曲信息">
            <h3 class="section-heading"><Music2 class="size-4" />歌曲信息</h3>
            <p class="mb-4 mt-3 break-words text-lg font-semibold">{{ fieldText(edit.preview.value.fields['track.title']) }}</p>
            <dl class="preview-facts"><template v-for="key in previewTrackKeys" :key="key"><dt>{{ labels[key] }}</dt><dd>{{ fieldText(edit.preview.value.fields[key], key) }}</dd></template></dl>
          </section>
          <section class="preview-card overflow-hidden" aria-label="上游专辑信息">
            <div class="p-4">
              <h3 class="section-heading"><Disc3 class="size-4" />专辑信息</h3>
              <div class="my-4 flex items-start gap-3">
                <img v-if="previewCovers[edit.preview.value.id]" :src="previewCovers[edit.preview.value.id]!.previewUrl" alt="上游封面预览" class="size-20 shrink-0 rounded-md border object-contain" />
                <button v-else class="flex size-20 shrink-0 flex-col items-center justify-center gap-2 rounded-md border bg-app-chrome text-xs text-muted-color disabled:cursor-default" :disabled="loading || edit.preview.value.fields['release.artwork']?.state !== 'value'" aria-label="查看上游封面" @click="viewPreviewCover"><ImageIcon class="size-6" /><span>{{ edit.preview.value.fields['release.artwork']?.state === 'value' ? '查看封面' : '暂无封面' }}</span></button>
                <div class="min-w-0"><p class="break-words font-semibold">{{ fieldText(edit.preview.value.fields['release.title']) }}</p><p class="mt-1 break-words text-sm text-muted-color">{{ fieldText(edit.preview.value.fields['release.credit']) }}</p><p class="mt-2 text-xs text-muted-color">{{ fieldText(edit.preview.value.fields['release.date']) }}<template v-if="edit.preview.value.fields['release.country']?.state === 'value'"> · {{ fieldText(edit.preview.value.fields['release.country']) }}</template></p></div>
              </div>
              <dl class="preview-facts"><template v-for="key in previewReleaseKeys" :key="key"><dt>{{ labels[key] }}</dt><dd>{{ fieldText(edit.preview.value.fields[key], key) }}</dd></template><template v-if="snapshot?.group?.title?.state === 'value'"><dt>所属发行组</dt><dd>{{ display(snapshot.group.title) }}</dd></template></dl>
            </div>
            <div v-if="settings.providerById(source)?.supportsVersions && previewGroup" class="border-t">
              <button class="disclosure" :disabled="loading || !settings.outboundAllowed" :aria-expanded="showVersions" @click="toggleVersions"><span>浏览版本<span v-if="versionTotal !== null" class="ml-1 text-muted-color">{{ versionTotal }}</span></span><ChevronDown class="size-4" :class="{ '-rotate-90': !showVersions }" /></button>
              <div v-if="showVersions" class="max-h-52 overflow-y-auto px-2 pb-2">
                <p v-if="versionsLoaded && !versions.length" class="p-2 text-xs text-muted-color">没有可浏览的版本。</p>
                <button v-for="version in versions" :key="version.ref" class="candidate-row" :disabled="loading" :aria-pressed="version.ref === edit.preview.value.releaseIdentity?.ref" :class="{ selected: version.ref === edit.preview.value.releaseIdentity?.ref }" @click="chooseVersion(version.ref)"><span>{{ version.title || '未命名发行' }}<span class="mt-1 block text-xs text-muted-color">{{ version.country || '未知国家' }} · {{ version.trackTotal || '未知' }} 首</span></span></button>
                <SecondaryButton v-if="versions.length && (versionTotal === null || versions.length < versionTotal)" size="small" :disabled="loading || !settings.outboundAllowed" @click="listVersions">更多版本</SecondaryButton>
              </div>
            </div>
            <div class="border-t">
              <button class="disclosure" :aria-expanded="showTracklist" @click="showTracklist = !showTracklist"><span>{{ showTracklist ? '收起' : '展开' }}当前发行清单</span><ChevronDown class="size-4" :class="{ '-rotate-90': !showTracklist }" /></button>
              <div v-if="showTracklist" class="px-3 pb-3">
                <p v-if="!tracklist.length" class="py-2 text-xs text-muted-color">清单未提供，请点击获取完整预览。</p>
                <p v-else-if="snapshot?.release?.tracklistCompleteness !== 'complete'" class="pb-2 text-xs text-muted-color">当前清单尚不完整。</p>
                <div v-for="disc in tracklistDiscs" :key="disc.number"><p class="border-b py-2 text-xs font-medium text-muted-color">第 {{ disc.number }} 碟</p><button v-for="(row, i) in disc.rows" :key="i" class="tracklist-row" :disabled="loading || !settings.outboundAllowed || !row.track?.value?.ref" :aria-current="trackState(row) === '本曲' ? 'true' : undefined" @click="chooseTrack(row.track.value!.ref)"><span class="w-6 shrink-0 text-xs tabular-nums text-muted-color">{{ row.trackNumber }}</span><span class="min-w-0 flex-1 break-words">{{ display(row.title) }}</span><span class="shrink-0 text-[10px] text-muted-color">{{ trackState(row) }}</span></button></div>
              </div>
            </div>
          </section>
          <p v-if="!edit.preview.value.trackIdentity" class="text-xs text-muted-color">请在当前发行清单中选择对应歌曲后使用。</p>
        </template>
        <div v-else class="flex flex-col items-center gap-3 rounded-lg border border-dashed px-5 py-12 text-center text-muted-color"><Search class="size-7" /><p class="text-sm">搜索并选择上游歌曲</p><p class="text-xs">也可以直接在右侧编辑当前信息</p></div>
      </section>
      <section class="current-pane editor-pane" aria-label="当前信息">
        <div class="pane-heading"><div><h2>当前信息</h2><p>编辑当前值，或逐项应用上游信息</p></div><span v-if="edit.dirty.value" class="status-chip">未保存</span></div>
        <div class="binding-summary">
          <div class="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2"><Link2 class="size-4 text-muted-color" /><span class="text-xs">歌曲 · {{ identityLabel(boundTrack) }}</span><span class="text-xs text-muted-color">专辑 · {{ identityLabel(boundRelease) }}</span><button class="ml-auto text-xs font-medium underline underline-offset-4" :disabled="loading" @click="openBinding">修改绑定</button></div>
          <div class="mt-3 flex flex-wrap items-center gap-x-3 gap-y-2 text-xs text-muted-color"><span v-if="bindingStatus">{{ bindingStatus }}</span><button v-if="boundTrack && boundRelease" class="inline-flex items-center gap-1" :disabled="loading || !settings.outboundAllowed" @click="refreshBoundUpstream"><RefreshCw class="size-3" />重新获取</button><button v-if="boundTrack" :disabled="loading" @click="edit.clearBinding()">清除歌曲绑定</button><button v-if="boundRelease" :disabled="loading" @click="edit.clearBinding(true)">解除发行绑定</button><button v-if="edit.bindingAction.value !== 'keep'" class="inline-flex items-center gap-1" :disabled="loading" @click="undoBinding"><RotateCcw class="size-3" />撤销绑定修改</button></div>
        </div>
        <section v-for="group in groups" :key="group.label" class="field-section" :aria-label="`编辑${group.label}`">
          <h3 class="section-heading"><component :is="group.icon" class="size-4" />{{ group.label }}</h3>
          <p v-if="group.label === '专辑信息'" class="mt-2 text-xs leading-relaxed text-muted-color">当前发行另有 {{ edit.baseSession.value.otherReleaseTracks }} 首歌曲；同组其他版本另有 {{ edit.baseSession.value.otherGroupTracks }} 首。</p>
          <div class="edit-grid field-column-head"><span class="field-column-label">字段</span><span>当前值</span><span>上游值</span><span aria-hidden="true" /></div>
          <EditCompareField v-for="key in group.keys" :key="key" :field="key" :label="labels[key]!" :value="edit.fields.value[key]" :upstream="edit.upstream.value[key]" :changed="key in edit.changes.value" @change="edit.fields.value[key] = $event" @apply="applyField(key)" @undo="edit.undo(key)" @lyrics="lyricsOpen = true" />
          <p v-if="group.label === '歌曲信息'" class="mt-3 text-xs leading-relaxed text-muted-color">演唱者与歌曲署名独立；上游未知时仍可补录。</p>
        </section>
        <section class="space-y-3" aria-label="编辑封面">
          <h3 class="section-heading"><ImageIcon class="size-4" />封面<span v-if="edit.coverAction.value !== 'keep'" class="status-chip">未保存</span></h3>
          <div class="flex items-start gap-4">
            <img v-if="coverURL && edit.coverAction.value === 'replace'" :src="coverURL" alt="封面草稿" class="size-24 shrink-0 rounded-md border object-contain" />
            <CoverArt v-else-if="edit.coverAction.value !== 'clear' && currentCover" usage="detail" :source="{ releaseId: edit.baseSession.value.releaseId, revision: edit.baseSession.value.coverRevision }" aria-label="当前封面" class="size-24 shrink-0 rounded-md" />
            <div v-else class="flex size-24 shrink-0 flex-col items-center justify-center gap-2 rounded-md border bg-app-chrome text-xs text-muted-color"><ImageIcon class="size-6" />{{ edit.coverAction.value === 'clear' ? '保存后清除' : '暂无封面' }}</div>
            <div class="space-y-2"><div class="flex flex-wrap gap-2"><SecondaryButton size="small" :disabled="loading" @click="pickCover">选择本地封面</SecondaryButton><SecondaryButton v-if="upstreamArtwork && upstreamArtwork.state !== 'unknown'" size="small" :disabled="loading" @click="applyUpstreamCover"><ArrowLeft class="size-3.5" />应用上游封面</SecondaryButton></div><div class="flex flex-wrap gap-3 text-xs text-muted-color"><button :disabled="loading || (!currentCover && edit.coverAction.value !== 'replace')" @click="edit.coverAction.value = 'clear'; edit.coverToken.value = ''">清除封面</button><button v-if="edit.coverAction.value !== 'keep'" :disabled="loading" @click="edit.coverAction.value = 'keep'; edit.coverToken.value = ''">撤销封面修改</button></div><p class="text-xs text-muted-color">封面属于当前发行，保存后生效。</p></div>
          </div>
        </section>
        <details class="preview-card"><summary class="cursor-pointer px-3 py-3 text-sm font-medium">其他信息 <span class="ml-1 text-xs font-normal text-muted-color">只读</span></summary><dl v-if="readonlyFields.length" class="preview-facts border-t p-3"><template v-for="([key, field]) in readonlyFields" :key="key"><dt>{{ editorFactLabels[key] }}</dt><dd>{{ fieldText(field, key) }}</dd></template></dl><p v-else class="border-t p-3 text-xs text-muted-color">当前上游尚未提供其他信息。</p></details>
      </section>
    </div>
    <div v-else class="flex flex-1 items-center justify-center gap-2 text-sm text-muted-color"><LoaderCircle v-if="loading" class="size-5 animate-spin" />{{ loading ? '正在载入编辑信息…' : '编辑信息载入失败' }}</div>
    <template #footer>
      <div class="mr-auto min-w-0 flex-1 text-xs text-muted-color"><p v-if="error" role="alert" class="break-words text-danger-color">{{ error }} <button class="underline underline-offset-2" :disabled="loading" @click="rebase">重新预览</button></p><span v-else class="inline-flex items-center gap-2"><LoaderCircle v-if="loading" class="size-3.5 animate-spin" />{{ loading ? '正在处理…' : edit.dirty.value ? '修改将在保存后生效' : '尚未修改' }}</span></div>
      <SecondaryButton :disabled="loading" @click="close">取消</SecondaryButton><Button :disabled="loading || !edit.baseSession.value || !edit.dirty.value" @click="save">保存</Button>
    </template>
  </Dialog>
  <Dialog v-model:visible="bindingOpen" modal :closable="!loading" :close-on-escape="!loading" header="修改上游绑定" :style="{ width: 'min(560px,94vw)' }">
    <div class="space-y-4"><p class="text-sm text-muted-color">粘贴目标歌曲或专辑的链接，也可以输入标识。解析后先核对预览，再确认使用。</p><Select v-model="bindingSource" :disabled="loading" :options="settings.providers" option-label="displayName" option-value="id" aria-label="绑定来源" class="w-full" /><label class="block text-sm">歌曲链接或标识<InputText v-model="trackRef" :disabled="loading" aria-label="歌曲绑定" :placeholder="settings.providerById(bindingSource)?.refs?.trackHint" class="mt-1 w-full" /></label><label class="block text-sm">专辑链接或标识<InputText v-model="releaseRef" :disabled="loading" aria-label="发行绑定" :placeholder="settings.providerById(bindingSource)?.refs?.releaseHint" class="mt-1 w-full" /></label><InputText v-if="settings.providerById(bindingSource)?.needsCountry" v-model="bindingRegion" :disabled="loading" aria-label="绑定地区" placeholder="地区" class="w-full" /><div v-if="bindingParsed" class="rounded-md border p-3 text-sm"><p class="font-medium">{{ fieldText(bindingParsed.fields['track.title']) }}</p><p class="mt-1 text-muted-color">{{ fieldText(bindingParsed.fields['release.title']) }} · {{ fieldText(bindingParsed.fields['release.credit']) }}</p><p v-if="!bindingParsed.trackIdentity" class="mt-2 text-xs text-muted-color">已解析专辑，请返回左侧清单选择歌曲。</p></div><p v-if="error" role="alert" class="text-sm text-danger-color">{{ error }}</p></div>
    <template #footer><SecondaryButton :disabled="loading" @click="bindingOpen = false">取消</SecondaryButton><SecondaryButton v-if="bindingParsed?.trackIdentity" :disabled="loading" @click="confirmUse(false)">仅更换绑定</SecondaryButton><Button v-if="bindingParsed?.trackIdentity" :disabled="loading" @click="confirmUse()">使用</Button><Button v-else :disabled="loading || !settings.outboundAllowed || (!trackRef.trim() && !releaseRef.trim())" @click="parseBinding">解析并预览</Button></template>
  </Dialog>
  <Dialog :visible="!!useConfirmation" modal :closable="!loading" :close-on-escape="!loading" :header="useValues ? '使用当前上游信息' : '更换绑定'" :style="{ width: 'min(520px,94vw)' }" @update:visible="useConfirmation = null">
    <div v-if="useConfirmation" class="mb-4 rounded-md border bg-app-chrome p-3"><p class="font-medium">{{ fieldText(useConfirmation.fields['track.title']) }}</p><p class="mt-1 text-sm text-muted-color">{{ fieldText(useConfirmation.fields['release.title']) }}</p></div><p class="text-sm leading-relaxed">{{ useValues ? '将把当前上游提供的歌曲和专辑信息全部填入当前编辑值，并替换已有的手工修改。未提供的信息将保留；上游明确为空的字段将清空。' : '仅替换绑定，保留当前编辑值。' }}修改只在点击保存后生效。</p><p v-if="error" role="alert" class="mt-3 text-sm text-danger-color">{{ error }}</p><template #footer><SecondaryButton :disabled="loading" @click="useConfirmation = null">取消</SecondaryButton><Button :disabled="loading" @click="applyUse">确认使用</Button></template>
  </Dialog>
  <Dialog v-model:visible="lyricsOpen" modal header="编辑歌词" :style="{ width: 'min(960px,94vw)' }">
    <div class="mb-4 flex flex-wrap items-center justify-between gap-2"><p class="text-xs text-muted-color">歌词保留原文及 LRC 时间标签，关闭后保留在本次编辑草稿中。</p><label class="flex items-center gap-2 text-xs"><input v-model="lyricsPlain" type="checkbox" />预览正文</label></div>
    <div class="grid gap-4 sm:grid-cols-2"><section><h3 class="mb-2 text-sm font-medium">当前值</h3><pre v-if="lyricsPlain" class="lyrics-content">{{ lyricsText(valueOf(edit.fields.value['track.lyrics'])) || '暂无歌词' }}</pre><textarea v-else :value="valueOf(edit.fields.value['track.lyrics']) ?? ''" aria-label="歌词" placeholder="输入歌词，支持 LRC 格式" class="lyrics-content w-full resize-none outline-none focus:border-primary" @input="edit.set('track.lyrics', ($event.target as HTMLTextAreaElement).value || null)" /></section><section><h3 class="mb-2 text-sm font-medium">上游值</h3><pre class="lyrics-content bg-app-chrome">{{ lyricsPlain ? lyricsText(valueOf(edit.upstream.value['track.lyrics'])) || fieldText(edit.upstream.value['track.lyrics']) : fieldText(edit.upstream.value['track.lyrics']) }}</pre></section></div>
    <template #footer><SecondaryButton :disabled="!edit.upstream.value['track.lyrics'] || edit.upstream.value['track.lyrics']?.state === 'unknown' || sameEditorValue(edit.fields.value['track.lyrics'], edit.upstream.value['track.lyrics'])" @click="applyField('track.lyrics')"><ArrowLeft class="size-4" />应用上游歌词</SecondaryButton><SecondaryButton @click="edit.set('track.lyrics', null)">清空歌词</SecondaryButton><SecondaryButton v-if="'track.lyrics' in edit.changes.value" @click="edit.undo('track.lyrics')">撤销歌词修改</SecondaryButton><Button @click="lyricsOpen = false">完成</Button></template>
  </Dialog>
 <Dialog v-model:visible="albumPrompt" modal header="专辑信息有改动" :style="{width:'min(540px,94vw)'}">
  <p v-if="saveImpact?.linksNewUpstream">已切换专辑上游，请新建专辑信息并逐曲确认迁入。新专辑保留所选上游绑定；目标专辑已在库时复用，保留其已有信息。</p>
  <div v-else class="space-y-2"><p>修改专辑信息：保存到当前专辑，保留上游绑定；该专辑关联的所有歌曲共享更新后的专辑信息。</p><p>新建专辑信息：保存为无上游绑定的本地专辑，并逐曲确认迁入。</p></div>
  <p v-if="albumUpdateReason" class="mt-2 text-sm text-muted-color">{{ albumUpdateReason }}</p>
  <template #footer><div class="flex flex-wrap justify-end gap-2"><Button :disabled="loading" @click="chooseAlbumAction('create')">新建专辑信息</Button><Button v-if="!saveImpact?.linksNewUpstream" :disabled="loading || !!albumUpdateReason" @click="chooseAlbumAction('update')">修改专辑信息</Button><SecondaryButton :disabled="loading" @click="albumPrompt=false">取消</SecondaryButton></div></template>
 </Dialog>
 <Dialog v-model:visible="impactOpen" :closable="!loading" :close-on-escape="!loading" modal header="确认迁入歌曲" :dismissable-mask="false" :style="{width:'min(1100px,96vw)'}">
  <p v-if="albumPlan?.impact?.preservesTargetValues" class="mb-3">复用在库发行，保留目标当前专辑信息。</p>
  <div v-if="albumPlan" class="grid gap-5 md:grid-cols-[42%_1fr]">
   <section aria-label="原发行全部收藏" class="max-h-[60vh] space-y-1 overflow-y-auto"><button v-for="row in albumPlan.rows" :key="row.trackId" :aria-label="`查看${fieldText(row.before['track.title'])}`" :aria-current="row.trackId===focusId?'true':undefined" class="flex w-full items-start gap-2 rounded border p-3 text-left" :class="row.stay?'text-muted-color':'text-green-600 dark:text-green-400'" @click="focusId=row.trackId"><span class="flex-1">{{fieldText(row.before['track.title'])}}<span class="block text-xs">{{fieldText(row.before['track.credit'])}} · {{fieldText(row.before['track.discNumber'])}}-{{fieldText(row.before['track.trackNumber'])}} · {{row.stay?'未确认迁入':'已确认迁入'}}</span></span><CircleAlert v-if="row.conflict" :aria-label="row.conflict" :title="row.conflict" class="size-5 shrink-0 text-danger-color"/></button></section>
   <section v-if="selectedRow" aria-label="歌曲迁入方案" class="max-h-[60vh] space-y-3 overflow-y-auto">
    <label class="flex gap-2 font-medium"><input type="checkbox" :checked="!!mapping(selectedRow.trackId)?.selected" @change="updateMapping(selectedRow.trackId,{selected:($event.target as HTMLInputElement).checked})"/>将此曲加入新建专辑</label>
    <template v-if="albumPlan.impact?.linksNewUpstream"><label>目标清单对应<select class="mt-1 w-full rounded border p-2" :value="mapping(selectedRow.trackId)?.targetRef??''" @change="updateMapping(selectedRow.trackId,{targetRef:($event.target as HTMLSelectElement).value})"><option value="">请选择对应</option><option v-for="target in albumPlan.targets" :key="target.ref" :value="target.ref">{{target.discNumber}}-{{target.trackNumber}} {{target.title}}</option></select></label><p class="text-xs">碟号和音轨号采用所选清单位置。</p></template>
    <div v-else class="flex gap-3"><label v-for="(label,key) in {discNumber:'碟号',trackNumber:'音轨号'}" :key="key">{{label}}<input type="number" min="1" step="1" :aria-label="`迁入${label}`" class="mt-1 block w-24 rounded border p-2" :value="(mapping(selectedRow.trackId)?mapping(selectedRow.trackId)![key]:valueOf(selectedRow.before['track.'+key]))??''" placeholder="未知" @input="updateMapping(selectedRow.trackId,{[key]:($event.target as HTMLInputElement).value===''?null:Number(($event.target as HTMLInputElement).value)})"/></label></div>
    <p v-if="selectedRow.conflict" role="alert" class="text-danger-color">{{selectedRow.conflict}}</p><p>{{selectedRow.evidence}}</p>
    <p class="text-xs text-muted-color">{{identityChange(selectedRow)}}</p>
    <div v-for="key in editableKeys" :key="key" class="grid grid-cols-[5rem_1fr_auto_1fr] gap-2 border-b py-2 text-sm"><span>{{labels[key]}}</span><span class="break-words">{{fieldText(selectedRow.before[key])}}</span><ArrowRight class="size-4"/><span class="break-words">{{fieldText(selectedRow.after[key])}}</span></div>
   </section>
  </div><p v-else>正在准备影响方案…</p>
  <template #footer><p v-if="error" role="alert" class="mr-auto text-sm text-danger-color">{{error}}</p><SecondaryButton :disabled="loading" @click="impactOpen=false">取消</SecondaryButton><Button :disabled="loading||impactPending||!albumPlan?.canSubmit" @click="confirmImpact">确认影响并继续</Button></template>
 </Dialog>
 <Dialog v-model:visible="exitPrompt" modal header="有未保存的修改" :style="{width:'min(480px,94vw)'}"><p>是否保存本次编辑？</p><template #footer><SecondaryButton @click="exitPrompt=false">继续编辑</SecondaryButton><SecondaryButton @click="emit('close')">不保存</SecondaryButton><Button @click="save">保存</Button></template></Dialog>
</template>

<style scoped>
:global(.track-editor > [data-pc-section="content"]) { display: flex; flex: 1; flex-direction: column; min-height: 0; overflow: hidden; padding: 0; }
:global(.track-editor > [data-pc-section="header"]) { border-bottom: 1px solid var(--p-content-border-color); }
:global(.track-editor > [data-pc-section="footer"]) { border-top: 1px solid var(--p-content-border-color); padding: 12px 20px; flex-wrap: wrap; align-items: center; }

.editor-layout { display: grid; grid-template-columns: minmax(0, 42fr) minmax(0, 58fr); flex: 1; min-height: 0; overflow: hidden; }
.editor-pane { min-width: 0; overflow-y: auto; padding: 22px 24px; scrollbar-gutter: stable; }
.editor-pane > * + * { margin-top: 20px; }
.upstream-pane { border-right: 1px solid var(--p-content-border-color); background: var(--p-app-chrome-background); }
.upstream-action { width: 85px; height: 35px; padding: 0; flex-shrink: 0; }
.current-pane { container: editor-fields / inline-size; }
.pane-heading { display: flex; align-items: start; justify-content: space-between; gap: 12px; }
.pane-heading h2 { font-size: 15px; font-weight: 600; }
.pane-heading p { margin-top: 5px; font-size: 12px; color: var(--p-text-muted-color); }
.status-chip { flex-shrink: 0; border: 1px solid var(--p-content-border-color); border-radius: 4px; padding: 2px 6px; font-size: 10px; color: var(--p-text-muted-color); }
.search-fields label { min-width: 0; font-size: 12px; color: var(--p-text-muted-color); }
.preview-card { border: 1px solid var(--p-content-border-color); border-radius: 8px; background: var(--p-app-background); }
.section-heading { display: flex; align-items: center; gap: 8px; font-size: 13px; font-weight: 600; }
.section-heading svg { color: var(--p-text-muted-color); }
.preview-facts { display: grid; grid-template-columns: 86px minmax(0, 1fr); gap: 10px 12px; font-size: 12px; line-height: 1.6; }
.preview-facts dt { color: var(--p-text-muted-color); }
.preview-facts dd { overflow-wrap: anywhere; }
.disclosure { display: flex; width: 100%; align-items: center; justify-content: space-between; gap: 8px; padding: 12px; font-size: 12px; font-weight: 500; cursor: pointer; }
.disclosure:disabled { opacity: .5; cursor: default; }
.candidate-row { display: flex; width: 100%; align-items: start; gap: 8px; border-radius: 5px; padding: 10px; text-align: left; font-size: 13px; cursor: pointer; }
.candidate-row:hover, .tracklist-row:hover { background: var(--p-content-hover-background); }
.candidate-row.selected { background: var(--p-highlight-background); }
.tracklist-row { display: flex; width: 100%; align-items: center; gap: 8px; border-radius: 4px; padding: 9px 4px; text-align: left; font-size: 12px; }
.tracklist-row[aria-current=true] { font-weight: 600; }
.binding-summary { border-bottom: 1px solid var(--p-content-border-color); padding-bottom: 16px; }
.edit-grid { display: grid; grid-template-columns: minmax(0, 1.1fr) minmax(0, 1fr) 54px; gap: 6px 12px; }
.field-column-head { margin-top: 14px; border-bottom: 1px solid var(--p-content-border-color); padding-bottom: 8px; font-size: 11px; color: var(--p-text-muted-color); }
.field-column-label { display: none; }
.lyrics-content { height: min(440px, 48vh); overflow-y: auto; white-space: pre-wrap; overflow-wrap: anywhere; border: 1px solid var(--p-content-border-color); border-radius: 6px; padding: 12px; font-family: inherit; font-size: 13px; line-height: 1.8; }
@container editor-fields (min-width: 600px) {
  .edit-grid { grid-template-columns: 70px minmax(0, 1.1fr) minmax(0, 1fr) 54px; }
  .field-column-label { display: block; }
}
@media (max-width: 959px) {
  .editor-layout { display: block; overflow-y: auto; }
  .editor-pane { overflow: visible; scrollbar-gutter: auto; padding: 20px; }
  .upstream-pane { border-right: 0; border-bottom: 1px solid var(--p-content-border-color); }
}
@media (max-width: 480px) {
  .editor-pane { padding: 16px; }
  .edit-grid { gap: 6px; grid-template-columns: minmax(0, 1fr) minmax(0, .8fr) 30px; }
  .pane-heading p { max-width: 20em; }
}
</style>
