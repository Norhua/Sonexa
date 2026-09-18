<script setup lang="ts">
/**
 * 艺术家浏览：中列列出全部艺术家 + 曲目数，选中后主区显示该艺术家的曲目表格。
 *
 * 艺术家视图是按 artist_id 过滤的标准曲目表格，不是结构不同的独立页面。
 */
import { computed, onMounted, ref, watch } from 'vue'
import { App, type Models } from '@/api'
import PaneHeader from '@/components/PaneHeader.vue'
import SortMenu, { type SortOption } from '@/components/SortMenu.vue'
import ViewSearchBox from '@/components/ViewSearchBox.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
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

/** 中间栏的排序维度。 */
const sortOptions: SortOption[] = [
  { key: 'name', label: '名称' },
  { key: 'count', label: '曲目数' },
]
const sortKey = ref('name')
const sortDesc = ref(false)

const visibleArtists = computed(() => {
  const q = filter.value.trim().toLowerCase()
  const list = q
    ? artists.value.filter((a) => titleOf(a).toLowerCase().includes(q))
    : [...artists.value]

  const dir = sortDesc.value ? -1 : 1
  return list.sort((a, b) =>
    sortKey.value === 'count'
      ? dir * (a.trackCount - b.trackCount)
      : dir * titleOf(a).localeCompare(titleOf(b), 'zh-Hans-CN'),
  )
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
    <!-- 中列：固定的栏目标题，不随选中艺术家变化 -->
    <div class="flex w-64 shrink-0 flex-col border-r">
      <PaneHeader>
        <h2 class="truncate text-sm font-normal text-muted-color">艺术家</h2>
      </PaneHeader>
      <div class="min-h-0 flex-1 overflow-auto">
      <button
        v-for="a in visibleArtists"
        :key="a.id"
        :class="[
          'flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm',
          library.view === 'artist' && library.contextId === a.id
            ? 'bg-highlight font-medium'
            : 'hover:bg-emphasis',
        ]"
        @click="select(a)"
      >
        <span class="flex-1 truncate">{{ titleOf(a) }}</span>
        <span class="text-xs text-muted-color">{{ a.trackCount }}</span>
      </button>
      <p v-if="visibleArtists.length === 0 && !loading" class="px-3 py-4 text-sm text-muted-color">
        {{ filter ? '没有匹配的艺术家' : '还没有艺术家' }}
      </p>
      </div>
    </div>

    <!-- 主区：操作区在右上角，与中间栏标题同一水平线 -->
    <div class="flex min-w-0 flex-1 flex-col">
      <PaneHeader>
        <div class="flex-1" />
        <SortMenu v-model:sort-key="sortKey" v-model:desc="sortDesc" :options="sortOptions" />
        <ViewSearchBox v-model="filter" placeholder="在艺术家中查找" />
      </PaneHeader>

      <div v-if="header" class="shrink-0 px-4 pb-2">
        <div class="flex flex-wrap gap-1">
          <SecondaryButton
            v-for="f in header.facets"
            :key="f.role || 'all'"
            variant="outlined"
            rounded
            size="small"
            :class="activeRole === f.role ? 'bg-highlight! font-medium' : ''"
            @click="pickFacet(f.role)"
          >
            {{ f.label }} {{ f.count }}
          </SecondaryButton>
        </div>
      </div>

      <!-- 未选中艺术家时不显示曲目表格：这一屏还没有「该艺术家的曲目」可言，
           沿用上一个视图残留的行会让人以为它们属于某位艺术家 -->
      <slot v-if="library.view === 'artist'" />
      <div v-else class="flex flex-1 items-center justify-center text-sm text-muted-color">
        从左侧选择一位艺术家
      </div>
    </div>
  </div>
</template>
