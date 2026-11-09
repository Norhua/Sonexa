<script setup lang="ts">
import { computed } from 'vue'
import type { Models } from '@/api'
const props = defineProps<{ targets: (Pick<Models.store.ImportDestination, 'kind' | 'displayName'> & { playlistId?: number })[] }>()
const distinct = computed(() => [...new Map(props.targets.map(t => [`${t.kind}:${t.playlistId ?? t.displayName}`, t])).values()])
</script>
<template>
  <span class="ml-auto flex min-w-0 flex-wrap justify-end gap-x-3 gap-y-1 text-xs" aria-label="导入目标">
    <span v-for="target in distinct" :key="`${target.kind}:${target.playlistId ?? target.displayName}`" class="max-w-64 truncate" :class="target.kind === 'synced_playlist' ? 'text-orange-600 dark:text-orange-400' : 'text-muted-color'" :title="`to ${target.displayName}`">to {{ target.displayName }}</span>
  </span>
</template>
