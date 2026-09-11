<script setup lang="ts">
/**
 * 编辑与重新匹配。
 *
 * 重新匹配上游只能在这里发起——其他任何位置都不对已入库曲目自动重新匹配。
 * 填充是逐字段确认的：source = user 的字段默认不勾选，用户改过的不被悄悄覆盖。
 */
import { computed, onMounted, ref } from 'vue'
import { X } from 'lucide-vue-next'
import { App, DASH, type Models } from '@/api'
import { useSettingsStore } from '@/stores/settings'

const props = defineProps<{ trackId: number }>()
const emit = defineEmits<{ close: []; saved: [] }>()

const settings = useSettingsStore()
const session = ref<Models.main.EditSession | null>(null)
/** 每个字段是否勾选填充。 */
const fill = ref<Record<string, boolean>>({})
/** 用户手动改写的值。 */
const manual = ref<Record<string, string>>({})
const rematchProvider = ref('musicbrainz')
const manualRef = ref('')
const loading = ref(false)
const error = ref('')

const basicFields = computed(() => session.value?.fields.filter((f) => f.group === 'basic') ?? [])
const richFields = computed(() => session.value?.fields.filter((f) => f.group === 'rich') ?? [])

onMounted(load)

async function load() {
  loading.value = true
  error.value = ''
  try {
    session.value = await App.GetEditSession(props.trackId)
    resetFill()
  } catch (e) {
    error.value = String(e)
  } finally {
    loading.value = false
  }
}

function resetFill() {
  const next: Record<string, boolean> = {}
  for (const f of session.value?.fields ?? []) {
    // source = user 的字段默认不勾选；其余默认勾选
    next[f.key] = f.fillByDefault
  }
  fill.value = next
  manual.value = {}
  manualRef.value = session.value?.matchRef ?? ''
}

/** 从编辑页发起重新匹配。 */
async function rematch() {
  if (!session.value) return
  loading.value = true
  error.value = ''
  try {
    const title = basicFields.value.find((f) => f.key === 'title')?.current ?? ''
    const artist = basicFields.value.find((f) => f.key === 'artist')?.current ?? ''
    const album = basicFields.value.find((f) => f.key === 'album')?.current ?? ''
    session.value = await App.RematchTrack(props.trackId, rematchProvider.value, title, artist, album, '')
    resetFill()
  } catch (e) {
    error.value = String(e)
  } finally {
    loading.value = false
  }
}

/** 全部填充，默认跳过用户锁定项。 */
function fillAll() {
  for (const f of session.value?.fields ?? []) {
    if (f.userLocked) continue // 带用户徽标的字段保持不变
    if (f.upstream !== undefined && f.upstream !== null && !f.same) fill.value[f.key] = true
  }
}

/** 保存：填充勾选在确认后才写入。 */
async function save() {
  if (!session.value) return
  loading.value = true
  error.value = ''
  try {
    const edits: Record<string, string> = {}
    const sources: Record<string, string> = {}

    for (const f of session.value.fields) {
      // 用户手动改写优先：来源置为 user 并加锁
      if (manual.value[f.key] !== undefined && manual.value[f.key] !== (f.current ?? '')) {
        edits[f.key] = manual.value[f.key]
        sources[f.key] = 'user'
        continue
      }
      // 勾选填充：取上游值，来源改为对应上游并清除用户锁
      if (fill.value[f.key] && f.upstream !== undefined && f.upstream !== null) {
        edits[f.key] = f.upstream
        sources[f.key] = session.value.matchSource ?? rematchProvider.value
      }
    }

    let matchSource: string | undefined
    let matchRef: string | undefined
    if (manualRef.value && manualRef.value !== session.value.matchRef) {
      matchSource = session.value.matchSource ?? rematchProvider.value
      matchRef = manualRef.value
    }

    await App.ApplyEdits(props.trackId, edits, sources, matchSource, matchRef)
    emit('saved')
    emit('close')
  } catch (e) {
    error.value = String(e)
  } finally {
    loading.value = false
  }
}

async function clearProvenance() {
  error.value = ''
  try {
    await App.ClearProvenance(props.trackId)
    await load()
  } catch (e) {
    error.value = String(e)
  }
}

/** 取消：丢弃全部未保存的改动。 */
function cancel() {
  emit('close')
}
</script>

<template>
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-8">
    <div class="flex max-h-full w-[760px] flex-col rounded-lg border bg-card shadow-xl">
      <header class="flex shrink-0 items-center justify-between border-b px-4 py-2">
        <h2 class="text-sm font-semibold">编辑 / 重新匹配</h2>
        <button class="rounded p-1 hover:bg-accent" @click="cancel">
          <X class="size-4" />
        </button>
      </header>

      <div v-if="session" class="min-h-0 flex-1 space-y-4 overflow-auto p-4">
        <!-- 匹配溯源 -->
        <section class="rounded border p-3">
          <h3 class="mb-1.5 text-xs font-medium text-muted-foreground">匹配来源</h3>
          <p class="text-sm">{{ session.matchLabel }}</p>
          <div class="mt-2 flex flex-wrap items-end gap-2 text-xs">
            <label>上游
              <select v-model="rematchProvider" class="mt-1 block rounded border bg-background px-2 py-1">
                <option v-for="p in settings.providers" :key="p.id" :value="p.id">
                  {{ p.displayName }}
                </option>
              </select>
            </label>
            <button
              class="rounded border px-2 py-1 hover:bg-accent disabled:opacity-50"
              :disabled="loading || !settings.outboundAllowed"
              :title="!settings.outboundAllowed ? settings.outboundReason : ''"
              @click="rematch"
            >
              更换
            </button>
            <button class="rounded border px-2 py-1 hover:bg-accent" @click="clearProvenance">
              清空溯源
            </button>
            <label class="flex-1">手动填入标识
              <input v-model="manualRef" placeholder="MB 为 track MBID；iTunes 为 地区:编号"
                class="mt-1 block w-full rounded border bg-background px-2 py-1" />
            </label>
          </div>
        </section>

        <!-- 两组字段用同一套「填充 / 保持」控件 -->
        <section v-for="group in [
          { label: '基本信息', fields: basicFields },
          { label: '丰富信息', fields: richFields },
        ]" :key="group.label">
          <h3 class="mb-1.5 text-xs font-medium text-muted-foreground">{{ group.label }}</h3>
          <table class="w-full border-collapse text-sm">
            <thead>
              <tr class="border-b text-left text-xs text-muted-foreground">
                <th class="w-20 px-2 py-1">字段</th>
                <th class="px-2 py-1">当前值</th>
                <th class="px-2 py-1">上游值</th>
                <th class="w-28 px-2 py-1">操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="f in group.fields" :key="f.key" class="border-b">
                <td class="px-2 py-1.5 text-xs">{{ f.label }}</td>
                <td class="px-2 py-1.5">
                  <input
                    :value="manual[f.key] ?? f.current ?? ''"
                    class="w-full rounded border bg-background px-1.5 py-0.5 text-sm"
                    @input="manual[f.key] = ($event.target as HTMLInputElement).value"
                  />
                  <!-- 用户改过的字段显示「用户」徽标 -->
                  <span v-if="f.userLocked" class="ml-1 rounded bg-secondary px-1 text-[10px]">用户</span>
                </td>
                <td class="px-2 py-1.5">
                  <span v-if="f.upstream === undefined || f.upstream === null" class="text-muted-foreground">
                    {{ DASH }}
                  </span>
                  <span v-else>{{ f.upstream }}</span>
                </td>
                <td class="px-2 py-1.5 text-xs">
                  <span v-if="f.same" class="text-muted-foreground">= 相同</span>
                  <label v-else-if="f.upstream !== undefined && f.upstream !== null"
                    class="flex items-center gap-1">
                    <input v-model="fill[f.key]" type="checkbox" /> 填充
                  </label>
                  <span v-else class="text-muted-foreground">—</span>
                </td>
              </tr>
            </tbody>
          </table>
        </section>

        <p class="text-xs text-muted-foreground">
          带「用户」徽标的字段默认不填充，需逐项勾选；也可手动直接改写值。
        </p>
      </div>

      <footer class="flex shrink-0 items-center gap-2 border-t px-4 py-2">
        <button class="rounded border px-3 py-1.5 text-sm hover:bg-accent" @click="fillAll">
          全部填充（跳过用户锁定）
        </button>
        <p v-if="error" class="flex-1 text-xs text-destructive">{{ error }}</p>
        <div v-else class="flex-1" />
        <button class="rounded border px-3 py-1.5 text-sm hover:bg-accent" @click="cancel">取消</button>
        <button
          class="rounded-md bg-primary px-3 py-1.5 text-sm text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
          :disabled="loading"
          @click="save"
        >
          保存
        </button>
      </footer>
    </div>
  </div>
</template>
