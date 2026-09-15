<template>
    <ContextMenu
        ref="el"
        unstyled
        :pt="theme"
        :ptOptions="{
            mergeProps: ptViewMerge
        }"
    >
        <template v-for="(_, slotName) in $slots" #[slotName]="slotProps">
            <slot :name="slotName" v-bind="slotProps ?? {}" />
        </template>
    </ContextMenu>
</template>

<script setup lang="ts">
/*
 * Volt 上游没有提供 ContextMenu，这一份按 Menu.vue 的写法补齐：
 * 同一套 surface / primary 令牌、同一组过渡，右键菜单与下拉菜单因此长得一样。
 * 升级 Volt 时这个文件不会被覆盖——它本来就是我们自己的。
 */
import ContextMenu, { type ContextMenuPassThroughOptions, type ContextMenuProps } from 'primevue/contextmenu';
import { ref } from 'vue';
import { ptViewMerge } from './utils';

interface Props extends /* @vue-ignore */ ContextMenuProps {}
defineProps<Props>();

const theme = ref<ContextMenuPassThroughOptions>({
    root: `bg-surface-0 dark:bg-surface-900
        text-surface-700 dark:text-surface-0
        border border-surface-200 dark:border-surface-700
        rounded-md min-w-48
        shadow-[0_4px_6px_-1px_rgba(0,0,0,0.1),0_2px_4px_-2px_rgba(0,0,0,0.1)]`,
    rootList: `m-0 p-1 list-none outline-none flex flex-col gap-[2px]`,
    item: `p-disabled:opacity-60 p-disabled:pointer-events-none`,
    itemContent: `group transition-colors duration-200 rounded-sm text-surface-700 dark:text-surface-0
        p-focus:bg-surface-100 dark:p-focus:bg-surface-800 p-focus:text-surface-800 dark:p-focus:text-surface-0
        hover:bg-surface-100 dark:hover:bg-surface-800 hover:text-surface-800 dark:hover:text-surface-0`,
    itemLink: `cursor-pointer flex items-center no-underline overflow-hidden relative text-inherit
        px-3 py-1.5 gap-2 select-none outline-none`,
    itemIcon: `text-surface-400 dark:text-surface-500
        p-focus:text-surface-500 dark:p-focus:text-surface-400
        group-hover:text-surface-500 dark:group-hover:text-surface-400`,
    itemLabel: ``,
    submenu: `bg-surface-0 dark:bg-surface-900 rounded-md min-w-48 p-1 flex flex-col gap-[2px]
        border border-surface-200 dark:border-surface-700
        shadow-[0_4px_6px_-1px_rgba(0,0,0,0.1),0_2px_4px_-2px_rgba(0,0,0,0.1)]`,
    submenuIcon: `ml-auto text-surface-400 dark:text-surface-500`,
    separator: `border-t border-surface-200 dark:border-surface-700 my-1`,
    transition: {
        enterFromClass: 'opacity-0 scale-y-75',
        enterActiveClass: 'transition duration-120 ease-[cubic-bezier(0,0,0.2,1)]',
        leaveActiveClass: 'transition-opacity duration-100 ease-linear',
        leaveToClass: 'opacity-0'
    }
});

const el = ref();
defineExpose({
    show: (event: Event, target?: any) => el.value.show(event, target),
    hide: () => el.value.hide()
});
</script>
