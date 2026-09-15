<script setup lang="ts">
/**
 * 头部右侧的搜索框。
 *
 * 它只管「一个搜索词」这一件事，作用对象由使用方决定——
 * 两栏页面搜曲目，三栏页面搜中间那列的名单。
 */
import { Search, X } from 'lucide-vue-next'
import InputText from '@/volt/InputText.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'

const text = defineModel<string>({ default: '' })

withDefaults(defineProps<{ placeholder?: string; width?: string }>(), {
  placeholder: '搜索',
  width: 'w-56',
})

const emit = defineEmits<{ submit: [] }>()

function clear() {
  text.value = ''
  emit('submit')
}
</script>

<template>
  <!-- no-drag：父级头部是拖拽区，而 --wails-draggable 会继承下来，
       不显式关掉的话点击输入框会变成拖窗口 -->
  <div :class="['relative shrink-0', width]" style="--wails-draggable: no-drag">
    <Search
      class="pointer-events-none absolute left-2.5 top-1/2 z-10 size-3.5 -translate-y-1/2 text-muted-color"
    />
    <InputText
      v-model="text"
      type="search"
      size="small"
      :placeholder="placeholder"
      class="w-full pl-8 pr-7"
      @keyup.enter="emit('submit')"
      @search="emit('submit')"
    />
    <SecondaryButton
      v-if="text"
      variant="text"
      rounded
      size="small"
      class="absolute right-0.5 top-1/2 -translate-y-1/2"
      title="清除"
      @click="clear"
    >
      <X class="size-3" />
    </SecondaryButton>
  </div>
</template>
