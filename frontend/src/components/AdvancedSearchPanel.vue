<script setup lang="ts">
/**
 * 高级搜索面板。只在搜索页提供。
 *
 * 条件从下拉列表选择后才添加，不默认铺开全部——默认全铺开时用户要先辨认
 * 哪些框是空的，再在其中挑出想用的那一两个；一屏空输入框传达的信息是零。
 *
 * 面板点击外部即关闭。只有点「搜索」才关会让人以为界面卡住了。
 */
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { ChevronDown, Plus, X } from 'lucide-vue-next'
import { useLibraryStore } from '@/stores/library'
import {
  FIELD_LABELS, UPSTREAM_FIELDS, useSearchStore, type SearchField,
} from '@/stores/search'

const library = useLibraryStore()
const search = useSearchStore()

const emit = defineEmits<{ run: [] }>()

const open = ref(false)
const root = ref<HTMLElement | null>(null)
const addOpen = ref(false)

/** 上游用不到标签条件——它筛的是本地收藏的组织方式。 */
const availableFields = computed<SearchField[]>(() => {
  const all: SearchField[] = search.isLibrary
    ? (['title', 'artist', 'album', 'tag'] as SearchField[])
    : UPSTREAM_FIELDS
  return all.filter((f) => !search.conditions.some((c) => c.field === f))
})

function toggle() {
  open.value = !open.value
  if (!open.value) addOpen.value = false
}

function add(field: SearchField) {
  search.addCondition(field)
  addOpen.value = false
}

function run() {
  open.value = false
  addOpen.value = false
  emit('run')
}

function reset() {
  search.resetConditions()
  addOpen.value = false
}

/** 点击面板之外关闭。用 pointerdown 而不是 click：按下即判定，不等松开。 */
function onPointerDown(event: PointerEvent) {
  if (!open.value) return
  if (root.value && !root.value.contains(event.target as Node)) {
    open.value = false
    addOpen.value = false
  }
}

function onKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape' && open.value) {
    open.value = false
    addOpen.value = false
  }
}

onMounted(() => {
  window.addEventListener('pointerdown', onPointerDown)
  window.addEventListener('keydown', onKeydown)
})
onUnmounted(() => {
  window.removeEventListener('pointerdown', onPointerDown)
  window.removeEventListener('keydown', onKeydown)
})

defineExpose({ open })
</script>

<template>
  <div ref="root" class="relative">
    <button
      class="flex items-center gap-1 rounded-md border px-2 py-1.5 text-sm hover:bg-accent"
      @click="toggle"
    >
      高级
      <span v-if="search.conditions.length > 0" class="text-xs text-muted-foreground">
        {{ search.conditions.length }}
      </span>
      <ChevronDown :class="['size-3.5 transition-transform', open ? 'rotate-180' : '']" />
    </button>

    <div
      v-if="open"
      class="absolute right-0 top-full z-30 mt-1 w-[420px] space-y-3 rounded-md border bg-popover p-4 shadow-lg"
    >
      <p v-if="search.conditions.length === 0" class="text-xs text-muted-foreground">
        还没有添加条件。从下面的「添加条件」中选一项开始。
      </p>

      <!-- 已添加的条件，每条可单独移除 -->
      <div v-for="c in search.conditions" :key="c.field" class="flex items-center gap-2">
        <span class="w-14 shrink-0 text-xs text-muted-foreground">{{ FIELD_LABELS[c.field] }}</span>

        <select
          v-if="c.field === 'tag'"
          :value="c.value"
          class="flex-1 rounded border bg-background px-2 py-1 text-sm"
          @change="search.setCondition('tag', ($event.target as HTMLSelectElement).value)"
        >
          <option value="">请选择标签…</option>
          <option v-for="t in library.tags" :key="t.id" :value="String(t.id)">#{{ t.name }}</option>
        </select>
        <input
          v-else
          :value="c.value"
          class="flex-1 rounded border bg-background px-2 py-1 text-sm"
          @input="search.setCondition(c.field, ($event.target as HTMLInputElement).value)"
          @keyup.enter="run"
        />

        <button
          class="rounded p-1 text-muted-foreground hover:bg-accent hover:text-foreground"
          title="移除该条件"
          @click="search.removeCondition(c.field)"
        >
          <X class="size-3.5" />
        </button>
      </div>

      <!-- 添加条件 -->
      <div class="relative">
        <button
          class="flex items-center gap-1 rounded border px-2 py-1 text-xs hover:bg-accent disabled:opacity-40"
          :disabled="availableFields.length === 0"
          @click="addOpen = !addOpen"
        >
          <Plus class="size-3" />
          添加条件
        </button>
        <div
          v-if="addOpen"
          class="absolute left-0 top-full z-40 mt-1 min-w-28 rounded-md border bg-popover py-1 text-sm shadow-md"
        >
          <button
            v-for="f in availableFields"
            :key="f"
            class="block w-full px-3 py-1.5 text-left hover:bg-accent"
            @click="add(f)"
          >
            {{ FIELD_LABELS[f] }}
          </button>
        </div>
      </div>

      <div class="flex justify-end gap-2 border-t pt-3">
        <button
          class="rounded-md border px-3 py-1.5 text-sm hover:bg-accent disabled:opacity-40"
          :disabled="search.conditions.length === 0"
          @click="reset"
        >
          重置
        </button>
        <button
          class="rounded-md bg-primary px-3 py-1.5 text-sm text-primary-foreground hover:bg-primary/90"
          @click="run"
        >
          搜索
        </button>
      </div>
    </div>
  </div>
</template>
