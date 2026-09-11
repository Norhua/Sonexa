<script setup lang="ts">
/**
 * 全局提示位置。
 *
 * 出网失败与离线状态在这里统一提示，页面内的局部区域不重复弹出相同错误。
 */
import { computed } from 'vue'
import { AlertTriangle, X, WifiOff } from 'lucide-vue-next'
import { useSettingsStore } from '@/stores/settings'

const props = defineProps<{ message: string; offline?: boolean }>()
const emit = defineEmits<{ dismiss: []; openSettings: [] }>()

const settings = useSettingsStore()

/** 出网前置条件未满足是一种持续状态，不可被关闭。 */
const gateBlocked = computed(() => !settings.outboundAllowed && settings.settings !== null)
</script>

<template>
  <div v-if="gateBlocked" class="flex items-center gap-2 border-b bg-secondary px-4 py-2 text-sm">
    <AlertTriangle class="size-4 shrink-0" />
    <span class="flex-1">{{ settings.outboundReason }}</span>
    <button class="rounded border px-2 py-0.5 text-xs hover:bg-accent" @click="emit('openSettings')">
      前往设置
    </button>
  </div>

  <div v-else-if="props.offline" class="flex items-center gap-2 border-b bg-secondary px-4 py-2 text-sm">
    <WifiOff class="size-4 shrink-0" />
    <span class="flex-1">当前无法访问上游，出网功能暂不可用。</span>
  </div>

  <div v-else-if="props.message" class="flex items-center gap-2 border-b bg-destructive/10 px-4 py-2 text-sm">
    <AlertTriangle class="size-4 shrink-0 text-destructive" />
    <span class="flex-1">{{ props.message }}</span>
    <button class="rounded p-0.5 hover:bg-accent" title="关闭" @click="emit('dismiss')">
      <X class="size-3.5" />
    </button>
  </div>
</template>
