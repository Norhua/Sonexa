<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { App, Events, EventsOn, type Models } from '@/api'
import { useAdmissionStore } from '@/stores/admission'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import Message from '@/volt/Message.vue'
const props = defineProps<{ releaseId?: number }>()
const proposals = ref<Models.app.GroupProposalView[]>([])
const error = ref('')
const admission = useAdmissionStore()
let generation = 0
async function load() {
  const request = ++generation
  try { const next = await App.ListGroupProposals(); if (request === generation) proposals.value = next ?? [] }
  catch (e) { if (request === generation) error.value = String(e) }
}
const off = EventsOn(Events.LibraryChanged, load)
onMounted(load)
onBeforeUnmount(() => { generation++; off() })
async function review(id: number) {
  error.value = ''
  try { await admission.run(() => App.PrepareGroupProposal(id, 0)); await load() }
  catch (e) { error.value = String(e) }
}
</script>
<template>
  <div v-for="proposal in proposals.filter(p => !props.releaseId || p.releaseId === props.releaseId)" :key="proposal.id" class="flex items-center gap-2 border-b px-4 py-2 text-xs">
    <span class="min-w-0 flex-1">待处理归组提议：{{ proposal.snapshot.release?.title?.value || '未命名发行' }} · {{ proposal.resolution.groups?.map(g => g.title || '未命名组').join('、') }}</span>
    <SecondaryButton size="small" variant="text" @click="review(proposal.id)">查看归组提议</SecondaryButton>
  </div>
  <Message v-if="error" severity="error" size="small">{{ error }}</Message>
</template>
