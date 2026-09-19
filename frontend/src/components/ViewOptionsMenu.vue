<script setup lang="ts">
/** 搜索旁的综合菜单；当前只提供标签筛选，排序仍由曲目表头负责。 */
import { computed, ref, useId, watch } from 'vue'
import { Check, ChevronRight, ListFilter } from 'lucide-vue-next'
import type { MenuItem } from 'primevue/menuitem'
import TieredMenu from '@/volt/TieredMenu.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import { useLibraryStore } from '@/stores/library'

const library = useLibraryStore()
const menu = ref<InstanceType<typeof TieredMenu> | null>(null)
const open = ref(false)
const id = useId()
const model = computed<MenuItem[]>(() => [{
  label: '按标签',
  items: [
    { label: '全部标签', checked: !library.filter.tagId, command: () => library.pickTag(0) },
    ...library.tags.map(tag => ({
      label: tag.name, checked: library.filter.tagId === tag.id, command: () => library.pickTag(tag.id),
    })),
    ...(!library.tags.length ? [{ label: '还没有标签', disabled: true }] : []),
  ],
}])
const menuPT = {
  item: ({ context }: { context: { item: MenuItem } }) => ({
    role: context.item.items || context.item.disabled ? 'menuitem' : 'menuitemradio',
    'aria-checked': context.item.checked,
  }),
}
watch(() => [library.view, library.contextId], () => menu.value?.hide())
</script>

<template>
  <div class="shrink-0" style="--wails-draggable: no-drag">
    <SecondaryButton variant="text" rounded size="small" title="视图选项" aria-label="视图选项"
      aria-haspopup="menu" :aria-controls="id" :aria-expanded="open"
      :class="open || library.filter.tagId ? 'bg-highlight!' : ''" @click="menu?.toggle($event)">
      <ListFilter class="size-4" />
    </SecondaryButton>
    <TieredMenu :id="id" ref="menu" :model="model" :pt="menuPT" popup aria-label="视图选项"
      @show="open = true" @hide="open = false">
      <template #itemicon="{ item }">
        <span v-if="!item.items" class="size-3.5 shrink-0">
          <Check v-if="item.checked" class="size-3.5" aria-hidden="true" />
        </span>
      </template>
      <template #submenuicon="{ class: iconClass }">
        <ChevronRight :class="[iconClass, 'size-3.5']" aria-hidden="true" />
      </template>
    </TieredMenu>
  </div>
</template>
