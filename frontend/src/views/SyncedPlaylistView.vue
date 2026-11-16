<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { RefreshCw } from 'lucide-vue-next'
import { App, type Models } from '@/api'
import { useLibraryStore } from '@/stores/library'
import { useSyncedPlaylistsStore } from '@/stores/syncedPlaylists'
import { useSettingsStore } from '@/stores/settings'
import TrackTable, { type Row } from '@/components/TrackTable.vue'
import PaneHeader from '@/components/PaneHeader.vue'
import ViewOptionsMenu from '@/components/ViewOptionsMenu.vue'
import FilterChips from '@/components/FilterChips.vue'
import PlaylistSyncDialog from '@/components/PlaylistSyncDialog.vue'
import LibraryAdditionDialog from '@/components/LibraryAdditionDialog.vue'
import InputText from '@/volt/InputText.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import Message from '@/volt/Message.vue'
defineProps<{ drawerOpen?: boolean }>()
const emit = defineEmits<{ open: [row: Row]; edit: [row: Row]; copyInfo: [rows: Row[]]; addToCollection: [rows: Row[], kind: 'playlist' | 'tag'] }>()
const library = useLibraryStore(), synced = useSyncedPlaylistsStore(), settings = useSettingsStore()
const query = ref(''), busy = ref(false), error = ref('')
const plan = ref<Models.playlists.Preview | null>(null), addition = ref<Row[] | null>(null)
let generation = 0, operation = ''
function cancel() {
  generation++
  if (operation) void App.CancelPlaylistOperation(operation).catch(() => {})
  operation = ''; plan.value = null; busy.value = false
}
watch(() => library.contextId, id => { cancel(); addition.value = null; query.value = ''; error.value = ''; void synced.activate(id) }, { immediate: true })
watch(() => JSON.stringify(library.categoryFilter), () => { void synced.activate(library.contextId, query.value) })
onBeforeUnmount(() => { cancel(); void synced.activate(0) })
const rows = computed<Row[]>(() => synced.entries.map(e => ({
  id: e.trackId, rowKey: `synced:${e.entryId}`, title: e.track.title,
  artist: e.track.artist, album: e.track.album, addedAt: e.track.addedAt,
  year: e.track.year, trackNumber: e.track.trackNumber, durationMs: e.track.durationMs,
  mediumFormat: e.track.mediumFormat, country: e.track.country,
  inLibrary: e.inLibrary, upstreamMissing: !e.upstreamPresent,
  coverSource: { releaseId: e.track.releaseId, revision: e.track.coverRevision },
})))
async function refresh() {
  cancel(); busy.value = true; error.value = ''
  const version = generation, id = operation = crypto.randomUUID()
  try {
    const next = await App.PreparePlaylistSync(id, library.contextId)
    if (version !== generation) { void App.CancelPlaylistOperation(id); return }
    plan.value = next
  } catch (e) { if (version === generation) error.value = String(e) }
  finally { if (version === generation) busy.value = false }
}
function committed() { cancel(); void synced.refresh() }
</script>
<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <PaneHeader>
      <h1 class="truncate text-lg font-semibold">{{ library.contextTitle }}</h1>
      <SecondaryButton variant="text" size="small" aria-label="刷新同步歌单" :disabled="busy || !!plan || !settings.outboundAllowed" :title="settings.outboundAllowed ? '读取上游并预览差异' : settings.outboundReason" style="--wails-draggable: no-drag" @click="refresh"><RefreshCw class="size-4" :class="{ 'animate-spin': busy }" /></SecondaryButton>
      <div class="flex-1" /><ViewOptionsMenu /><InputText v-model="query" type="search" size="small" :placeholder="`在“${library.contextTitle}”中搜索…`" style="--wails-draggable: no-drag" @input="synced.activate(library.contextId, query)" />
    </PaneHeader>
    <FilterChips />
    <Message v-if="error || synced.error" severity="error">{{ error || synced.error }}</Message>
    <TrackTable :rows="rows" :drawer-open="drawerOpen" :loading="synced.loading" column-context="syncedPlaylist" :context-key="`synced:${library.contextId}`"
      @add-to-collection="(rows, kind) => emit('addToCollection', rows, kind)"
      @open="r => emit('open', r)" @edit="r => emit('edit', r)" @copy-info="r => emit('copyInfo', r)" @add-to-library="addition = $event">
      <template #empty>{{ query || library.hasCategoryFilter || library.libraryOnly ? '没有匹配的已导入曲目' : '还没有已导入曲目；待复核、失败和已忽略项不在此显示。' }}</template>
    </TrackTable>
    <PlaylistSyncDialog :plan="plan" @close="cancel" @committed="committed" />
    <LibraryAdditionDialog :rows="addition" @close="addition = null" @added="synced.refresh()" />
  </div>
</template>
