<script setup lang="ts">
/**
 * 应用内的确认对话框。
 *
 * 替代 window.confirm —— 它在 Wails(macOS) 上恒返回 false 且不弹窗，
 * 于是「删除」类动作全都静默失败。详见 stores/ui.ts。
 */
import { useUiStore } from '@/stores/ui'

const ui = useUiStore()
</script>

<template>
  <div
    v-if="ui.confirmOptions"
    class="fixed inset-0 z-[70] flex items-center justify-center bg-black/40 p-8"
    @click.self="ui.settleConfirm(false)"
  >
    <div class="w-[400px] rounded-lg border bg-card p-4 shadow-xl">
      <h2 class="text-sm font-semibold">{{ ui.confirmOptions.title }}</h2>
      <p v-if="ui.confirmOptions.message" class="mt-2 text-sm text-muted-foreground">
        {{ ui.confirmOptions.message }}
      </p>

      <div class="mt-4 flex justify-end gap-2">
        <button class="rounded border px-3 py-1.5 text-sm hover:bg-accent" @click="ui.settleConfirm(false)">
          取消
        </button>
        <button
          :class="[
            'rounded-md px-3 py-1.5 text-sm',
            ui.confirmOptions.danger
              ? 'bg-destructive text-destructive-foreground hover:bg-destructive/90'
              : 'bg-primary text-primary-foreground hover:bg-primary/90',
          ]"
          @click="ui.settleConfirm(true)"
        >
          {{ ui.confirmOptions.confirmLabel ?? '确定' }}
        </button>
      </div>
    </div>
  </div>
</template>
