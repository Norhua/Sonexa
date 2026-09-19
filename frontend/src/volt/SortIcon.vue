<script setup lang="ts">
import { ArrowDown, ArrowUp, ArrowDownUp } from 'lucide-vue-next'
defineProps<{ sorted?: boolean; sortOrder?: number }>()
</script>
<template><component :is="sorted ? (sortOrder === -1 ? ArrowDown : ArrowUp) : ArrowDownUp" class="size-3.5 shrink-0" /></template>
