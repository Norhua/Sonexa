<script setup lang="ts">
import { computed, ref, useId, watch } from 'vue'
import { Check, ChevronRight, CircleDashed, ListFilter } from 'lucide-vue-next'
import type { MenuItem } from 'primevue/menuitem'
import TieredMenu from '@/volt/TieredMenu.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import { useLibraryStore } from '@/stores/library'
import { useSyncedPlaylistsStore } from '@/stores/syncedPlaylists'
import { categoryLabels, supportsLibraryScope, supportsSyncedCategory, type FilterCategory } from '@/lib/viewFilters'

const library = useLibraryStore(), synced = useSyncedPlaylistsStore()
const menu = ref<InstanceType<typeof TieredMenu> | null>(null)
const open = ref(false)
const id = useId()
function category(key: FilterCategory, options: { id: number; name: string }[]): MenuItem {
  const selected = library.filter[key]
  return {
    label: categoryLabels[key], partial: selected != null,
    items: [
      { label: '全选', checked: selected == null, multiple: true, command: () => library.selectCategory(key, null) },
      ...options.map(option => ({
        label: option.name, checked: selected == null || selected.includes(option.id), multiple: true,
        command: () => library.toggleCategoryOption(key, option.id, options),
      })),
      ...(!options.length ? [{ label: `还没有${categoryLabels[key]}`, disabled: true }] : []),
    ],
  }
}
const model = computed<MenuItem[]>(() => [
  { label: '全部歌曲', checked: library.allSongsSelected, command: () => library.selectAllSongs() },
  ...(supportsLibraryScope(library.view) ? [{ label: '仅在曲库', checked: library.libraryOnly, command: () => library.selectLibraryOnly() }] : []),
  { separator: true },
  category('playlistIds', library.playlists),
  category('tagIds', library.tags),
  ...(supportsSyncedCategory(library.view) ? [category('syncedPlaylistIds', synced.playlists)] : []),
].map((item, index) => ({ ...item, labelId: `${id}_${index}_label` })))
const menuPT = {
  item: ({ context }: { context: { item: MenuItem } }) => ({
    role: context.item.items || context.item.disabled ? 'menuitem' : context.item.multiple ? 'menuitemcheckbox' : 'menuitemradio',
    'aria-checked': context.item.checked,
  }),
}
function enter(event: MouseEvent, item: MenuItem) {
  // PrimeVue 的首个子菜单默认需点击才启用 hover；通过公开 DOM 点击打开，
  // 后续的侧向定位、方向键与焦点仍交给 TieredMenu。
  const link = event.currentTarget as HTMLElement
  if (item.items && link.closest('li')?.getAttribute('aria-expanded') !== 'true') link.click()
}
function click(event: MouseEvent, item: MenuItem) {
  const link = event.currentTarget as HTMLElement
  const focusMenu = () => link.closest('[data-pc-name="tieredmenu"]')
    ?.querySelector<HTMLElement>('[data-pc-section="rootlist"]')?.focus({ preventScroll: true })
  if (item.items && link.closest('li')?.getAttribute('aria-expanded') === 'true') {
    event.stopPropagation()
    focusMenu()
    return
  }
  if (!item.multiple) return
  // 多选保持菜单和子菜单打开；Enter/Space 也通过同一 itemlink 点击路径。
  event.stopPropagation()
  item.command?.({ originalEvent: event, item })
  focusMenu()
}
watch(() => [library.view, library.contextId], () => menu.value?.hide())
</script>

<template>
  <div class="shrink-0" style="--wails-draggable: no-drag">
    <SecondaryButton variant="text" rounded size="small" title="视图选项" aria-label="视图选项"
      aria-haspopup="menu" :aria-controls="id" :aria-expanded="open"
      :class="open || library.hasCategoryFilter ? 'bg-highlight!' : ''" @click="menu?.toggle($event)">
      <ListFilter class="size-4" />
    </SecondaryButton>
    <TieredMenu :id="id" ref="menu" :model="model" :pt="menuPT" popup aria-label="视图选项"
      @show="open = true" @hide="open = false">
      <template #item="{ item, props }">
        <a v-bind="props.action" data-pc-section="itemlink" :title="item.label" @mousedown.prevent @mouseenter="enter($event, item)" @click="click($event, item)">
          <span class="size-3.5 shrink-0">
            <CircleDashed v-if="item.partial" class="size-3.5" aria-hidden="true" />
            <Check v-else-if="item.checked" class="size-3.5" aria-hidden="true" />
          </span>
          <span :id="item.labelId" class="min-w-0 break-words">{{ item.label }}</span>
          <ChevronRight v-if="item.items" class="ml-auto size-3.5 shrink-0" aria-hidden="true" />
        </a>
      </template>
    </TieredMenu>
  </div>
</template>
