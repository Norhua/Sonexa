<script setup lang="ts">
/**
 * 导入向导。
 *
 * 自动探测优先：选文件后直接进入「映射 + 预览」，不先要求用户逐项配置。
 * 手动控件仅在用户点击调整后才展开；探测置信度低时自动展开并高亮。
 */
import { computed, ref } from 'vue'
import { AlertTriangle } from 'lucide-vue-next'
import { App, display, type Models } from '@/api'
import Column from 'primevue/column'
import Button from '@/volt/Button.vue'
import DataTable from '@/volt/DataTable.vue'
import Checkbox from '@/volt/Checkbox.vue'
import Dialog from '@/volt/Dialog.vue'
import InputText from '@/volt/InputText.vue'
import Message from '@/volt/Message.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import Select from '@/volt/Select.vue'
import { useSettingsStore } from '@/stores/settings'

const settings = useSettingsStore()
const emit = defineEmits<{ close: []; submitted: [batchId: number]; goSettings: [] }>()

const path = ref('')
const preview = ref<Models.app.ImportPreview | null>(null)
const sheetName = ref('')
const manualOpen = ref(false)
const loading = ref(false)
const error = ref('')

/** 提交按钮显示实际将导入的条数。 */
const importableCount = computed(() => preview.value?.preview.importableRows ?? 0)

/**
 * 关窗即关向导。
 *
 * Dialog 自己也会在 Esc 与点遮罩时关闭，这里把那一路也接到同一个出口，
 * 免得窗关了而父组件仍以为它开着。
 */
const open = computed({
  get: () => true,
  set: (v: boolean) => {
    if (!v) emit('close')
  },
})

async function pickFile() {
  error.value = ''
  try {
    const p = await App.PickImportFile()
    if (!p) return
    path.value = p
    await analyze()
  } catch (e) {
    error.value = String(e)
  }
}

async function analyze() {
  loading.value = true
  error.value = ''
  try {
    preview.value = await App.AnalyzeImportFile(path.value, sheetName.value)
    // 低置信时自动展开手动面板
    manualOpen.value = preview.value?.detection.needsAttention ?? false
  } catch (e) {
    error.value = String(e)
    preview.value = null
  } finally {
    loading.value = false
  }
}

/** 改写列映射后预览随之更新。 */
async function remap() {
  if (!preview.value) return
  loading.value = true
  try {
    preview.value = await App.RemapImportColumns(
      path.value, sheetName.value, preview.value.columnMap,
      preview.value.detection.encoding, preview.value.detection.delimiterRune,
      preview.value.detection.hasHeader,
    )
  } catch (e) {
    error.value = String(e)
  } finally {
    loading.value = false
  }
}

async function submit() {
  if (!preview.value) return
  loading.value = true
  error.value = ''
  try {
    const batchID = await App.SubmitImport(
      path.value, sheetName.value, preview.value.columnMap,
      preview.value.detection.encoding, preview.value.detection.delimiterRune,
      preview.value.detection.hasHeader,
    )
    // 提交后关窗，条目入队并自动开始匹配
    emit('submitted', batchID)
    emit('close')
  } catch (e) {
    error.value = String(e)
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <Dialog
    v-model:visible="open"
    modal
    :header="`导入${path ? ` ${path.split('/').pop()}` : ''}`"
    :style="{ width: '820px' }"
    content-class="min-h-0"
  >
    <Message v-if="error" severity="error" size="small" class="mb-3">{{ error }}</Message>
    <!-- 出网前置条件未满足时入口禁用，文案与其他出网入口一致 -->
    <div v-if="!settings.outboundAllowed" class="p-6 text-center">
      <AlertTriangle class="mx-auto mb-2 size-8 text-muted-color" />
      <p class="text-sm">{{ settings.outboundReason }}</p>
      <!-- 跳转设置，而不是把窗口关掉 -->
      <SecondaryButton variant="outlined" size="small" class="mt-3" @click="emit('goSettings')">
        前往设置
      </SecondaryButton>
    </div>

    <template v-else>
      <div v-if="!preview" class="p-8 text-center">
        <p class="mb-3 text-sm text-muted-color">支持 CSV、TSV 与 XLSX</p>
        <Button :disabled="loading" @click="pickFile">
          {{ loading ? '解析中…' : '选择文件' }}
        </Button>
      </div>

      <div v-else class="space-y-3">
        <!-- 顶部一行浅色状态；点击[调整]才展开手动控件 -->
        <div class="flex items-center gap-2 rounded bg-surface-100 px-3 py-1.5 text-xs dark:bg-surface-800">
          <span class="text-muted-color">{{ preview.statusLine }}</span>
          <SecondaryButton variant="text" size="small" @click="manualOpen = !manualOpen">
            调整
          </SecondaryButton>
        </div>

        <Message v-if="preview.detection.needsAttention" severity="warn" size="small">
          <template #icon><AlertTriangle class="size-3.5" /></template>
          {{ preview.detection.reason }}
        </Message>

        <!-- 手动控件：默认收起 -->
        <div v-if="manualOpen" class="space-y-2 rounded border p-3 text-xs">
          <div class="flex flex-wrap items-end gap-3">
            <label>编码
              <Select
                v-model="preview.detection.encoding"
                :options="preview.encodings"
                size="small"
                class="mt-1 block"
              />
            </label>
            <label>分隔符
              <InputText
                v-model="preview.detection.delimiterRune"
                size="small"
                class="mt-1 block w-16"
              />
            </label>
            <label class="flex items-center gap-1">
              <Checkbox v-model="preview.detection.hasHeader" binary /> 首行为表头
            </label>
            <label v-if="preview.detection.sheetNames?.length">工作表
              <Select
                v-model="sheetName"
                :options="preview.detection.sheetNames"
                size="small"
                placeholder="请选择…"
                class="mt-1 block"
                @update:model-value="analyze"
              />
            </label>
            <SecondaryButton variant="outlined" size="small" @click="remap">应用</SecondaryButton>
          </div>
        </div>

        <!-- 列映射 -->
        <section>
          <h3 class="mb-1.5 text-xs font-medium">列映射</h3>
          <div class="grid grid-cols-3 gap-2">
            <label
              v-for="(header, i) in preview.headers"
              :key="i"
              class="flex items-center gap-1.5 text-xs"
            >
              <span class="w-24 truncate text-muted-color" :title="header">{{ header }}</span>
              →
              <Select
                v-model="preview.columnMap[i]"
                :options="preview.fieldOptions"
                option-label="label"
                option-value="value"
                size="small"
                class="min-w-0 flex-1"
                @update:model-value="remap"
              />
            </label>
          </div>
        </section>

        <!-- 预览与跳过统计 -->
        <section>
          <div class="mb-1.5 flex items-center gap-2 text-xs">
            <h3 class="font-medium">
              预览 (前 {{ preview.preview.rows.length }} 行 / 共 {{ preview.preview.totalRows }} 行)
            </h3>
            <span v-if="preview.preview.skipSummary" class="text-yellow-600 dark:text-yellow-500">
              [!] {{ preview.preview.skipSummary }}
            </span>
          </div>
          <p v-if="preview.preview.missingSummary" class="mb-2 text-xs text-muted-color">{{ preview.preview.missingSummary }}</p>
          <DataTable :value="preview.preview.rows" scrollable scroll-height="16rem">
            <Column header="曲名">
              <template #body="{ data }">
                <span :class="data.skipReason ? 'text-muted-color line-through' : ''">
                  {{ data.title || '(缺少曲名)' }}
                </span>
              </template>
            </Column>
            <Column header="艺术家">
              <template #body="{ data }">{{ display(data.artist) }}</template>
            </Column>
            <Column header="专辑">
              <template #body="{ data }">{{ display(data.album) }}</template>
            </Column>
            <Column header="年份">
              <template #body="{ data }">{{ display(data.year) }}</template>
            </Column>
            <Column header="音轨">
              <template #body="{ data }">{{ display(data.trackNo) }}</template>
            </Column>
          </DataTable>
        </section>
      </div>
    </template>

    <template v-if="settings.outboundAllowed && preview" #footer>
      <SecondaryButton size="small" @click="emit('close')">取消</SecondaryButton>
      <Button size="small" :disabled="loading || importableCount === 0" @click="submit">
        提交导入 ({{ importableCount }} 条)
      </Button>
    </template>
  </Dialog>
</template>
