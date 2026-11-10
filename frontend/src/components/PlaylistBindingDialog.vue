<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { App, display, type Models } from '@/api'
import { useSettingsStore } from '@/stores/settings'
import Dialog from '@/volt/Dialog.vue'
import InputText from '@/volt/InputText.vue'
import Select from '@/volt/Select.vue'
import Button from '@/volt/Button.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import Message from '@/volt/Message.vue'
const visible = defineModel<boolean>('visible', { default: false })
const emit = defineEmits<{ bound: [id: number] }>()
const settings = useSettingsStore()
const sources = ref<Models.playlists.Source[]>([])
const source = ref(''), input = ref(''), alias = ref(''), error = ref('')
const preview = ref<Models.playlists.Preview | null>(null)
const existing = ref(0), busy = ref(false), saving = ref(false)
let generation = 0, operation = ''
const available = computed(() => sources.value.find(s => s.id === source.value))
function invalidate() {
  generation++
  if (operation) void App.CancelPlaylistOperation(operation).catch(() => {})
  operation = ''; preview.value = null; existing.value = 0; busy.value = false
}
watch(visible, async open => {
  invalidate(); error.value = ''
  if (!open) return
  source.value = ''; sources.value = []; input.value = ''; alias.value = ''
  const version = generation
  try {
    const next = await App.ListPlaylistSources()
    if (!visible.value || version !== generation) return
    sources.value = next ?? []
    source.value = sources.value.find(s => s.available)?.id ?? sources.value[0]?.id ?? ''
  } catch (e) { if (visible.value && version === generation) error.value = String(e) }
})
watch([source, input, alias], () => { invalidate(); error.value = '' }, { flush: 'sync' })
onBeforeUnmount(invalidate)
async function read() {
  if (saving.value || busy.value || !input.value.trim() || !available.value?.available || !settings.outboundAllowed) return
  invalidate(); error.value = ''; busy.value = true
  const version = generation
  const id = operation = crypto.randomUUID()
  try {
    const result = await App.PreparePlaylistBinding(id, source.value, input.value, alias.value)
    if (version !== generation || !visible.value) { void App.CancelPlaylistOperation(id); return }
    preview.value = result.preview ?? null; existing.value = result.existingPlaylistId
  } catch (e) { if (version === generation) error.value = String(e) }
  finally { if (version === generation) busy.value = false }
}
async function commit() {
  if (!preview.value) return
  saving.value = true; error.value = ''
  try {
    const result = await App.CommitPlaylistBinding(preview.value.id, preview.value.hash)
    emit('bound', result.playlistId); visible.value = false
  } catch (e) { error.value = String(e) }
  finally { saving.value = false }
}
</script>
<template>
  <Dialog v-model:visible="visible" modal header="绑定同步歌单" :closable="!saving" :close-on-escape="!saving" :style="{ width: 'min(600px, 94vw)' }">
    <div class="space-y-3 text-sm">
      <p>读取公开歌单后确认绑定。曲目完成导入后只在同步歌单显示，不会自动加入音乐库。</p>
      <label class="block">平台<Select v-model="source" :options="sources" option-label="name" option-value="id" class="mt-1 w-full" aria-label="歌单平台" :disabled="saving" /></label>
      <p v-if="!sources.length" role="status">没有可用的歌单来源</p>
      <Message v-else-if="!available?.available" severity="warn">{{ available?.reason || '来源不可用' }}</Message>
      <label class="block">歌单链接或 ID<InputText v-model="input" autofocus class="mt-1 w-full" :disabled="saving" @keyup.enter="read" /></label>
      <label class="block">本地别名（可选）<InputText v-model="alias" class="mt-1 w-full" :disabled="saving" /></label>
      <Message v-if="!settings.outboundAllowed" severity="secondary">{{ settings.outboundReason }}</Message>
      <div v-if="preview" class="rounded border p-3" data-testid="binding-preview">
        <p>{{ display(preview.name) }} → {{ preview.alias || preview.name }}</p>
        <p>将建立 {{ preview.difference.added.length }} 项导入需求；明确排除 {{ preview.difference.excluded.length }} 项。</p>
        <ul v-if="preview.difference.excluded.length" class="mt-2 max-h-40 overflow-auto text-muted-color"><li v-for="(entry, index) in preview.difference.excluded" :key="index">{{ entry.trackRef || entry.kind }} · {{ entry.reason || entry.kind }}</li></ul>
      </div>
      <Message v-if="existing" severity="info">该歌单已绑定。<SecondaryButton size="small" @click="emit('bound', existing); visible = false">打开已绑定歌单</SecondaryButton></Message>
      <Message v-if="error" severity="error">{{ error }}</Message>
    </div>
    <template #footer>
      <SecondaryButton :disabled="saving" @click="visible = false">取消</SecondaryButton>
      <SecondaryButton :disabled="saving || busy || !input.trim() || !available?.available || !settings.outboundAllowed" @click="read">{{ busy ? '读取中…' : '读取歌单' }}</SecondaryButton>
      <Button :disabled="!preview || saving" @click="commit">确认绑定</Button>
    </template>
  </Dialog>
</template>
