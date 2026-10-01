<script setup lang="ts">
/**
 * 编辑与重新匹配。
 *
 * 重新匹配上游只能在这里发起——其他任何位置都不对已入库曲目自动重新匹配。
 * 填充是逐字段确认的：source = user 的字段默认不勾选，用户改过的不被悄悄覆盖。
 */
import { computed, onMounted, ref } from 'vue'
import Column from 'primevue/column'
import { App, DASH, type Models } from '@/api'
import Button from '@/volt/Button.vue'
import Checkbox from '@/volt/Checkbox.vue'
import DataTable from '@/volt/DataTable.vue'
import Dialog from '@/volt/Dialog.vue'
import InputText from '@/volt/InputText.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import Select from '@/volt/Select.vue'
import Tag from '@/volt/Tag.vue'
import { useSettingsStore } from '@/stores/settings'

const props = defineProps<{ trackId: number }>()
const emit = defineEmits<{ close: []; saved: [] }>()

const settings = useSettingsStore()
const session = ref<Models.app.EditSession | null>(null)
/** 每个字段是否勾选填充。 */
const fill = ref<Record<string, boolean>>({})
/** 用户手动改写的值。 */
const manual = ref<Record<string, string>>({})
const rematchProvider = ref('')
const manualRef = ref('')
const loading = ref(false)
const error = ref('')

/**
 * 改歌集名的二次确认。
 *
 * 歌集是共享实体：改名默认影响该歌集下的全部曲目。只有一首时没有歧义，
 * 直接改；不止一首时必须让用户先看见影响范围再选处置方式。
 */
const renamePlan = ref<Models.app.AlbumRenamePlan | null>(null)
const pendingAlbumTitle = ref('')

const basicFields = computed(() => session.value?.fields.filter((f) => f.group === 'basic') ?? [])
const richFields = computed(() => session.value?.fields.filter((f) => f.group === 'rich') ?? [])

onMounted(load)

async function load() {
  loading.value = true
  error.value = ''
  try {
    session.value = await App.GetEditSession(props.trackId)
    rematchProvider.value = session.value.matchSource || settings.providers[0]?.id || ''
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
async function save(albumRenameMode = '') {
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

    // 歌集名改动会波及同专辑的其他曲目，先问清影响范围
    if (edits.album !== undefined && !albumRenameMode) {
      const plan = await App.PlanAlbumRename(props.trackId, edits.album)
      if (plan.needsConfirm) {
        renamePlan.value = plan
        pendingAlbumTitle.value = edits.album
        loading.value = false
        return
      }
    }

    await App.ApplyEdits(props.trackId, edits, sources, matchSource, matchRef, albumRenameMode)
    emit('saved')
    emit('close')
  } catch (e) {
    error.value = String(e)
  } finally {
    loading.value = false
  }
}

/** 用户选定处置方式后继续保存。 */
async function confirmRename(mode: string) {
  renamePlan.value = null
  await save(mode)
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

/** 关窗即取消。Dialog 的 Esc 与点遮罩也走这一条出口。 */
const open = computed({
  get: () => true,
  set: (v: boolean) => {
    if (!v) cancel()
  },
})

/** 两组字段共用同一套「填充 / 保持」控件。 */
const groups = computed(() => [
  { label: '基本信息', fields: basicFields.value },
  { label: '丰富信息', fields: richFields.value },
])
</script>

<template>
  <Dialog
    v-model:visible="open"
    modal
    header="编辑 / 重新匹配"
    :style="{ width: '760px' }"
  >
    <div v-if="session" class="relative space-y-4">
      <!-- 匹配溯源 -->
      <section class="rounded border p-3">
        <h3 class="mb-1.5 text-xs font-medium text-muted-color">匹配来源</h3>
        <p class="text-sm">{{ session.matchLabel }}</p>
        <div class="mt-2 flex flex-wrap items-end gap-2 text-xs">
          <label>上游
            <Select
              v-model="rematchProvider"
              :options="settings.providers"
              option-label="displayName"
              option-value="id"
              size="small"
              class="mt-1 block"
            />
          </label>
          <SecondaryButton
            variant="outlined"
            size="small"
            :disabled="loading || !settings.outboundAllowed"
            :title="!settings.outboundAllowed ? settings.outboundReason : ''"
            @click="rematch"
          >
            更换
          </SecondaryButton>
          <SecondaryButton variant="outlined" size="small" @click="clearProvenance">
            清空溯源
          </SecondaryButton>
          <label class="flex-1">手动填入标识
            <InputText
              v-model="manualRef"
              :placeholder="settings.providerById(rematchProvider)?.refs?.trackHint"
              size="small"
              class="mt-1 block w-full"
            />
          </label>
        </div>
      </section>

      <!-- 两组字段用同一套「填充 / 保持」控件 -->
      <section v-for="group in groups" :key="group.label">
        <h3 class="mb-1.5 text-xs font-medium text-muted-color">{{ group.label }}</h3>
        <DataTable :value="group.fields">
          <Column header="字段" class="w-24">
            <template #body="{ data }">
              <span class="text-xs">{{ data.label }}</span>
              <!-- 发行级字段改一次对整张歌集生效，标出来免得用户以为只动这一首 -->
              <Tag
                v-if="data.releaseScoped"
                severity="secondary"
                value="歌集"
                class="ml-1 cursor-help px-1 py-0 text-[10px] font-normal"
                title="属于歌集：改动对该歌集的全部曲目生效"
              />
            </template>
          </Column>
          <Column header="当前值">
            <template #body="{ data }">
              <div class="flex items-center gap-1">
                <InputText
                  :model-value="manual[data.key] ?? data.current ?? ''"
                  size="small"
                  class="w-full"
                  @update:model-value="manual[data.key] = $event ?? ''"
                />
              </div>
            </template>
          </Column>
          <Column header="上游值">
            <template #body="{ data }">
              <span v-if="data.upstream === undefined || data.upstream === null" class="text-muted-color">
                {{ DASH }}
              </span>
              <span v-else>{{ data.upstream }}</span>
            </template>
          </Column>
          <Column header="操作" class="w-28">
            <template #body="{ data }">
              <span v-if="data.same" class="text-xs text-muted-color">= 相同</span>
              <label
                v-else-if="data.upstream !== undefined && data.upstream !== null"
                class="flex items-center gap-1 text-xs"
              >
                <Checkbox v-model="fill[data.key]" binary /> 填充
              </label>
              <span v-else class="text-xs text-muted-color">—</span>
            </template>
          </Column>
        </DataTable>
      </section>

      <p class="text-xs text-muted-color">
        用户改过的字段默认不填充，需逐项勾选；也可手动直接改写值。
        标「歌集」的字段属于歌集，改动对该歌集的全部曲目生效。
      </p>

      <!-- 改歌集名的二次确认：歌集是共享实体，先让用户看见影响范围 -->
      <Dialog
        :visible="renamePlan !== null"
        modal
        header="选择歌集改名范围"
        :style="{ width: '460px' }"
        @update:visible="renamePlan = null"
      >
        <div v-if="renamePlan">
          <h3 class="text-sm font-semibold">这张歌集下有 {{ renamePlan.trackCount }} 首曲目</h3>
          <p class="mt-1 text-xs text-muted-color">
            把「{{ renamePlan.currentTitle || '未命名专辑' }}」改为「{{ pendingAlbumTitle }}」，
            请选择这次改动的影响范围。
          </p>
          <div class="mt-3 flex flex-col gap-2">
            <button
              class="rounded border px-3 py-2 text-left text-sm hover:bg-emphasis"
              @click="confirmRename('release')"
            >
              改这张歌集
              <span class="block text-xs text-muted-color">
                该歌集下的 {{ renamePlan.trackCount }} 首曲目一起改名
              </span>
            </button>
            <button
              v-if="renamePlan.existingReleaseId"
              class="rounded border px-3 py-2 text-left text-sm hover:bg-emphasis"
              @click="confirmRename('link')"
            >
              关联到已有的「{{ pendingAlbumTitle }}」
              <span class="block text-xs text-muted-color">
                只把这一首挪到库中已有的那张歌集下
              </span>
            </button>
            <button
              class="rounded border px-3 py-2 text-left text-sm hover:bg-emphasis"
              @click="confirmRename('move')"
            >
              只把这一首挪走
              <span class="block text-xs text-muted-color">
                新建一张歌集，原歌集及其余曲目不变
              </span>
            </button>
          </div>
          <SecondaryButton
            variant="outlined"
            size="small"
            class="mt-3 w-full"
            @click="renamePlan = null"
          >
            取消
          </SecondaryButton>
        </div>
      </Dialog>
    </div>

    <template #footer>
      <SecondaryButton variant="outlined" size="small" class="mr-auto" @click="fillAll">
        全部填充（跳过用户锁定）
      </SecondaryButton>
      <p v-if="error" class="mr-auto text-xs text-danger-color">{{ error }}</p>
      <SecondaryButton size="small" @click="cancel">取消</SecondaryButton>
      <Button size="small" :disabled="loading || !!renamePlan || !session" @click="save()">保存</Button>
    </template>
  </Dialog>
</template>
