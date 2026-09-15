<script setup lang="ts">
/**
 * 应用内的确认对话框。
 *
 * 替代 window.confirm —— 它在 Wails(macOS) 上恒返回 false 且不弹窗，
 * 于是「删除」类动作全都静默失败。详见 stores/ui.ts。
 */
import { computed } from 'vue'
import Dialog from '@/volt/Dialog.vue'
import Button from '@/volt/Button.vue'
import DangerButton from '@/volt/DangerButton.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import { useUiStore } from '@/stores/ui'

const ui = useUiStore()

const open = computed({
  get: () => ui.confirmOptions !== null,
  // Dialog 自己关闭（Esc、点遮罩、右上角叉）时按取消结算，
  // 否则调用方会永远等在那个 Promise 上
  set: (v: boolean) => {
    if (!v) ui.settleConfirm(false)
  },
})
</script>

<template>
  <Dialog
    v-model:visible="open"
    modal
    :header="ui.confirmOptions?.title ?? ''"
    :style="{ width: '400px' }"
  >
    <p v-if="ui.confirmOptions?.message" class="text-sm text-muted-color">
      {{ ui.confirmOptions.message }}
    </p>

    <template #footer>
      <SecondaryButton size="small" @click="ui.settleConfirm(false)">取消</SecondaryButton>
      <DangerButton
        v-if="ui.confirmOptions?.danger"
        size="small"
        @click="ui.settleConfirm(true)"
      >
        {{ ui.confirmOptions?.confirmLabel ?? '确定' }}
      </DangerButton>
      <Button v-else size="small" @click="ui.settleConfirm(true)">
        {{ ui.confirmOptions?.confirmLabel ?? '确定' }}
      </Button>
    </template>
  </Dialog>
</template>
