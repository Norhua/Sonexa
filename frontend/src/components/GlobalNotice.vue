<script setup lang="ts">
/**
 * 全局提示位置。
 *
 * 出网失败与离线状态在这里统一提示，页面内的局部区域不重复弹出相同错误。
 * 三种提示共用 Volt 的 Message，靠 severity 区分——各写各的底色必然漂移。
 */
import { computed } from 'vue'
import { AlertTriangle, WifiOff } from 'lucide-vue-next'
import Message from '@/volt/Message.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import { useSettingsStore } from '@/stores/settings'

const props = defineProps<{ message: string; offline?: boolean }>()
const emit = defineEmits<{ dismiss: []; openSettings: [] }>()

const settings = useSettingsStore()

/** 出网前置条件未满足是一种持续状态，不可被关闭。 */
const gateBlocked = computed(() => !settings.outboundAllowed && settings.settings !== null)
</script>

<template>
  <Message v-if="gateBlocked" severity="secondary" size="small" class="shrink-0 rounded-none">
    <template #icon><AlertTriangle class="size-4 shrink-0" /></template>
    <span class="flex-1">{{ settings.outboundReason }}</span>
    <SecondaryButton variant="outlined" size="small" @click="emit('openSettings')">
      前往设置
    </SecondaryButton>
  </Message>

  <Message v-else-if="props.offline" severity="secondary" size="small" class="shrink-0 rounded-none">
    <template #icon><WifiOff class="size-4 shrink-0" /></template>
    <span class="flex-1">当前无法访问上游，出网功能暂不可用。</span>
  </Message>

  <Message
    v-else-if="props.message"
    severity="error"
    size="small"
    closable
    class="shrink-0 rounded-none"
    @close="emit('dismiss')"
  >
    <template #icon><AlertTriangle class="size-4 shrink-0" /></template>
    <span class="flex-1">{{ props.message }}</span>
  </Message>
</template>
