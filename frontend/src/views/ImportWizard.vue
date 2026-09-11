<script setup lang="ts">
/**
 * 导入向导。
 *
 * 自动探测优先：选文件后直接进入「映射 + 预览」，不先要求用户逐项配置。
 * 手动控件仅在用户点击调整后才展开；探测置信度低时自动展开并高亮。
 */
import { computed, ref } from 'vue'
import { X, AlertTriangle } from 'lucide-vue-next'
import { App, display, type Models } from '@/api'
import { useSettingsStore } from '@/stores/settings'

const settings = useSettingsStore()
const emit = defineEmits<{ close: []; submitted: [batchId: number] }>()

const path = ref('')
const preview = ref<Models.main.ImportPreview | null>(null)
const sheetName = ref('')
const manualOpen = ref(false)
const loading = ref(false)
const error = ref('')

/** 提交按钮显示实际将导入的条数。 */
const importableCount = computed(() => preview.value?.preview.importableRows ?? 0)

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
    const batchID = await App.SubmitImport(path.value, sheetName.value, preview.value.columnMap)
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
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-8">
    <div class="flex max-h-full w-[820px] flex-col rounded-lg border bg-card shadow-xl">
      <header class="flex shrink-0 items-center justify-between border-b px-4 py-2">
        <h2 class="text-sm font-semibold">
          导入{{ path ? ` ${path.split('/').pop()}` : '' }}
        </h2>
        <button class="rounded p-1 hover:bg-accent" @click="emit('close')">
          <X class="size-4" />
        </button>
      </header>

      <!-- 出网前置条件未满足时入口禁用，文案与其他出网入口一致 -->
      <div v-if="!settings.outboundAllowed" class="p-6 text-center">
        <AlertTriangle class="mx-auto mb-2 size-8 text-muted-foreground" />
        <p class="text-sm">{{ settings.outboundReason }}</p>
        <button class="mt-3 rounded border px-3 py-1.5 text-sm hover:bg-accent" @click="emit('close')">
          前往设置
        </button>
      </div>

      <template v-else>
        <div v-if="!preview" class="p-8 text-center">
          <p class="mb-3 text-sm text-muted-foreground">支持 CSV、TSV 与 XLSX</p>
          <button
            class="rounded-md bg-primary px-4 py-2 text-sm text-primary-foreground hover:bg-primary/90"
            :disabled="loading"
            @click="pickFile"
          >
            {{ loading ? '解析中…' : '选择文件' }}
          </button>
        </div>

        <div v-else class="min-h-0 flex-1 space-y-3 overflow-auto p-4">
          <!-- 顶部一行浅色状态；点击[调整]才展开手动控件 -->
          <div class="flex items-center gap-2 rounded bg-muted/50 px-3 py-1.5 text-xs">
            <span class="text-muted-foreground">{{ preview.statusLine }}</span>
            <button class="underline" @click="manualOpen = !manualOpen">调整</button>
          </div>

          <div
            v-if="preview.detection.needsAttention"
            class="flex items-center gap-2 rounded border border-yellow-500/40 bg-yellow-500/10 px-3 py-1.5 text-xs"
          >
            <AlertTriangle class="size-3.5" />
            {{ preview.detection.reason }}
          </div>

          <!-- 手动控件：默认收起 -->
          <div v-if="manualOpen" class="space-y-2 rounded border p-3 text-xs">
            <div class="flex flex-wrap items-end gap-3">
              <label>编码
                <select v-model="preview.detection.encoding" class="mt-1 block rounded border bg-background px-2 py-1">
                  <option v-for="e in preview.encodings" :key="e" :value="e">{{ e }}</option>
                </select>
              </label>
              <label>分隔符
                <input v-model="preview.detection.delimiterRune"
                  class="mt-1 block w-16 rounded border bg-background px-2 py-1" />
              </label>
              <label class="flex items-center gap-1">
                <input v-model="preview.detection.hasHeader" type="checkbox" /> 首行为表头
              </label>
              <label v-if="preview.detection.sheetNames?.length">工作表
                <select v-model="sheetName" class="mt-1 block rounded border bg-background px-2 py-1"
                  @change="analyze">
                  <option value="">请选择…</option>
                  <option v-for="s in preview.detection.sheetNames" :key="s" :value="s">{{ s }}</option>
                </select>
              </label>
              <button class="rounded border px-2 py-1 hover:bg-accent" @click="remap">应用</button>
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
                <span class="w-24 truncate text-muted-foreground" :title="header">{{ header }}</span>
                →
                <select
                  v-model="preview.columnMap[i]"
                  class="flex-1 rounded border bg-background px-1.5 py-0.5"
                  @change="remap"
                >
                  <option v-for="f in preview.fieldOptions" :key="f.value" :value="f.value">
                    {{ f.label }}
                  </option>
                </select>
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
            <div class="max-h-64 overflow-auto rounded border">
              <table class="w-full border-collapse text-xs">
                <thead class="sticky top-0 bg-muted">
                  <tr class="text-left">
                    <th class="px-2 py-1">曲名</th>
                    <th class="px-2 py-1">艺术家</th>
                    <th class="px-2 py-1">专辑</th>
                    <th class="px-2 py-1">年份</th>
                    <th class="px-2 py-1">音轨</th>
                  </tr>
                </thead>
                <tbody>
                  <tr
                    v-for="row in preview.preview.rows"
                    :key="row.index"
                    :class="['border-t', row.skipReason ? 'text-muted-foreground line-through' : '']"
                  >
                    <td class="px-2 py-1">{{ row.title || '(缺少曲名)' }}</td>
                    <td class="px-2 py-1">{{ display(row.artist) }}</td>
                    <td class="px-2 py-1">{{ display(row.album) }}</td>
                    <td class="px-2 py-1">{{ display(row.year) }}</td>
                    <td class="px-2 py-1">{{ display(row.trackNo) }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>
        </div>

        <footer v-if="preview" class="flex shrink-0 items-center justify-end gap-2 border-t px-4 py-2">
          <p v-if="error" class="mr-auto text-xs text-destructive">{{ error }}</p>
          <button class="rounded border px-3 py-1.5 text-sm hover:bg-accent" @click="emit('close')">
            取消
          </button>
          <button
            class="rounded-md bg-primary px-3 py-1.5 text-sm text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
            :disabled="loading || importableCount === 0"
            @click="submit"
          >
            提交导入 ({{ importableCount }} 条)
          </button>
        </footer>
      </template>
    </div>
  </div>
</template>
