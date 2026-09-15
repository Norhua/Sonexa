<script setup lang="ts">
/**
 * 应用内的输入对话框。
 *
 * 替代 window.prompt —— 它在 Wails(macOS) 上恒返回 null 且不弹窗，
 * 详见 stores/ui.ts 的说明。
 */
import { computed, nextTick, ref, watch } from 'vue'
import Dialog from '@/volt/Dialog.vue'
import Button from '@/volt/Button.vue'
import InputText from '@/volt/InputText.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import { useUiStore } from '@/stores/ui'

const ui = useUiStore()
const value = ref('')
const input = ref<{ $el: HTMLInputElement } | null>(null)

const open = computed({
  get: () => ui.promptOptions !== null,
  set: (v: boolean) => {
    if (!v) ui.settlePrompt(null)
  },
})

watch(
  () => ui.promptOptions,
  async (options) => {
    if (!options) return
    value.value = options.value ?? ''
    await nextTick()
    input.value?.$el?.focus()
    input.value?.$el?.select()
  },
  { immediate: true },
)

/** 留空视同取消：调用方拿到 null 就不会去建一个无名歌单。 */
function submit() {
  const trimmed = value.value.trim()
  ui.settlePrompt(trimmed === '' ? null : trimmed)
}
</script>

<template>
  <Dialog
    v-model:visible="open"
    modal
    :header="ui.promptOptions?.title ?? ''"
    :style="{ width: '400px' }"
  >
    <p v-if="ui.promptOptions?.description" class="mb-3 text-xs text-muted-color">
      {{ ui.promptOptions.description }}
    </p>

    <label class="block text-xs text-muted-color">
      {{ ui.promptOptions?.label ?? '名称' }}
      <InputText
        ref="input"
        v-model="value"
        size="small"
        class="mt-1 w-full"
        :placeholder="ui.promptOptions?.placeholder"
        @keyup.enter="submit"
      />
    </label>

    <template #footer>
      <SecondaryButton size="small" @click="ui.settlePrompt(null)">取消</SecondaryButton>
      <Button size="small" :disabled="value.trim() === ''" @click="submit">
        {{ ui.promptOptions?.confirmLabel ?? '确定' }}
      </Button>
    </template>
  </Dialog>
</template>
