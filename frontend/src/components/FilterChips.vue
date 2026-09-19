<script setup lang="ts">
import Chip from '@/volt/Chip.vue'
import { useLibraryStore } from '@/stores/library'
const library = useLibraryStore()
</script>

<template>
  <div v-if="library.chips.length" class="flex shrink-0 flex-wrap items-center gap-1 px-4 pb-2" aria-label="当前筛选">
    <span class="text-xs text-muted-color">筛选:</span>
    <Chip v-for="chip in library.chips" :key="chip.key" :label="chip.label" removable
      class="cursor-pointer text-xs" @remove="library.removeChip(chip.key)" />
  </div>
</template>
