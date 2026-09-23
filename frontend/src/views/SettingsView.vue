<script setup lang="ts">
/**
 * 设置：常规、上游、匹配、导入导出、缓存、关于 六个分组。
 *
 * 以弹窗呈现（见 SettingsDialog）：设置不是浏览目的地，
 * 占用主内容区会把用户当前的浏览位置顶掉。
 *
 * 上游分组按注册表动态渲染，每个 provider 一个配置分组——
 * 不硬编码 provider 名称或数量。
 */
import { computed, onMounted, ref } from 'vue'
import { App, type Models } from '@/api'
import Button from '@/volt/Button.vue'
import Checkbox from '@/volt/Checkbox.vue'
import InputNumber from '@/volt/InputNumber.vue'
import InputText from '@/volt/InputText.vue'
import Select from '@/volt/Select.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import { useSettingsStore } from '@/stores/settings'
import { useThemeStore } from '@/stores/theme'

const settings = useSettingsStore()
const theme = useThemeStore()

const props = defineProps<{ anchor?: string }>()

const draft = ref<Models.config.Settings | null>(null)
const about = ref<Models.app.AboutInfo | null>(null)
const countries = ref<Models.app.CountryOption[]>([])
const saving = ref(false)
const message = ref('')
const error = ref('')

type GroupKey = 'general' | 'upstream' | 'matching' | 'io' | 'cache' | 'about'
const active = ref<GroupKey>((props.anchor as GroupKey) ?? 'general')

/** 主题与语言的取值域。写成常量而不是模板里硬编码的 option 列表。 */
const themeOptions = [
  { value: 'dark', label: '暗色' },
  { value: 'light', label: '亮色' },
]
const languageOptions = [{ value: 'zh-CN', label: '简体中文' }]
const exportFormats = [
  { value: 'csv', label: 'CSV' },
  { value: 'json', label: 'JSON' },
]
const variantStrategies = [
  { value: 'progressive', label: '渐进式（默认）' },
  { value: 'exhaustive', label: '全量' },
]
const tracklistTimings = [
  { value: 'on_album_open', label: '首次打开专辑时（默认）' },
  { value: 'on_match', label: '匹配时' },
]

/** 地区下拉：「跟随系统区域设置」置顶。 */
const countryOptions = computed(() => [
  { code: '', name: '跟随系统区域设置' },
  ...countries.value,
])

const groups: { key: GroupKey; label: string }[] = [
  { key: 'general', label: '常规' },
  { key: 'upstream', label: '上游' },
  { key: 'matching', label: '匹配' },
  { key: 'io', label: '导入导出' },
  { key: 'cache', label: '缓存' },
  { key: 'about', label: '关于' },
]

onMounted(async () => {
  draft.value = JSON.parse(JSON.stringify(settings.settings ?? (await App.GetSettings())))
  about.value = await App.About()
  countries.value = (await App.ListCountries()) ?? []
})

/** 联系方式必填——未填写时全局禁用出网。 */
const contactMissing = computed(() => !draft.value?.musicbrainz.contact?.trim())

async function save() {
  if (!draft.value) return
  // 主题由 theme store 持有（需同步读取以避免首帧闪白），
  // 保存时写回草稿，免得 settings.json 里留着一个与界面不符的值
  draft.value.general.theme = theme.theme
  saving.value = true
  error.value = ''
  message.value = ''
  try {
    await settings.save(draft.value)
    message.value = '已保存，改动立即生效'
  } catch (e) {
    error.value = String(e)
  } finally {
    saving.value = false
  }
}

async function restoreDefaults() {
  draft.value = await App.DefaultSettings()
}

async function clearCache() {
  error.value = ''
  try {
    await App.ClearCache()
    message.value = '缓存已清除（已采用的封面不受影响）'
  } catch (e) {
    error.value = String(e)
  }
}

defineExpose({ active })
</script>

<template>
  <div v-if="draft" class="flex min-h-0 flex-1 overflow-hidden">
    <!-- 分组导航 -->
    <nav class="w-32 shrink-0 overflow-auto border-r py-2">
      <button
        v-for="g in groups"
        :key="g.key"
        :class="[
          'block w-full px-3 py-1.5 text-left text-sm',
          active === g.key ? 'bg-highlight font-medium' : 'hover:bg-emphasis',
        ]"
        @click="active = g.key"
      >
        {{ g.label }}
      </button>
    </nav>

    <div class="min-h-0 flex-1 overflow-auto p-5">
      <!-- 常规 -->
      <section v-if="active === 'general'" class="max-w-lg space-y-4">
        <h2 class="text-base font-semibold">常规</h2>
        <!-- 主题切换的唯一入口。顶栏取消后，这里是唯一改主题的地方 -->
        <label class="block text-sm">主题
          <Select
            :model-value="theme.theme"
            :options="themeOptions"
            option-label="label"
            option-value="value"
            size="small"
            class="mt-1 block"
            @update:model-value="theme.set($event)"
          />
        </label>
        <label class="block text-sm">语言
          <Select
            v-model="draft.general.language"
            :options="languageOptions"
            option-label="label"
            option-value="value"
            size="small"
            class="mt-1 block"
          />
        </label>
        <p class="text-xs text-muted-color">
          数据库位置：由系统的用户配置目录决定，首次启动自动创建。
        </p>
      </section>

      <!-- 上游：按注册表动态渲染 -->
      <section v-if="active === 'upstream'" class="max-w-lg space-y-5">
        <h2 class="text-base font-semibold">上游</h2>

        <div
          v-for="p in settings.providers"
          :key="p.id"
          class="space-y-2 rounded border p-3"
        >
          <h3 class="text-sm font-medium">{{ p.displayName }}</h3>

          <template v-if="p.id === 'musicbrainz'">
            <label class="block text-sm">
              联系方式
              <span class="text-danger-color">*</span>
              <InputText
                v-model="draft.musicbrainz.contact"
                placeholder="邮箱或主页地址"
                size="small"
                class="mt-1 block w-full"
              />
            </label>
            <p v-if="contactMissing" class="text-xs text-danger-color">
              必填项。未填写时导入、上游搜索与封面获取全部禁用。
            </p>
            <label class="block text-sm">自定义服务器地址
              <InputText v-model="draft.musicbrainz.serverUrl" size="small" class="mt-1 block w-full" />
            </label>
            <label class="block text-sm">速率限制（请求/秒）
              <InputNumber
                v-model="draft.musicbrainz.ratePerSec"
                :step="0.1"
                :min="0.1"
                :max-fraction-digits="1"
                size="small"
                class="mt-1 block w-24"
              />
            </label>
          </template>

          <template v-else-if="p.id === 'itunes'">
            <label class="block text-sm">默认地区
              <Select
                v-model="draft.itunes.defaultCountry"
                :options="countryOptions"
                option-label="name"
                option-value="code"
                size="small"
                class="mt-1 block"
              />
            </label>
            <label class="block text-sm">速率限制（请求/分钟）
              <InputNumber
                v-model="draft.itunes.ratePerMin"
                :min="1"
                size="small"
                class="mt-1 block w-24"
              />
            </label>
          </template>

          <p v-else class="text-xs text-muted-color">该上游无需额外配置。</p>

          <p class="text-xs text-muted-color">
            能力：{{ p.capabilities?.join('、') || '—' }}
          </p>
        </div>

        <label class="block text-sm">上游优先级顺序
          <InputText
            :model-value="draft.providerPriority?.join(', ')"
            size="small"
            class="mt-1 block w-full"
            @update:model-value="draft.providerPriority = ($event ?? '').split(',').map((s: string) => s.trim()).filter(Boolean)"
          />
        </label>
      </section>

      <!-- 匹配 -->
      <section v-if="active === 'matching'" class="max-w-lg space-y-3">
        <h2 class="text-base font-semibold">匹配</h2>
        <label class="block text-sm">复核阈值
          <InputNumber v-model="draft.matching.reviewThreshold" size="small" class="mt-1 block w-24" />
        </label>
        <p class="text-xs text-muted-color">
          高置信是结构性条件（曲名匹配 且 署名或歌集精确匹配），不是分数线。
        </p>
        <label class="block text-sm">偏好国家列表
          <InputText
            :model-value="draft.matching.preferredCountries?.join(', ')"
            size="small"
            class="mt-1 block w-full"
            @update:model-value="draft.matching.preferredCountries = ($event ?? '').split(',').map((s: string) => s.trim()).filter(Boolean)"
          />
        </label>
        <label class="block text-sm">载体格式优先级
          <InputText
            :model-value="draft.matching.mediumPriority?.join(', ')"
            size="small"
            class="mt-1 block w-full"
            @update:model-value="draft.matching.mediumPriority = ($event ?? '').split(',').map((s: string) => s.trim()).filter(Boolean)"
          />
        </label>
        <label class="block text-sm">最大重试次数
          <InputNumber v-model="draft.matching.maxRetries" :min="1" size="small" class="mt-1 block w-20" />
        </label>
        <label class="block text-sm">退避基数（秒）
          <InputNumber
            v-model="draft.matching.backoffBaseSeconds"
            :min="1"
            size="small"
            class="mt-1 block w-20"
          />
        </label>
        <label class="block text-sm">查询变体策略
          <Select
            v-model="draft.matching.variantStrategy"
            :options="variantStrategies"
            option-label="label"
            option-value="value"
            size="small"
            class="mt-1 block"
          />
        </label>
        <label class="block text-sm">曲目清单拉取时机
          <Select
            v-model="draft.matching.tracklistTiming"
            :options="tracklistTimings"
            option-label="label"
            option-value="value"
            size="small"
            class="mt-1 block"
          />
        </label>
      </section>

      <!-- 导入导出 -->
      <section v-if="active === 'io'" class="max-w-lg space-y-3">
        <h2 class="text-base font-semibold">导入导出</h2>
        <label class="block text-sm">默认编码
          <InputText v-model="draft.importExport.defaultEncoding" size="small" class="mt-1 block w-32" />
        </label>
        <label class="block text-sm">默认分隔符
          <InputText v-model="draft.importExport.defaultDelimiter" size="small" class="mt-1 block w-32" />
        </label>
        <label class="block text-sm">多值分隔符
          <InputText v-model="draft.importExport.multiValueSep" size="small" class="mt-1 block w-32" />
        </label>
        <label class="block text-sm">导出格式
          <Select
            v-model="draft.importExport.exportFormat"
            :options="exportFormats"
            option-label="label"
            option-value="value"
            size="small"
            class="mt-1 block"
          />
        </label>
        <label class="flex items-center gap-2 text-sm">
          <Checkbox v-model="draft.importExport.exportUtf8Bom" binary />
          导出使用带 BOM 的 UTF-8（便于表格软件打开）
        </label>
      </section>

      <!-- 缓存 -->
      <section v-if="active === 'cache'" class="max-w-lg space-y-3">
        <h2 class="text-base font-semibold">缓存</h2>
        <label class="block text-sm">封面缓存目录
          <InputText
            v-model="draft.cache.coverDir"
            placeholder="留空则与数据库同级"
            size="small"
            class="mt-1 block w-full"
          />
        </label>
        <label class="block text-sm">封面缓存上限（字节）
          <InputNumber v-model="draft.cache.coverMaxBytes" size="small" class="mt-1 block w-40" />
        </label>
        <label class="block text-sm">封面并发数
          <InputNumber
            v-model="draft.cache.coverConcurrency"
            :min="1"
            :max="64"
            size="small"
            class="mt-1 block w-20"
          />
          <span class="ml-2 text-xs text-muted-color">CAA 不限制并发；网络较慢时可调高，上限 64</span>
        </label>
        <label class="block text-sm">上游响应缓存有效期（小时）
          <InputNumber
            v-model="draft.cache.providerCacheTtlHours"
            :min="1"
            size="small"
            class="mt-1 block w-24"
          />
        </label>
        <p class="text-xs text-muted-color">iTunes 响应、曲目清单和已采用图片不按保存日期失效。</p>
        <SecondaryButton variant="outlined" size="small" @click="clearCache">
          清除缓存
        </SecondaryButton>
        <p class="text-xs text-muted-color">
          清除封面与上游响应缓存；曲库数据与已确认采用的封面不受影响。
        </p>
      </section>

      <!-- 关于 -->
      <section v-if="active === 'about' && about" class="max-w-lg space-y-4">
        <h2 class="text-base font-semibold">关于</h2>
        <p class="text-sm">Sonexa {{ about.version }}</p>
        <div v-for="a in about.attributions" :key="a.name" class="space-y-1">
          <h3 class="text-sm font-medium">{{ a.name }}</h3>
          <p class="text-xs text-muted-color">{{ a.text }}</p>
          <p class="text-xs text-muted-color">{{ a.url }}</p>
        </div>
      </section>

      <div v-if="active !== 'about'" class="mt-6 flex items-center gap-2 border-t pt-4">
        <Button size="small" :disabled="saving" @click="save">
          {{ saving ? '保存中…' : '保存' }}
        </Button>
        <SecondaryButton variant="outlined" size="small" @click="restoreDefaults">
          恢复默认
        </SecondaryButton>
        <p v-if="message" class="text-xs text-muted-color">{{ message }}</p>
        <p v-if="error" class="text-xs text-danger-color">{{ error }}</p>
      </div>
    </div>
  </div>
</template>
