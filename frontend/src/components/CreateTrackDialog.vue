<script setup lang="ts">
import { reactive, ref } from 'vue'
import { App, type Models } from '@/api'
import { useAdmissionStore } from '@/stores/admission'
import Dialog from '@/volt/Dialog.vue'
import InputText from '@/volt/InputText.vue'
import Button from '@/volt/Button.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import Message from '@/volt/Message.vue'
const visible = defineModel<boolean>('visible', { default: false })
const input = reactive({ title: '', artist: '', album: '', albumArtist: '' })
const error = ref(''), busy = ref(false)
const admission = useAdmissionStore()
async function save() {
  busy.value = true; error.value = ''
  const draft = Object.fromEntries(Object.entries(input).map(([k, v]) => [k, v || null])) as unknown as Models.app.LocalAdmissionInput
  try { const result = await admission.run(() => App.PrepareLocalAdmission(draft)); if (result) { visible.value = false; Object.assign(input, { title: '', artist: '', album: '', albumArtist: '' }) } }
  catch (e) { error.value = String(e) }
  finally { busy.value = false }
}
</script>
<template>
  <Dialog v-model:visible="visible" modal header="新建曲目" :closable="!busy" :dismissable-mask="false" :style="{ width: '440px' }">
    <p class="mb-4 text-sm text-muted-color">按已知信息保存，所有字段都可以留空。</p>
    <div class="space-y-3">
      <label v-for="field in (['title', 'artist', 'album', 'albumArtist'] as const)" :key="field" class="block text-sm">
        {{ { title: '曲名', artist: '歌曲署名', album: '专辑名', albumArtist: '专辑艺术家' }[field] }}
        <InputText v-model="input[field]" class="mt-1 w-full" :disabled="busy" />
      </label>
      <Message v-if="error" severity="error">{{ error }}</Message>
    </div>
    <template #footer><SecondaryButton :disabled="busy" @click="visible = false">取消</SecondaryButton><Button :disabled="busy" @click="save">保存</Button></template>
  </Dialog>
</template>
