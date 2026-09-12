<script setup lang="ts">
/**
 * 头部右侧的排序菜单（搜索框左边那个图标）。
 *
 * 排序在前端就地做：列表已经整批取回，为换一个顺序再往返一次后端
 * 既慢又会把滚动位置丢掉。
 */
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { ArrowDownUp, Check } from 'lucide-vue-next'

/** 一个可选的排序维度。 */
export interface SortOption {
  key: string
  label: string
}

const props = defineProps<{ options: SortOption[] }>()

const sortKey = defineModel<string>('sortKey', { required: true })
const desc = defineModel<boolean>('desc', { default: false })

const open = ref(false)
const root = ref<HTMLElement | null>(null)

const activeLabel = computed(
  () => props.options.find((o) => o.key === sortKey.value)?.label ?? '',
)

function pick(key: string) {
  // 再点一次当前维度就反向，与 Finder / Apple Music 的行为一致
  if (sortKey.value === key) desc.value = !desc.value
  else {
    sortKey.value = key
    desc.value = false
  }
  open.value = false
}

function onPointerDown(event: PointerEvent) {
  if (open.value && root.value && !root.value.contains(event.target as Node)) {
    open.value = false
  }
}

onMounted(() => window.addEventListener('pointerdown', onPointerDown))
onUnmounted(() => window.removeEventListener('pointerdown', onPointerDown))
</script>

<template>
  <!-- no-drag：理由同 ViewSearchBox -->
  <div ref="root" class="relative shrink-0" style="--wails-draggable: no-drag">
    <button
      class="flex size-8 items-center justify-center rounded-full border text-muted-foreground hover:bg-accent hover:text-foreground"
      :title="`排序：${activeLabel}${desc ? '（降序）' : ''}`"
      @click="open = !open"
    >
      <ArrowDownUp class="size-4" />
    </button>

    <div
      v-if="open"
      class="absolute right-0 top-full z-30 mt-1 min-w-36 rounded-md border bg-popover py-1 text-sm shadow-md"
    >
      <button
        v-for="o in options"
        :key="o.key"
        class="flex w-full items-center gap-2 px-3 py-1.5 text-left hover:bg-accent"
        @click="pick(o.key)"
      >
        <Check :class="['size-3.5', sortKey === o.key ? '' : 'invisible']" />
        <span class="flex-1">{{ o.label }}</span>
        <span v-if="sortKey === o.key" class="text-xs text-muted-foreground">
          {{ desc ? '降序' : '升序' }}
        </span>
      </button>
    </div>
  </div>
</template>
