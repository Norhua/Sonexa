<script setup lang="ts">
/**
 * 复核队列。
 *
 * 单一界面处置全部人工判断——疑似重复、跨发行组的多个高置信、无结果、失败。
 * 待选 / 匹配中 / 失败只是状态标签，界面结构完全一致。
 */
import { computed, onMounted, ref, watch } from 'vue'
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

const queue = ref<Models.main.ReviewQueue | null>(null)
const index = ref(0)
const loading = ref(false)
const error = ref('')
const expanded = ref(false)

/** 可改写的重新搜索表单。改写只影响本次查询，不动原始导入值。 */
const research = ref({ title: '', artist: '', album: '' })
/** 上游为单选下拉，默认 MusicBrainz。 */
const researchProvider = ref('musicbrainz')
const country = ref('')
const countries = ref<Models.main.CountryOption[]>([])
const hint = ref<Models.main.CountryHint | null>(null)
const researching = ref(false)
/** 本条目是否已经手动搜过一次——决定改选上游要不要自动重搜。 */
const searched = ref(false)

const current = computed(() => queue.value?.items[index.value] ?? null)

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

watch(current, (item) => {
  if (!item) return
  // 表单以原始导入值预填，但改写不回写
  research.value = {
    title: item.inTitle ?? '',
    artist: item.inArtist ?? '',
    album: item.inAlbum ?? '',
  }
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

async function doResearch() {
  if (!current.value) return
  researching.value = true
  error.value = ''
  try {
    const rows = await App.ResearchReviewItem(
      current.value.id, research.value.title, research.value.artist,
      research.value.album, researchProvider.value, country.value,
    )
    current.value.candidates = rows ?? []
    searched.value = true
  } catch (e) {
    error.value = String(e)
  } finally {
    researching.value = false
  }
}

async function act(fn: () => Promise<unknown>) {
  error.value = ''
  try {
    await fn()
    await load()
  } catch (e) {
    error.value = String(e)
  }
}

/** 接受：采用该上游候选入库并写入溯源。 */
function accept(row: Models.main.CandidateRow) {
  if (!current.value) return
  // 带本地徽章的候选走「合并」：不新建曲目
  if (row.localTrackId) {
    void act(() => App.MergeCandidate(current.value!.id, row.localTrackId))
    return
  }
  void act(() => App.AcceptCandidate(current.value!.id, row))
}

/** 空态的引导动作。上游搜索现在是搜索页的一种范围，不再是独立视图。 */
async function onEmptyAction(key: string) {
  if (key !== 'upstream') {
    await library.goTo('all')
    return
  }
  search.setScope(settings.providers[0]?.id ?? 'musicbrainz')
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
      <!-- 上半区：原始导入文本与条目动作 -->
      <section class="border-b p-4">
        <div class="flex items-start gap-3">
          <div class="min-w-0 flex-1">
            <p class="text-base font-medium">{{ display(current.inTitle) }}</p>
            <p class="mt-0.5 text-sm text-muted-color">
              {{ display(current.inArtist) }} · {{ display(current.inAlbum) }}
            </p>
            <p class="mt-0.5 text-xs text-muted-color">
              导入时的原始文本 · 来自 {{ current.batchTag }}
            </p>
          </div>
          <Tag
            class="shrink-0"
            :severity="current.status === 'failed' ? 'danger' : 'secondary'"
            :value="current.status === 'failed' ? '失败' : '待选'"
          />
        </div>

        <!--
          逐条浏览就放在条目动作的上面一行：它此前在窗口右上角，
          离用户的视线与手都太远——处置一条之后紧接着要用的就是「下一条」。
        -->
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

        <div class="mt-2 flex gap-2">
          <SecondaryButton
            variant="outlined"
            size="small"
            @click="act(() => App.NoMatchNeeded(current!.id))"
          >
            无需匹配
          </SecondaryButton>
          <!--
            「移出队列」而不是「删除记录」：它丢弃的是这条**待处置记录**，
            曲库里什么都不会少。叫「删除」会让人以为动的是曲目，
            因此按钮也不用 danger 色——它不是破坏性操作。
          -->
          <SecondaryButton
            variant="outlined"
            size="small"
            @click="act(() => App.DiscardReviewItem(current!.id))"
          >
            移出队列
          </SecondaryButton>
        </div>
      </section>

      <!-- 重新搜索 -->
      <section class="border-b p-4">
        <h3 class="mb-2 text-sm font-medium">重新搜索</h3>
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
        <p class="mt-2 text-xs text-muted-color">
          改搜只影响这次查询，不改动条目的原始导入值。
        </p>
        <!-- 已尝试变体：变体机制对用户是黑盒，不显示会让人困惑 -->
        <p v-if="current.triedVariants?.length" class="mt-1 text-xs text-muted-color">
          已尝试变体:
          <template v-for="(v, i) in current.triedVariants" :key="i">
            <span v-if="i > 0"> · </span>「{{ v }}」
          </template>
        </p>
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
