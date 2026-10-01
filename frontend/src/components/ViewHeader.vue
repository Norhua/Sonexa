<script setup lang="ts">
/**
 * 两栏页面（全部曲目 / 最近添加 / 歌单 / 标签）的页头。
 *
 * 大标题在最左、操作区（综合菜单 + 搜索）在最右，同一水平线上垂直居中。
 * 三栏页面不用这个组件——那里标题归中间栏、操作区归主区，
 * 见 ArtistsView / AlbumsView 各自的 PaneHeader。
 */
import { ref, watch } from 'vue'
import CreateTrackDialog from '@/components/CreateTrackDialog.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
const creating = ref(false)
import PaneHeader from '@/components/PaneHeader.vue'
import ViewOptionsMenu from '@/components/ViewOptionsMenu.vue'
import ViewSearchBox from '@/components/ViewSearchBox.vue'
import Message from '@/volt/Message.vue'
import FilterChips from '@/components/FilterChips.vue'
import { useLibraryStore } from '@/stores/library'

const library = useLibraryStore()

const text = ref('')

/** 换视图时清掉上一个视图的搜索词，避免条件泄漏过来。 */
watch(
  () => [library.view, library.contextId],
  () => {
    text.value = ''
  },
)

watch(() => library.filter.query, query => { text.value = query ?? '' })

async function run() {
  await library.quickSearch(text.value.trim())
}
</script>

<template>
  <PaneHeader>
    <h2 class="min-w-0 flex-1 truncate text-xl font-bold">{{ library.contextTitle }}</h2>
    <SecondaryButton v-if="library.view === 'all'" size="small" variant="text" @click="creating = true">新建曲目</SecondaryButton>
    <ViewOptionsMenu />
    <ViewSearchBox
      v-model="text"
      :placeholder="`在“${library.contextTitle}”中搜索`"
      @submit="run"
    />
  </PaneHeader>

  <CreateTrackDialog v-model:visible="creating" />
  <FilterChips />
  <Message v-if="library.error" severity="error" size="small" class="shrink-0 rounded-none">{{ library.error }}</Message>
</template>
