<script setup lang="ts">
/**
 * 应用内的输入对话框。
 *
 * 替代 window.prompt —— 它在 Wails(macOS) 上恒返回 null 且不弹窗，
 * 详见 stores/ui.ts 的说明。
 */
import { nextTick, ref, watch } from 'vue'
import { useUiStore } from '@/stores/ui'

const ui = useUiStore()
const value = ref('')
const input = ref<HTMLInputElement | null>(null)

watch(
  () => ui.promptOptions,
  async (options) => {
    if (!options) return
    value.value = options.value ?? ''
    await nextTick()
    input.value?.focus()
    input.value?.select()
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
  <div
    v-if="ui.promptOptions"
    class="fixed inset-0 z-[70] flex items-center justify-center bg-black/40 p-8"
    @click.self="ui.settlePrompt(null)"
  >
    <div class="w-[400px] rounded-lg border bg-card p-4 shadow-xl" @keydown.esc="ui.settlePrompt(null)">
      <h2 class="text-sm font-semibold">{{ ui.promptOptions.title }}</h2>
      <p v-if="ui.promptOptions.description" class="mt-1 text-xs text-muted-foreground">
        {{ ui.promptOptions.description }}
      </p>

      <label class="mt-3 block text-xs text-muted-foreground">
        {{ ui.promptOptions.label ?? '名称' }}
        <input
          ref="input"
          v-model="value"
          class="mt-1 w-full rounded border bg-background px-2 py-1.5 text-sm text-foreground outline-none focus:ring-1 focus:ring-ring"
          :placeholder="ui.promptOptions.placeholder"
          @keyup.enter="submit"
        />
      </label>

      <div class="mt-4 flex justify-end gap-2">
        <button class="rounded border px-3 py-1.5 text-sm hover:bg-accent" @click="ui.settlePrompt(null)">
          取消
        </button>
        <button
          class="rounded-md bg-primary px-3 py-1.5 text-sm text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
          :disabled="value.trim() === ''"
          @click="submit"
        >
          {{ ui.promptOptions.confirmLabel ?? '确定' }}
        </button>
      </div>
    </div>
  </div>
</template>
