<script setup lang="ts">
/**
 * 各视图的页头：左侧标题，右上角视图内搜索框。
 *
 * 搜索框的作用域就是当前视图——占位文案里写明是哪个视图，
 * 用户不必猜这一框搜的是全库还是眼前这个歌单。
 */
import { ref, watch } from 'vue'
import { Search, X } from 'lucide-vue-next'
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

async function run() {
  await library.quickSearch(text.value.trim())
}

async function clear() {
  text.value = ''
  await library.quickSearch('')
}
</script>

<template>
  <header class="shrink-0 border-b px-4 py-2">
    <div class="flex items-center gap-3">
      <h2 class="min-w-0 flex-1 truncate text-base font-semibold">{{ library.contextTitle }}</h2>
      <slot name="actions" />

      <div v-if="library.hasViewSearch" class="relative w-64 shrink-0">
        <Search
          class="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground"
        />
        <input
          v-model="text"
          type="search"
          :placeholder="library.searchPlaceholder"
          class="w-full rounded-md border bg-background py-1 pl-8 pr-7 text-sm outline-none focus:ring-1 focus:ring-ring"
          @keyup.enter="run"
          @search="run"
        />
        <button
          v-if="text"
          class="absolute right-1.5 top-1/2 -translate-y-1/2 rounded p-0.5 text-muted-foreground hover:bg-accent"
          title="清除"
          @click="clear"
        >
          <X class="size-3" />
        </button>
      </div>
    </div>

    <!-- 筛选 chip：标明生效的条件，可清除 -->
    <div v-if="library.chips.length > 0" class="mt-2 flex flex-wrap items-center gap-1">
      <span class="text-xs text-muted-foreground">筛选:</span>
      <button
        v-for="chip in library.chips"
        :key="chip.key"
        class="flex items-center gap-1 rounded-full border bg-secondary px-2 py-0.5 text-xs hover:bg-accent"
        @click="library.removeChip(chip.key)"
      >
        {{ chip.label }}
        <X class="size-3" />
      </button>
    </div>
  </header>
</template>
