<script setup lang="ts">
/** Volt Dialog；PrimeVue 4 的全局 Escape 会同时关闭嵌套弹窗，改为仅由焦点所在弹窗处理。 */
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { X, Maximize2, Minimize2 } from 'lucide-vue-next'
import Dialog, { type DialogProps, type DialogPassThroughOptions } from 'primevue/dialog'
import { ptViewMerge } from './utils'
interface Props extends /* @vue-ignore */ DialogProps {
  visible?: boolean
  closeOnEscape?: boolean
}
const props = withDefaults(defineProps<Props>(), { visible: false, closeOnEscape: true })
const emit = defineEmits<{ 'update:visible': [value: boolean] }>()
let returnFocus: HTMLElement | null = null
watch(() => props.visible, visible => {
  if (visible) returnFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
}, { immediate: true })
function restoreFocus() {
  const target = returnFocus
  returnFocus = null
  void nextTick(() => {
    if (target?.isConnected) target.focus({ preventScroll: true })
  })
}
onBeforeUnmount(restoreFocus)
function keydown(event: KeyboardEvent) {
  if (event.key !== 'Escape' || event.defaultPrevented || event.isComposing || !props.closeOnEscape) return
  event.preventDefault()
  event.stopPropagation()
  emit('update:visible', false)
}
const theme = ref<DialogPassThroughOptions>({
  root: {
    class: `flex flex-col max-h-[90dvh] max-w-[calc(100vw-2rem)] rounded-xl border border-surface-200 dark:border-surface-700
      bg-surface-0 dark:bg-surface-900 text-color shadow-lg
      p-maximized:w-screen p-maximized:h-screen p-maximized:max-h-full p-maximized:rounded-none`,
    onKeydown: keydown,
    style: { '--wails-draggable': 'no-drag' },
  },
  header: 'flex items-center justify-between shrink-0 gap-4 px-5 py-4',
  title: 'font-semibold text-base',
  headerActions: 'flex items-center gap-2',
  content: 'min-h-0 overflow-y-auto px-5 pb-5 p-maximized:grow',
  footer: 'shrink-0 px-5 pb-5 flex justify-end gap-2',
  pcCloseButton: { root: 'inline-flex size-7 items-center justify-center rounded-md cursor-pointer hover:bg-emphasis focus-visible:outline focus-visible:outline-primary' },
  pcMaximizeButton: { root: 'inline-flex size-7 items-center justify-center rounded-md cursor-pointer hover:bg-emphasis' },
  mask: 'p-modal:bg-black/40 p-modal:fixed p-modal:inset-0',
  transition: {
    enterFromClass: 'opacity-0 scale-95', enterActiveClass: 'transition duration-150 motion-reduce:transition-none',
    leaveActiveClass: 'transition duration-100 motion-reduce:transition-none', leaveToClass: 'opacity-0 scale-95',
  },
})
</script>
<template>
  <Dialog unstyled :visible="visible" :close-on-escape="false" :draggable="false" block-scroll
    :pt="theme" :pt-options="{ mergeProps: ptViewMerge }" @update:visible="emit('update:visible', $event)" @after-hide="restoreFocus">
    <template #closeicon><X class="size-4" /></template>
    <template #maximizeicon="{ maximized }"><component :is="maximized ? Minimize2 : Maximize2" class="size-4" /></template>
    <template v-for="(_, slotName) in $slots" #[slotName]="slotProps"><slot :name="slotName" v-bind="slotProps ?? {}" /></template>
  </Dialog>
</template>
