<script setup lang="ts">
/**
 * 空态。
 *
 * 库为空 / 搜索无结果 / 歌单为空 / 复核队列为空 各有独立文案与引导动作，
 * 不复用同一句通用提示。
 */
import { Inbox } from 'lucide-vue-next'

defineProps<{
  title: string
  description?: string
  actions?: { label: string; key: string }[]
}>()

const emit = defineEmits<{ action: [key: string] }>()
</script>

<template>
  <div class="flex flex-1 flex-col items-center justify-center gap-3 p-8 text-center">
    <Inbox class="size-10 text-muted-foreground" />
    <div>
      <p class="font-medium">{{ title }}</p>
      <p v-if="description" class="mt-1 max-w-md text-sm text-muted-foreground">
        {{ description }}
      </p>
    </div>
    <div v-if="actions?.length" class="flex gap-2">
      <button
        v-for="a in actions"
        :key="a.key"
        class="rounded-md border px-3 py-1.5 text-sm hover:bg-accent"
        @click="emit('action', a.key)"
      >
        {{ a.label }}
      </button>
    </div>
  </div>
</template>
