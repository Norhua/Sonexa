<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { App, Events, EventsOn, display, type Models } from '@/api'
import { useAdmissionStore } from '@/stores/admission'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import Message from '@/volt/Message.vue'
const props = defineProps<{ releaseId?: number }>()
const proposals = ref<Models.app.GroupProposalView[]>([])
// 同组两个发行曲目相同的提议：只供查看与忽略，不改任何关系。
const merges = ref<Models.store.ReleaseMergeProposal[]>([])
const error = ref('')
const admission = useAdmissionStore()
let generation = 0
async function load() {
  const request = ++generation
  try {
    const [next, same] = await Promise.all([App.ListGroupProposals(), App.ListReleaseMergeProposals()])
    if (request === generation) { proposals.value = next ?? []; merges.value = same ?? [] }
  } catch (e) { if (request === generation) error.value = String(e) }
}
const off = EventsOn(Events.LibraryChanged, load)
onMounted(load)
onBeforeUnmount(() => { generation++; off() })
const visibleMerges = computed(() => merges.value.filter(m => !props.releaseId || m.a.releaseId === props.releaseId || m.b.releaseId === props.releaseId))
const side = (s: Models.store.ReleaseMergeSide) => `${s.title || '未命名发行'}（${s.identity?.provider || '本地'} · ${display(s.identity?.ref)} · 已收藏 ${s.trackRefs} 首）`
async function review(id: number) {
  error.value = ''
  try { await admission.run(() => App.PrepareGroupProposal(id, 0)); await load() }
  catch (e) { error.value = String(e) }
}
async function dismissMerge(id: number) {
  error.value = ''
  try { await App.DismissReleaseMergeProposal(id); await load() }
  catch (e) { error.value = String(e) }
}
</script>
<template>
  <div v-for="proposal in proposals.filter(p => !props.releaseId || p.releaseId === props.releaseId)" :key="proposal.id" class="flex items-center gap-2 border-b px-4 py-2 text-xs">
    <span class="min-w-0 flex-1">待处理归组提议：{{ proposal.snapshot.release?.title?.value || '未命名发行' }} · {{ proposal.resolution.groups?.map(g => g.title || '未命名组').join('、') }}</span>
    <SecondaryButton size="small" variant="text" @click="review(proposal.id)">查看归组提议</SecondaryButton>
  </div>
  <div v-for="merge in visibleMerges" :key="'m' + merge.id" class="flex items-center gap-2 border-b px-4 py-2 text-xs" data-testid="release-merge-proposal">
    <span class="min-w-0 flex-1 break-words">发行曲目相同（共 {{ merge.trackTotal }} 首）：{{ side(merge.a) }} 与 {{ side(merge.b) }}。两者保持不变，可在确认无需处理后忽略。</span>
    <SecondaryButton size="small" variant="text" @click="dismissMerge(merge.id)">忽略</SecondaryButton>
  </div>
  <Message v-if="error" severity="error" size="small">{{ error }}</Message>
</template>
