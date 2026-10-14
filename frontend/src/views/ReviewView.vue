<script setup lang="ts">
import { useAdmissionStore } from '@/stores/admission'
const admission = useAdmissionStore()
/**
 * 复核队列。
 *
 * 单一界面处置全部人工判断——疑似重复、跨发行组的多个高置信、无结果、失败。
 * 待选 / 匹配中 / 失败只是状态标签，界面结构完全一致。
 */
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { App, display, type Models } from '@/api'
import CandidateTable from '@/components/CandidateTable.vue'
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

const settings = useSettingsStore()
const tasks = useTasksStore()
const library = useLibraryStore()
const search = useSearchStore()

const emit = defineEmits<{ jumpLocal: [trackId: number]; goSettings: [] }>()

const queue = ref<Models.app.ReviewQueue | null>(null)
const index = ref(0)
const loading = ref(false)
const error = ref('')
const expanded = ref(false)

/** 可改写的重新搜索表单。改写只影响本次查询，不动原始导入值。 */
const research = ref({ title: '', artist: '', album: '' })
/** 上游为单选下拉，默认 MusicBrainz。 */
const researchProvider = ref('')
watch(() => settings.providers, providers => {
  if (!providers.some(p => p.id === researchProvider.value)) researchProvider.value = providers[0]?.id ?? ''
}, { immediate: true })
const country = ref('')
const countries = ref<Models.app.CountryOption[]>([])
const hint = ref<Models.app.CountryHint | null>(null)
const researching = ref(false)
/** 本条目是否已经手动搜过一次——决定改选上游要不要自动重搜。 */
const searched = ref(false)

const current = computed(() => queue.value?.items[index.value] ?? null)

const reviewReason = computed(() => ({
  new_release: '新增发行加入已有专辑组，接受后还需确认成员变化',
  group_insufficient: '判组资料不足，可按已知信息保存', group_conflict: '专辑组证据冲突，需要明确合组方案',
  unknown_total: '发行总曲数未知，需要人工选版', no_match: '没有高置信匹配', ambiguous: '候选存在歧义', duplicate: '可能与已有收藏重复',
}[current.value?.reviewReason ?? ''] ?? current.value?.reviewReason ?? ''))
const reviewEvidence = computed(() => {
  try { return JSON.parse(current.value?.reviewEvidence || 'null') as (Models.domain.AdmissionBaseline & { proposed?: Models.app.PlanReleaseView }) | null }
  catch { return null }
})

/** 地区下拉仅在选中需要地区参数的上游时出现。 */
const showCountry = computed(() => settings.anyNeedsCountry([researchProvider.value]))

const providerCaps = computed(() => {
  const map: Record<string, string[]> = {}
  for (const p of settings.providers) map[p.id] = p.capabilities ?? []
  return map
})

/** 当前选中上游的能力决定候选列表的列显示。 */
const activeCaps = computed(() => settings.providerById(researchProvider.value)?.capabilities ?? [])

onMounted(async () => {
  countries.value = (await App.ListCountries()) ?? []
  await load()
})

/**
 * 切换上游即重新检索。
 *
 * 此前改选上游不动结果：屏幕上还挂着上一个上游的候选，而「上游」下拉已经指向
 * 另一家——读起来像是这些候选来自新选的那个上游。
 *
 * 只在真的搜过一次之后才自动重搜：刚打开一个条目就因为默认值触发一次出网，
 * 会把 1 req/s 的配额花在用户还没提出的请求上。
 */
watch(researchProvider, async () => {
  if (!current.value) return
  if (!settings.outboundAllowed) return
  if (!searched.value) return
  await doResearch()
})

let researchRequest = 0
onBeforeUnmount(() => { researchRequest++ })

watch(current, (item) => {
  researchRequest++
  researching.value = false
  if (!item) return
  restoreOriginal()
  expanded.value = false
  // 换了条目就还没搜过：此时改选上游不该自动出网
  searched.value = false
  void updateHint()
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    queue.value = await App.GetReviewQueue()
    if (index.value >= (queue.value?.items.length ?? 0)) {
      index.value = Math.max(0, (queue.value?.items.length ?? 1) - 1)
    }
    await tasks.refresh()
  } catch (e) {
    error.value = String(e)
  } finally {
    loading.value = false
  }
}

/** 地区下拉的选项：「跟随默认」置顶。 */
const countryOptions = computed(() => [{ code: '', name: '跟随默认' }, ...countries.value])

async function updateHint() {
  const text = `${research.value.title} ${research.value.artist}`.trim()
  if (!text) return
  hint.value = await App.SuggestCountry(text)
  if (hint.value?.detected && !country.value) country.value = hint.value.country
}

/** 只恢复三个输入，不提交查询、不修改原始记录或上游/地区选择。 */
function restoreOriginal() {
  const item = current.value
  if (!item) return
  research.value = {
    title: item.inTitle ?? '',
    artist: item.inArtist ?? '',
    album: item.inAlbum ?? '',
  }
}

async function doResearch() {
  const item = current.value
  if (!item) return
  const request = ++researchRequest
  searched.value = true
  researching.value = true
  error.value = ''
  try {
    const rows = await App.ResearchReviewItem(
      item.id, research.value.title, research.value.artist,
      research.value.album, researchProvider.value, country.value,
    )
    if (request !== researchRequest) return
    item.candidates = rows ?? []
  } catch (e) {
    if (request === researchRequest) error.value = String(e)
  } finally {
    if (request === researchRequest) researching.value = false
  }
}

/** 只有调用方明确返回 false 才表示取消；Wails 空结果 null 仍代表操作成功。 */
async function act(fn: () => Promise<void | boolean>) {
  error.value = ''
  try {
    const result = await fn()
    if (result !== false) await load()
  } catch (e) {
    error.value = String(e)
  }
}

/** 接受：采用该上游候选入库并写入溯源。 */
function accept(row: Models.app.CandidateRow) {
  if (!current.value) return
  // 带本地徽章的候选走「合并」：不新建曲目
  if (row.localTrackId) {
    void act(() => App.MergeCandidate(current.value!.id, row.localTrackId))
    return
  }
  const itemId = current.value.id
  void act(async () => (await admission.run(() => App.PrepareAdmission(row, itemId))) !== null)
}

function noMatchNeeded() {
  const itemId = current.value?.id
  if (itemId) void act(async () => (await admission.run(() => App.PrepareNoMatchNeeded(itemId))) !== null)
}

/** 空态的引导动作。上游搜索现在是搜索页的一种范围，不再是独立视图。 */
async function onEmptyAction(key: string) {
  if (key !== 'upstream') {
    await library.goTo('all')
    return
  }
  search.setScope(settings.providers[0]?.id ?? 'library')
  await library.goTo('search')
}

function prev() {
  if (index.value > 0) index.value--
}
function next() {
  if (queue.value && index.value < queue.value.items.length - 1) index.value++
}
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <!-- 顶部：待处置总数与分项计数 -->
    <header class="flex h-12 shrink-0 items-center gap-3 px-4" style="--wails-draggable: drag">
      <h2 class="text-xl font-bold">复核队列</h2>
      <span v-if="queue" class="text-sm text-muted-color">
        {{ queue.total }} 条待处置 · 待选 {{ queue.pending }} · 失败 {{ queue.failed }}
      </span>
      <div class="flex-1" />
    </header>

    <EmptyState
      v-if="queue && queue.items.length === 0 && !loading"
      title="复核队列是空的"
      description="导入时无法自动判断的条目会出现在这里，逐条处置即可。"
      :actions="[{ label: '到全部曲目', key: 'all' }, { label: '到上游搜索', key: 'upstream' }]"
      @action="onEmptyAction"
    />

    <div v-else-if="current" class="min-h-0 flex-1 overflow-auto">
      <!-- 条目标题、来源批次与逐条浏览 -->
      <section class="border-b p-4">
        <div class="flex items-start gap-3">
          <div class="min-w-0 flex-1">
            <p class="text-base font-medium">{{ display(current.inTitle) }}</p>
            <p class="mt-0.5 text-xs text-muted-color">来自 {{ current.batchTag }}</p>
          </div>
          <Tag
            class="shrink-0"
            :severity="current.status === 'failed' ? 'danger' : 'secondary'"
            :value="current.status === 'failed' ? '失败' : '待选'"
          />
        </div>

        <div v-if="reviewReason" class="mt-3 space-y-2 rounded border p-3 text-xs">
          <p class="font-medium">{{ reviewReason }}</p>
          <div v-for="group in reviewEvidence?.resolution?.groups ?? []" :key="group.id">
            <p>已有专辑组：{{ group.title || '未命名专辑' }} · {{ display(group.credit) }}</p>
            <p v-for="member in group.members" :key="member.id" class="text-muted-color">已有发行：{{ member.title || '未命名发行' }} · {{ member.identity?.provider || '本地' }} · {{ display(member.identity?.ref) }} · 收藏 {{ member.trackIds?.length || 0 }} 首</p>
          </div>
          <p v-if="reviewEvidence?.proposed">拟加入发行：{{ reviewEvidence.proposed.title?.value || '未命名发行' }} · {{ reviewEvidence.proposed.identity?.provider }} · {{ reviewEvidence.proposed.identity?.ref }}</p>
          <p v-for="(e, i) in reviewEvidence?.resolution?.evidence ?? []" :key="i">判组依据：{{ e.kind === 'native' ? '相同上游组身份' : '专辑名与专辑艺术家相同' }} · {{ e.title }} · {{ e.credit }}</p>
        </div>
        <p v-if="!current.inTitle || !current.inArtist || !current.inAlbum" class="mt-2 text-xs text-muted-color">部分基本信息缺失，仍可选择候选或按已知原始信息无需匹配保存。</p>
        <!-- 逐条浏览保留在条目标题下方。 -->
        <div v-if="queue && queue.items.length > 0" class="mt-3 flex items-center gap-2 text-sm">
          <SecondaryButton variant="outlined" size="small" :disabled="index === 0" @click="prev">
            ← 上一条
          </SecondaryButton>
          <span class="tabular-nums text-muted-color">
            {{ index + 1 }} / {{ queue.items.length }}
          </span>
          <SecondaryButton
            variant="outlined"
            size="small"
            :disabled="index >= queue.items.length - 1"
            @click="next"
          >
            下一条 →
          </SecondaryButton>
        </div>
      </section>

      <!-- 重新搜索 -->
      <section class="border-b p-4">
        <div class="flex flex-wrap items-end gap-2">
          <label class="text-xs">曲名
            <InputText v-model="research.title" size="small" class="mt-1 block w-48" />
          </label>
          <label class="text-xs">艺术家
            <InputText v-model="research.artist" size="small" class="mt-1 block w-36" />
          </label>
          <label class="text-xs">专辑
            <InputText v-model="research.album" size="small" class="mt-1 block w-36" />
          </label>
          <!-- 上游单选下拉，从注册表渲染，默认 MusicBrainz -->
          <label class="text-xs">上游
            <Select
              v-model="researchProvider"
              :options="settings.providers"
              option-label="displayName"
              option-value="id"
              size="small"
              class="mt-1 block"
            />
          </label>
          <label v-if="showCountry" class="text-xs">地区
            <Select
              v-model="country"
              :options="countryOptions"
              option-label="name"
              option-value="code"
              size="small"
              class="mt-1 block"
            />
          </label>
          <Button
            size="small"
            :disabled="researching || !settings.outboundAllowed"
            :title="!settings.outboundAllowed ? settings.outboundReason : ''"
            @click="doResearch"
          >
            {{ researching ? '搜索中…' : '重新搜索' }}
          </Button>
        </div>
        <p v-if="showCountry && hint?.detected" class="mt-1 text-xs text-muted-color">
          {{ hint.message }}
        </p>
        <div class="mt-3 flex flex-wrap gap-2">
          <SecondaryButton variant="outlined" size="small" @click="restoreOriginal">
            恢复原始信息
          </SecondaryButton>
          <SecondaryButton variant="outlined" size="small" @click="noMatchNeeded">
            无需匹配
          </SecondaryButton>
          <SecondaryButton variant="outlined" size="small" @click="act(() => App.DiscardReviewItem(current!.id))">
            移出队列
          </SecondaryButton>
        </div>
      </section>

      <!-- 候选列表：与上游搜索共用同一组件 -->
      <section>
        <CandidateTable
          v-model:expanded="expanded"
          :rows="current.candidates"
          :capabilities="activeCaps"
          :provider-capabilities="providerCaps"
          :limit="10"
          :total="current.candidates.length"
          @act="accept"
          @jump-local="(id) => emit('jumpLocal', id)"
        >
          <template #empty>还没有候选结果，可改写条件或换上游再试</template>
          <template #footer-actions>
            <SecondaryButton
              variant="outlined"
              size="small"
              @click="act(() => App.NoneMatched(current!.id))"
            >
              无一匹配
            </SecondaryButton>
          </template>
        </CandidateTable>
      </section>

      <!-- 状态说明：仅失败状态显示 -->
      <section v-if="current.status === 'failed'" class="border-t p-4 text-sm">
        <h3 class="mb-1 font-medium">状态说明</h3>
        <p class="text-muted-color">
          已重试 {{ current.attemptCount }} 次
          <template v-if="current.lastError"> · 最后错误: {{ current.lastError }}</template>
          <template v-if="current.lastErrorAt">
            · {{ new Date(current.lastErrorAt).toLocaleTimeString() }}
          </template>
        </p>
        <div class="mt-2 flex gap-2">
          <SecondaryButton
            variant="outlined"
            size="small"
            @click="act(() => App.RetryReviewItem(current!.id))"
          >
            立即重试
          </SecondaryButton>
          <SecondaryButton variant="outlined" size="small" @click="library.goTo('importQueue')">
            查看日志
          </SecondaryButton>
        </div>
      </section>
    </div>

    <Message v-if="error" severity="error" size="small" class="shrink-0 rounded-none">
      {{ error }}
    </Message>
  </div>
</template>
