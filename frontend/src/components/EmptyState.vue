<script setup lang="ts">
/**
 * 空态。
 *
 * 库为空 / 搜索无结果 / 歌单为空 / 复核队列为空 各有独立文案与引导动作，
 * 不复用同一句通用提示。
 */
import { Inbox } from 'lucide-vue-next'
import SecondaryButton from '@/volt/SecondaryButton.vue'

defineProps<{
  title: string
  description?: string
  actions?: { label: string; key: string }[]
}>()

const emit = defineEmits<{ action: [key: string] }>()
</script>

<template>
  <div class="flex flex-1 flex-col items-center justify-center gap-3 p-8 text-center">
    <Inbox class="size-10 text-muted-color" />
    <div>
      <p class="font-medium">{{ title }}</p>
      <p v-if="description" class="mt-1 max-w-md text-sm text-muted-color">
        {{ description }}
      </p>
    </div>
    <div v-if="actions?.length" class="flex gap-2">
      <SecondaryButton
        v-for="a in actions"
        :key="a.key"
        variant="outlined"
        size="small"
        @click="emit('action', a.key)"
      >
        {{ a.label }}
      </SecondaryButton>
    </div>
  </div>
</template>
