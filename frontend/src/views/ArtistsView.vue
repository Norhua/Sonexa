<script setup lang="ts">
/**
 * 艺术家浏览：中列列出全部艺术家 + 曲目数，选中后主区显示该艺术家的曲目表格。
 *
 * 艺术家视图是按 artist_id 过滤的标准曲目表格，不是结构不同的独立页面。
 */
import { computed, onMounted, ref, watch } from 'vue'
import { Search } from 'lucide-vue-next'
import { App, type Models } from '@/api'
import { useLibraryStore } from '@/stores/library'

const library = useLibraryStore()
const artists = ref<Models.store.ArtistListItem[]>([])
/** 中列的就地过滤：这一列是艺术家名单，不是曲目，因此不走曲目筛选。 */
const filter = ref('')
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

// 回到未选中态时清掉上一位艺术家的头部：否则分面条还挂着他的角色计数
watch(
  () => library.view,
  (v) => {
    if (v !== 'artist') {
      header.value = null
      activeRole.value = ''
    }
  },
)

const visibleArtists = computed(() => {
  const q = filter.value.trim().toLowerCase()
  if (!q) return artists.value
  return artists.value.filter((a) => titleOf(a).toLowerCase().includes(q))
})

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
    <div class="flex w-64 shrink-0 flex-col border-r">
      <div class="relative shrink-0 border-b p-2">
        <Search class="pointer-events-none absolute left-4 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
        <input
          v-model="filter"
          type="search"
          placeholder="在艺术家中查找"
          class="w-full rounded-md border bg-background py-1 pl-7 pr-2 text-sm outline-none focus:ring-1 focus:ring-ring"
        />
      </div>
      <div class="min-h-0 flex-1 overflow-auto">
      <button
        v-for="a in visibleArtists"
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
      <p v-if="visibleArtists.length === 0 && !loading" class="px-3 py-4 text-sm text-muted-foreground">
        {{ filter ? '没有匹配的艺术家' : '还没有艺术家' }}
      </p>
      </div>
    </div>

    <!-- 主区：页头 + 分面 + 标准曲目表格（表格由父组件经默认插槽渲染） -->
    <div class="flex min-w-0 flex-1 flex-col">
      <slot name="header" />
      <div v-if="header" class="shrink-0 border-b px-4 py-1.5">
        <div class="flex flex-wrap gap-1">
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

      <!-- 未选中艺术家时不显示曲目表格：这一屏还没有「该艺术家的曲目」可言，
           沿用上一个视图残留的行会让人以为它们属于某位艺术家 -->
      <slot v-if="library.view === 'artist'" />
      <div v-else class="flex flex-1 items-center justify-center text-sm text-muted-foreground">
        从左侧选择一位艺术家
      </div>
    </div>
  </div>
</template>
