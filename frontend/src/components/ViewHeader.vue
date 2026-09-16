<script setup lang="ts">
/**
 * 两栏页面（全部曲目 / 最近添加 / 歌单 / 标签）的页头。
 *
 * 大标题在最左、操作区（排序 + 搜索）在最右，同一水平线上垂直居中。
 * 三栏页面不用这个组件——那里标题归中间栏、操作区归主区，
 * 见 ArtistsView / AlbumsView 各自的 PaneHeader。
 */
import { ref, watch } from 'vue'
import PaneHeader from '@/components/PaneHeader.vue'
import SortMenu, { type SortOption } from '@/components/SortMenu.vue'
import ViewSearchBox from '@/components/ViewSearchBox.vue'
import Chip from '@/volt/Chip.vue'
import { useLibraryStore } from '@/stores/library'

const library = useLibraryStore()

const sortKey = defineModel<string>('sortKey', { required: true })
const sortDesc = defineModel<boolean>('sortDesc', { required: true })

defineProps<{ sortOptions: SortOption[] }>()

const text = ref('')

/** 换视图时清掉上一个视图的搜索词，避免条件泄漏过来。 */
watch(
  () => [library.view, library.contextId],
  () => {
    text.value = ''
  },
)

async function run() {
  await library.quickSearch(text.value.trim())
}
</script>

<template>
  <PaneHeader>
    <h2 class="min-w-0 flex-1 truncate text-xl font-bold">{{ library.contextTitle }}</h2>
    <SortMenu v-model:sort-key="sortKey" v-model:desc="sortDesc" :options="sortOptions" />
    <ViewSearchBox
      v-model="text"
      :placeholder="`在“${library.contextTitle}”中搜索`"
      @submit="run"
    />
  </PaneHeader>

  <!-- 筛选 chip：标明生效的条件，可清除 -->
  <div v-if="library.chips.length > 0" class="flex flex-wrap items-center gap-1 px-4 pb-2">
    <span class="text-xs text-muted-color">筛选:</span>
    <Chip
      v-for="chip in library.chips"
      :key="chip.key"
      :label="chip.label"
      removable
      class="cursor-pointer text-xs"
      @remove="library.removeChip(chip.key)"
    />
  </div>
</template>
