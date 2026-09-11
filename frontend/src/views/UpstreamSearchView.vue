<script setup lang="ts">
/**
 * 上游搜索页。独立入口与独立页面，不与库内搜索结果混排——
 * 两者的可用动作完全不同。
 */
import { computed, onMounted, ref, watch } from 'vue'
import { App, type Models } from '@/api'
import CandidateTable from '@/components/CandidateTable.vue'
import { useSettingsStore } from '@/stores/settings'

const settings = useSettingsStore()

const props = defineProps<{ prefill?: { title: string; artist: string; album: string } }>()
const emit = defineEmits<{ jumpLocal: [trackId: number] }>()

const form = ref({ title: '', artist: '', album: '' })
const selectedProviders = ref<string[]>([])
const country = ref('')
const countries = ref<Models.main.CountryOption[]>([])
const hint = ref<Models.main.CountryHint | null>(null)
/** 用户手动选过地区后不再被探测建议覆盖。 */
const countryTouched = ref(false)

const result = ref<Models.main.SearchResponse | null>(null)
const activeTab = ref('aggregate')
const loading = ref(false)
const error = ref('')

onMounted(async () => {
  countries.value = (await App.ListCountries()) ?? []
  // 上游勾选区从注册表动态渲染，不是硬编码的名单
  selectedProviders.value = settings.providers.map((p) => p.id)
  if (props.prefill) form.value = { ...props.prefill }
})

watch(
  () => props.prefill,
  (v) => {
    if (v) form.value = { ...v }
  },
)

/** 地区下拉仅在勾选了需要地区参数的上游时出现。 */
const showCountry = computed(() => settings.anyNeedsCountry(selectedProviders.value))

/** 各上游各自的能力，供候选表格判断某一行是否支持某列。 */
const providerCaps = computed(() => {
  const map: Record<string, string[]> = {}
  for (const p of settings.providers) map[p.id] = p.capabilities ?? []
  return map
})

const activeRows = computed(() => {
  if (!result.value) return []
  if (activeTab.value === 'aggregate') return result.value.aggregate
  return result.value.byProvider.find((p) => p.provider === activeTab.value)?.rows ?? []
})

const activeCaps = computed(() => {
  if (!result.value) return []
  if (activeTab.value === 'aggregate') return result.value.capabilities
  return result.value.byProvider.find((p) => p.provider === activeTab.value)?.capabilities ?? []
})

async function updateHint() {
  const text = `${form.value.title} ${form.value.artist} ${form.value.album}`.trim()
  if (!text) return
  hint.value = await App.SuggestCountry(text)
  // 探测结果仅作建议：用户已手动选择过就不覆盖
  if (hint.value?.detected && !countryTouched.value) {
    country.value = hint.value.country
  }
}

async function search() {
  error.value = ''
  loading.value = true
  result.value = null
  try {
    await updateHint()
    result.value = await App.SearchUpstream(
      form.value.title, form.value.artist, form.value.album,
      selectedProviders.value, country.value,
    )
    activeTab.value = 'aggregate'
    // 一页列表只发起一次批量预取，并发度由 Go 的池决定
    if (result.value?.aggregate?.length) {
      const refs = await App.BuildCoverRefs(result.value.aggregate)
      if (refs?.length) void App.PrefetchCovers(refs)
    }
  } catch (e) {
    error.value = String(e)
  } finally {
    loading.value = false
  }
}

async function addToLibrary(row: Models.main.CandidateRow) {
  try {
    await App.AddToLibrary(row)
    // 加入后该行动作随即变为「已在库中」
    row.action = '已在库中'
    if (!row.badges.includes('本地')) row.badges = ['本地', ...row.badges]
  } catch (e) {
    error.value = String(e)
  }
}
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div class="shrink-0 space-y-2 border-b p-4">
      <h2 class="text-base font-semibold">上游搜索</h2>

      <!-- 三个输入字段可任意留空 -->
      <div class="flex flex-wrap items-end gap-2">
        <label class="text-xs">曲名
          <input v-model="form.title" class="mt-1 block w-48 rounded border bg-background px-2 py-1 text-sm" />
        </label>
        <label class="text-xs">艺术家
          <input v-model="form.artist" class="mt-1 block w-40 rounded border bg-background px-2 py-1 text-sm" />
        </label>
        <label class="text-xs">专辑
          <input v-model="form.album" class="mt-1 block w-40 rounded border bg-background px-2 py-1 text-sm" />
        </label>
        <button
          class="rounded-md bg-primary px-3 py-1.5 text-sm text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
          :disabled="loading || !settings.outboundAllowed || selectedProviders.length === 0"
          :title="!settings.outboundAllowed ? settings.outboundReason : ''"
          @click="search"
        >
          {{ loading ? '搜索中…' : '搜索' }}
        </button>
      </div>

      <!-- 上游勾选区：从注册表动态渲染 -->
      <div class="flex flex-wrap items-center gap-3 text-xs">
        <span>上游</span>
        <label v-for="p in settings.providers" :key="p.id" class="flex items-center gap-1">
          <input v-model="selectedProviders" type="checkbox" :value="p.id" />
          {{ p.displayName }}
          <span v-if="!p.configured" class="text-muted-foreground" :title="p.reason">（未配置）</span>
        </label>

        <!-- 地区下拉仅在勾选 iTunes 时出现 -->
        <label v-if="showCountry" class="flex items-center gap-1">
          地区
          <select
            v-model="country"
            class="rounded border bg-background px-1.5 py-0.5"
            @change="countryTouched = true"
          >
            <option value="">跟随默认</option>
            <option v-for="c in countries" :key="c.code" :value="c.code">{{ c.name }}</option>
          </select>
        </label>
        <span v-if="showCountry && hint?.detected" class="text-muted-foreground">
          {{ hint.message }}
        </span>
      </div>
    </div>

    <!-- 标签页：聚合 + 每个被勾选上游各一个，各自独立 loading 与结果计数 -->
    <div v-if="result" class="flex shrink-0 gap-1 border-b px-4 py-1.5 text-sm">
      <button
        :class="['rounded px-2 py-1', activeTab === 'aggregate' ? 'bg-accent font-medium' : 'hover:bg-accent/50']"
        @click="activeTab = 'aggregate'"
      >
        聚合 ({{ result.aggregate.length }})
      </button>
      <button
        v-for="p in result.byProvider"
        :key="p.provider"
        :class="['rounded px-2 py-1', activeTab === p.provider ? 'bg-accent font-medium' : 'hover:bg-accent/50']"
        @click="activeTab = p.provider"
      >
        {{ p.displayName }}
        <span v-if="p.error" class="text-destructive">（失败）</span>
        <span v-else>({{ p.count }})</span>
      </button>
    </div>

    <div class="min-h-0 flex-1 overflow-auto">
      <p
        v-if="activeTab !== 'aggregate' && result?.byProvider.find((p) => p.provider === activeTab)?.error"
        class="m-4 rounded border border-destructive/40 bg-destructive/10 px-3 py-2 text-xs text-destructive"
      >
        {{ result.byProvider.find((p) => p.provider === activeTab)?.error }}
      </p>

      <CandidateTable
        v-if="result"
        :rows="activeRows"
        :capabilities="activeCaps"
        :provider-capabilities="providerCaps"
        @act="addToLibrary"
        @jump-local="(id) => emit('jumpLocal', id)"
      >
        <template #empty>没有搜到结果</template>
      </CandidateTable>

      <div v-else class="flex h-full items-center justify-center text-sm text-muted-foreground">
        填写条件后开始搜索（三个字段可任意留空）
      </div>
    </div>

    <p v-if="error" class="shrink-0 border-t bg-destructive/10 px-4 py-2 text-xs text-destructive">
      {{ error }}
    </p>
  </div>
</template>
