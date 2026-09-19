<template>
    <TieredMenu
        ref="el"
        unstyled
        @hide="restoreFocus"
        :pt="mergedTheme"
        :ptOptions="{ mergeProps: ptViewMerge }"
    >
        <template v-for="(_, slotName) in $slots" #[slotName]="slotProps">
            <slot :name="slotName" v-bind="slotProps ?? {}" />
        </template>
    </TieredMenu>
</template>

<script setup lang="ts">
/* 本地 Volt 风格级联菜单：复用 PrimeVue 的展开、键盘与边界定位，不另写菜单交互。
 * 根容器不能 overflow-auto，否则会裁掉侧向子菜单；滚动限制放在子菜单上。
 */
import TieredMenu, { type TieredMenuPassThroughOptions, type TieredMenuProps } from 'primevue/tieredmenu';
import { computed, nextTick, onBeforeUnmount, ref } from 'vue';
import { usePassThrough } from 'primevue/passthrough';
import { ptViewMerge } from './utils';

interface Props extends /* @vue-ignore */ TieredMenuProps {
    pt?: TieredMenuPassThroughOptions;
}
const props = defineProps<Props>();

const theme: TieredMenuPassThroughOptions = {
    root: `bg-surface-0 dark:bg-surface-900 text-surface-700 dark:text-surface-0
        border border-surface-200 dark:border-surface-700 rounded-md w-52
        shadow-[0_4px_6px_-1px_rgba(0,0,0,0.1),0_2px_4px_-2px_rgba(0,0,0,0.1)]`,
    rootList: `m-0 p-1 list-none outline-none flex flex-col gap-[2px]`,
    item: `relative p-disabled:opacity-60 p-disabled:pointer-events-none`,
    itemContent: `group transition-colors duration-200 rounded-sm text-surface-700 dark:text-surface-0
        p-focus:bg-surface-100 dark:p-focus:bg-surface-800 p-focus:text-surface-800 dark:p-focus:text-surface-0
        p-active:bg-surface-100 dark:p-active:bg-surface-800
        hover:bg-surface-100 dark:hover:bg-surface-800 hover:text-surface-800 dark:hover:text-surface-0`,
    itemLink: `cursor-pointer flex items-center no-underline relative text-inherit
        px-3 py-2 gap-2 select-none outline-none`,
    itemIcon: `text-surface-400 dark:text-surface-500`,
    itemLabel: `min-w-0 break-words`,
    // 宽度跟随定位父项，向左回退的 -100% 才不会盖住父菜单文字。
    submenu: `absolute top-0 start-full z-10 m-0 w-full max-h-[70vh] overflow-y-auto
        bg-surface-0 dark:bg-surface-900 text-surface-700 dark:text-surface-0
        rounded-md p-1 list-none flex flex-col gap-[2px] outline-none
        border border-surface-200 dark:border-surface-700
        shadow-[0_4px_6px_-1px_rgba(0,0,0,0.1),0_2px_4px_-2px_rgba(0,0,0,0.1)]`,
    submenuIcon: `ml-auto shrink-0 text-surface-400 dark:text-surface-500`,
    separator: `border-t border-surface-200 dark:border-surface-700 my-1`,
    // 和桌面右键菜单一致，即时绑定外部点击与键盘监听。
    transition: { css: false }
};

const mergedTheme = computed(() => usePassThrough(theme, props.pt ?? {}, { mergeSections: true, mergeProps: true }));
const el = ref<InstanceType<typeof TieredMenu>>();
let returnFocus: HTMLElement | null = null;
function restoreFocus() {
    const target = returnFocus;
    returnFocus = null;
    void nextTick(() => {
        // PrimeVue 4.5.5 的 hide 先清空 target，Escape 无法自行返回按钮。
        // 仅菜单失焦到 body 时恢复，不抢外部输入框或新弹窗的焦点。
        if (target?.isConnected && document.activeElement === document.body) target.focus({ preventScroll: true });
    });
}
onBeforeUnmount(restoreFocus);
defineExpose({
    toggle: (event: Event) => {
        returnFocus = event.currentTarget instanceof HTMLElement ? event.currentTarget : null;
        el.value?.toggle(event);
    },
    hide: () => el.value?.hide()
});
</script>
