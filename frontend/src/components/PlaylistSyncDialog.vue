<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { App, display, type Models } from '@/api'
import Dialog from '@/volt/Dialog.vue'
import Checkbox from '@/volt/Checkbox.vue'
import Button from '@/volt/Button.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import Message from '@/volt/Message.vue'
const props = defineProps<{ plan: Models.playlists.Preview | null }>()
const emit = defineEmits<{ close: []; committed: [] }>()
const saving = ref(false), error = ref(''), restore = ref<number[]>([])
const visible = computed({ get: () => !!props.plan, set: v => { if (!v && !saving.value) emit('close') } })
watch(() => props.plan?.id, () => { restore.value = []; error.value = '' })
const sections = computed(() => props.plan ? [
  { name: '新增', rows: props.plan.difference.added },
  { name: '删除', rows: props.plan.difference.removed },
  { name: '恢复已导入关联', rows: props.plan.difference.restored },
] : [])
const disposition: Record<string, string> = { import: '进入导入队列', cancel_import: '撤销本歌单导入需求', retain_absent: '保留已导入曲目并置灰', reuse: '复用原本地曲目', forget_ignored: '移除忽略记录' }
async function commit() {
  if (!props.plan) return
  saving.value = true; error.value = ''
  try { await App.CommitPlaylistSync(props.plan.id, props.plan.hash, restore.value); emit('committed') }
  catch (e) { error.value = String(e) }
  finally { saving.value = false }
}
</script>
<template>
  <Dialog v-model:visible="visible" modal header="确认同步差异" :closable="!saving" :close-on-escape="!saving" :style="{ width: 'min(760px, 94vw)' }">
    <div v-if="plan" class="max-h-[60vh] space-y-4 overflow-auto text-sm" data-testid="sync-difference">
      <p>{{ plan.alias || plan.name }}：预览不会修改列表或队列。确认后更新名称、顺序和所选差异。</p>
      <section v-for="section in sections" :key="section.name">
        <h3 class="font-medium">{{ section.name }}（{{ section.rows.length }}）</h3>
        <ul class="divide-y"><li v-for="change in section.rows" :key="change.entry.entryKey" class="py-2">
          {{ display(change.entry.title) }} · {{ change.entry.artists?.join(' / ') || '—' }} · {{ display(change.entry.album) }}
          <span class="block text-xs text-muted-color">{{ disposition[change.disposition] || change.disposition }}</span>
        </li></ul>
      </section>
      <details :key="plan.id">
        <summary class="cursor-pointer font-medium">已忽略（{{ plan.difference.ignored.length }}），展开选择恢复</summary>
        <label v-for="change in plan.difference.ignored" :key="change.entryId" class="flex items-center gap-2 py-2">
          <Checkbox v-model="restore" :value="change.entryId" :disabled="saving" :input-id="`restore-${change.entryId}`" />
          {{ display(change.entry.title) }} · {{ change.entry.artists?.join(' / ') || '—' }}
        </label>
      </details>
      <section v-if="plan.difference.excluded.length"><h3>明确排除（{{ plan.difference.excluded.length }}）</h3><p v-for="(entry, index) in plan.difference.excluded" :key="index">{{ entry.position ? `位置 ${entry.position}` : '原始位置未知' }} · {{ entry.trackRef || entry.kind }} · {{ entry.reason || entry.kind }}</p></section>
      <Message v-if="error" severity="error">{{ error }}。请取消并重新读取差异。</Message>
    </div>
    <template #footer><SecondaryButton :disabled="saving" @click="emit('close')">取消</SecondaryButton><Button :disabled="saving || !!error" @click="commit">确认同步<span v-if="restore.length">并恢复 {{ restore.length }} 项</span></Button></template>
  </Dialog>
</template>
