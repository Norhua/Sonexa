<script setup lang="ts">
/**
 * 艺术家浏览：中列列出全部艺术家 + 曲目数，选中后主区显示该艺术家的曲目表格。
 *
 * 艺术家视图是按 artist_id 过滤的标准曲目表格，不是结构不同的独立页面。
 */
import { onMounted, ref } from 'vue'
import { App, type Models } from '@/api'
import { useLibraryStore } from '@/stores/library'

const library = useLibraryStore()
const artists = ref<Models.store.ArtistListItem[]>([])
const header = ref<Models.main.ArtistHeader | null>(null)
const activeRole = ref('')
const loading = ref(false)

const emit = defineEmits<{ selected: [] }>()

onMounted(load)

async function load() {
  loading.value = true
  try {
    artists.value = (await App.ListArtists()) ?? []
  } finally {
    loading.value = false
  }
}

/** 角色艺术家的视图标题从配音关系渲染为「涼宮ハルヒ (CV: 平野綾)」。 */
function titleOf(a: Models.store.ArtistListItem): string {
  return a.voiceActor ? `${a.name} (CV: ${a.voiceActor})` : a.name
}

async function select(a: Models.store.ArtistListItem) {
  activeRole.value = ''
  header.value = await App.GetArtistHeader(a.id)
  await library.goTo('artist', { id: a.id, title: titleOf(a) })
  emit('selected')
}

/** 按 role 的分面筛选：在同一张表格内收窄。 */
async function pickFacet(role: string) {
  activeRole.value = role
  await library.applyFilter({ role }, role ? [{ key: 'role', label: `角色=${role}` }] : [])
}

defineExpose({ header, activeRole, pickFacet })
</script>

<template>
  <div class="flex min-h-0 flex-1">
    <!-- 中列：艺术家列表 -->
    <div class="w-64 shrink-0 overflow-auto border-r">
      <button
        v-for="a in artists"
        :key="a.id"
        :class="[
          'flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm hover:bg-accent',
          library.view === 'artist' && library.contextId === a.id ? 'bg-accent font-medium' : '',
        ]"
        @click="select(a)"
      >
        <span class="flex-1 truncate">{{ titleOf(a) }}</span>
        <span class="text-xs text-muted-foreground">{{ a.trackCount }}</span>
      </button>
      <p v-if="artists.length === 0 && !loading" class="px-3 py-4 text-sm text-muted-foreground">
        还没有艺术家
      </p>
    </div>

    <!-- 主区：分面 + 标准曲目表格（由父组件渲染表格） -->
    <div class="flex min-w-0 flex-1 flex-col">
      <div v-if="header" class="shrink-0 border-b px-4 py-2">
        <h2 class="text-base font-semibold">{{ header.title }}</h2>
        <div class="mt-2 flex flex-wrap gap-1">
          <button
            v-for="f in header.facets"
            :key="f.role || 'all'"
            :class="[
              'rounded-full border px-2.5 py-0.5 text-xs hover:bg-accent',
              activeRole === f.role ? 'bg-accent font-medium' : '',
            ]"
            @click="pickFacet(f.role)"
          >
            {{ f.label }} {{ f.count }}
          </button>
        </div>
      </div>
      <slot />
    </div>
  </div>
</template>
