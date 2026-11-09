<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { App, type Models } from '@/api'
import { uniqueTracks, type Row } from '@/lib/trackRows'
import Dialog from '@/volt/Dialog.vue'
import Button from '@/volt/Button.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import Message from '@/volt/Message.vue'
const props = defineProps<{ rows: Row[] | null }>()
const emit = defineEmits<{ close: []; added: [] }>()
const plan = ref<Models.library.LibraryAdditionPlan | null>(null)
const error = ref(''), busy = ref(false), saving = ref(false)
const visible = computed({ get: () => props.rows !== null, set: v => { if (!v && !saving.value) emit('close') } })
let generation = 0
function cancel() {
  generation++
  if (plan.value) void App.CancelLibraryAddition(plan.value.id).catch(() => {})
  plan.value = null
}
watch(() => props.rows, async rows => {
  cancel(); error.value = ''
  if (!rows) return
  const version = generation
  busy.value = true
  try {
    const next = await App.PrepareLibraryAddition(uniqueTracks(rows).map(r => r.id))
    if (version !== generation) { void App.CancelLibraryAddition(next.id); return }
    plan.value = next
  } catch (e) { if (version === generation) error.value = String(e) }
  finally { if (version === generation) busy.value = false }
}, { immediate: true })
onBeforeUnmount(cancel)
const title = (id: number) => props.rows?.find(r => r.id === id)?.title || `曲目 #${id}`
async function commit() {
  if (!plan.value) return
  saving.value = true; error.value = ''
  try { await App.CommitLibraryAddition(plan.value.id, plan.value.hash); emit('added'); emit('close') }
  catch (e) { error.value = String(e) }
  finally { saving.value = false }
}
</script>
<template>
  <Dialog v-model:visible="visible" modal header="添加到我的音乐库" :closable="!saving" :close-on-escape="!saving" :style="{ width: 'min(600px, 94vw)' }">
    <div class="max-h-[60vh] space-y-3 overflow-auto text-sm">
      <p v-if="busy">正在核对入库状态…</p>
      <template v-if="plan"><p>按本地曲目去重；只改变入库状态，不重新导入或改变上游状态。</p>
        <h3 class="font-medium">将添加（{{ plan.willAdd.length }}）</h3><ul><li v-for="id in plan.willAdd" :key="id">{{ title(id) }}</li></ul>
        <h3 class="font-medium">已在音乐库（{{ plan.alreadyInLibrary.length }}）</h3><ul><li v-for="id in plan.alreadyInLibrary" :key="id">{{ title(id) }}</li></ul>
      </template>
      <Message v-if="error" severity="error">{{ error }}。请关闭后重新预览。</Message>
    </div>
    <template #footer><SecondaryButton :disabled="saving" @click="emit('close')">取消</SecondaryButton><Button :disabled="!plan || busy || saving || !!error" @click="commit">确认添加</Button></template>
  </Dialog>
</template>
