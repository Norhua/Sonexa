import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { App, Events, EventsOn, type Models } from '@/api'

export const useCoverSearchStore = defineStore('coverSearch', () => {
  const target = ref<Models.app.TrackDetail | null>(null)
  const searchId = ref('')
  const title = ref(''), artist = ref(''), album = ref(''), country = ref('')
  const sources = ref(['caa', 'itunes', 'qq', '163'])
  const results = ref<Record<string, Omit<Models.app.CoverSearchEvent, 'convertValues'>>>({})
  const error = ref('')
  const revision = ref(0)
  let off: (() => void) | null = null
  const cards = computed(() => Object.values(results.value).flatMap(result => result.cards ?? []))
  async function open(detail: Models.app.TrackDetail) {
    await close()
    target.value = detail
    title.value = detail.track.title
    artist.value = detail.track.artist ?? ''
    album.value = detail.track.album ?? ''
    revision.value = await App.CoverRevision(detail.track.releaseId ?? 0)
    off = EventsOn(Events.CoverResults, (event: Models.app.CoverSearchEvent) => {
      if (event.searchId !== searchId.value || event.releaseId !== target.value?.track.releaseId) return
      results.value = { ...results.value, [event.source]: event }
    })
  }
  async function search(only?: string) {
    if (!target.value?.track.releaseId) return
    if (searchId.value) await App.CancelCoverSearch(searchId.value)
    searchId.value = crypto.randomUUID()
    const chosen = only ? [only] : sources.value
    if (!only) results.value = {}
    chosen.forEach(source => { results.value[source] = { searchId: searchId.value, releaseId: target.value!.track.releaseId!, source, state: 'loading', cards: [], error: '' } })
    error.value = ''
    try {
      await App.StartCoverSearch({ searchId: searchId.value, releaseId: target.value.track.releaseId, title: title.value, artist: artist.value, album: album.value, country: country.value, sources: chosen })
    } catch (e) { error.value = String(e); chosen.forEach(source => { results.value[source]!.state = 'error' }) }
  }
  async function adopt(variantId: number) {
    const result = await App.AdoptCoverCandidate(searchId.value, variantId, revision.value)
    revision.value = result.revision
    return result
  }
  async function close() {
    const old = searchId.value
    searchId.value = ''
    off?.(); off = null
    target.value = null
    results.value = {}
    error.value = ''
    if (old) await App.CancelCoverSearch(old)
  }
  return { target, title, artist, album, country, sources, results, cards, error, open, search, adopt, close }
})
