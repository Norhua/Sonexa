<template>
    <DataTable
        ref="el"
        v-bind="$attrs"
        unstyled
        :pt="mergedTheme"
        :ptOptions="{
            mergeProps: ptViewMerge
        }"
    >
        <template #paginatorcontainer="{ page, pageCount, pageLinks, changePageCallback, firstPageCallback, lastPageCallback, prevPageCallback, nextPageCallback }">
            <div class="flex flex-wrap gap-2 items-center justify-center">
                <SecondaryButton text rounded @click="firstPageCallback" :disabled="page === 0">
                    <template #icon>
                        <AngleDoubleLeftIcon />
                    </template>
                </SecondaryButton>
                <SecondaryButton text rounded @click="prevPageCallback" :disabled="page === 0">
                    <template #icon>
                        <AngleLeftIcon />
                    </template>
                </SecondaryButton>
                <div class="items-center justify-center gap-2 hidden sm:flex">
                    <SecondaryButton v-for="pageLink of pageLinks" :key="pageLink" :text="page + 1 !== pageLink" rounded @click="() => changePageCallback(pageLink - 1)" :class="['shrink-0 min-w-10 h-10', { 'bg-highlight!': page + 1 === pageLink }]"
                        >{{ pageLink }}
                    </SecondaryButton>
                </div>
                <SecondaryButton text rounded @click="nextPageCallback" :disabled="page === pageCount! - 1">
                    <template #icon>
                        <AngleRightIcon />
                    </template>
                </SecondaryButton>
                <SecondaryButton text rounded @click="lastPageCallback" :disabled="page === pageCount! - 1">
                    <template #icon>
                        <AngleDoubleRightIcon />
                    </template>
                </SecondaryButton>
            </div>
        </template>
        <template #loadingicon>
            <SpinnerIcon class="animate-spin text-[2rem] w-8 h-8" />
        </template>
        <template v-for="(_, slotName) in $slots" #[slotName]="slotProps">
            <slot :name="slotName" v-bind="slotProps ?? {}" />
        </template>
    </DataTable>
</template>

<script setup lang="ts">
/*
 * 本文件由 `npx volt-vue add DataTable` 取自 Volt，之后有四处本地改动：
 *
 *   1. 行密度：headerCell / bodyCell 的 py-3 px-4 收到 py-2 / py-1.5 px-2。
 *      曲目表格是一份长列表，Volt 的默认间距一屏放不下多少行。
 *   2. 表头字重从 font-semibold 改为 font-normal，字号 text-xs、取 muted 色——
 *      表头是标注不是内容，与下方的曲目文字拉开层级。
 *   3. bodyCell 的下边框在浅色下取 surface-100：逐行都画满 surface-200 的线时，
 *      一屏几十行读起来像格子纸。
 *   4. 排序图标默认透明，只在该列已排序或鼠标悬在列头上时显形。
 *      每个可排序列都常驻一个箭头时，一排表头读起来全是箭头，
 *      反而看不出当前按哪一列排的。
 *
 * 重新从上游拉取这个文件时，以上四处需重新施加。
 */
import { ChevronsLeft as AngleDoubleLeftIcon, ChevronsRight as AngleDoubleRightIcon, ChevronLeft as AngleLeftIcon, ChevronRight as AngleRightIcon, LoaderCircle as SpinnerIcon } from 'lucide-vue-next';
import DataTable, { type DataTablePassThroughOptions, type DataTableProps } from 'primevue/datatable';
import { computed, ref } from 'vue';
import { usePassThrough } from 'primevue/passthrough';
import SecondaryButton from './SecondaryButton.vue';
import { ptViewMerge } from './utils';

interface Props extends /* @vue-ignore */ DataTableProps {
    pt?: DataTablePassThroughOptions;
}
const props = defineProps<Props>();
defineOptions({ inheritAttrs: false });

const theme = ref<DataTablePassThroughOptions>({
    root: `relative p-flex-scrollable:flex p-flex-scrollable:flex-col p-flex-scrollable:h-full`,
    tableContainer: `p-scrollable:relative p-flex-scrollable:flex p-flex-scrollable:flex-col p-flex-scrollable:flex-1 p-flex-scrollable:h-full`,
    header: `py-3 px-4 border-b border-surface-200 dark:border-surface-700
        bg-surface-0 dark:bg-surface-900
        text-surface-700 dark:text-surface-0`,
    table: `border-spacing-0 w-full border-separate`,
    thead: `p-scrollable:bg-surface-0 dark:p-scrollable:bg-surface-900 p-scrollable:top-0 p-scrollable:z-10`,
    tbody: `p-hoverable:*:hover:bg-surface-100 p-hoverable:*:hover:text-surface-800 dark:p-hoverable:*:hover:bg-surface-800 dark:p-hoverable:*:hover:text-surface-0
        p-frozen:sticky p-frozen:z-10`,
    bodyRow: `bg-surface-0 dark:bg-surface-900 text-surface-700 dark:text-surface-0 p-selectable:cursor-pointer p-selected:bg-highlight!`,
    tfoot: `p-scrollable:bg-surface-0 dark:p-scrollable:bg-surface-900 p-scrollable:bottom-0 p-scrollable:z-10`,
    footer: `py-3 px-4 border-b border-surface-200 dark:border-surface-700
        bg-surface-0 dark:bg-surface-900
        text-surface-700 dark:text-surface-0`,
    mask: `bg-black/50 text-surface-200 absolute z-10 flex items-center justify-center w-full h-full backdrop-blur-md`,
    column: {
        root: ``,
        headerCell: `group py-2 px-2 font-normal text-xs text-start transition-colors duration-200
            border-b border-surface-200 dark:border-surface-700
            bg-surface-0 dark:bg-surface-900
            text-surface-500 dark:text-surface-400
            p-sortable:cursor-pointer p-sortable:select-none p-sortable:focus-visible:outline p-sortable:focus-visible:outline-1 p-sortable:focus-visible:-outline-offset-1 p-sortable:focus-visible:outline-primary
            p-sortable:not-p-sorted:hover:bg-surface-100 p-sortable:not-p-sorted:hover:text-surface-800
            dark:p-sortable:not-p-sorted:hover:bg-surface-800 dark:p-sortable:not-p-sorted:hover:text-surface-0
            p-sorted:bg-highlight
            p-frozen:sticky p-frozen:bg-surface-0 dark:p-frozen:bg-surface-900 p-frozen:z-10
        `,
        columnHeaderContent: `flex items-center gap-2`,
        columnTitle: `font-normal`,
        bodyCell: `text-start py-1.5 px-2 border-b border-surface-100 dark:border-surface-800
            p-frozen:sticky p-frozen:bg-surface-0 dark:p-frozen:bg-surface-900`,
        bodyCellContent: ``,
        footerCell: `text-start py-3 px-4 border-b border-surface-200 dark:border-surface-800
            bg-surface-0 dark:bg-surface-900
            text-surface-700 dark:text-surface-0
            p-frozen:sticky p-frozen:bg-surface-0 dark:p-frozen:bg-surface-900`,
        columnFooter: `font-semibold`,
        columnResizer: `block absolute top-0 end-0 m-0 w-2 h-full p-0 cursor-col-resize border border-transparent`,
        sort: ``,
        sortIcon: `text-surface-500 dark:text-surface-400 transition-colors duration-200
            group-p-sortable:not-group-p-sorted:group-hover:text-surface-600 dark:group-p-sortable:not-group-p-sorted:group-hover:text-surface-300
            group-p-sorted:bg-highlight
            opacity-0 group-hover:opacity-100 group-focus-visible:opacity-100 group-p-sorted:opacity-100`,
        pcSortBadge: {
            root: `bg-primary text-primary-contrast rounded-full min-w-6 h-6 inline-flex items-center justify-center text-xs font-bold`
        },
        pcHeaderCheckbox: {
            root: `relative inline-flex select-none w-5 h-5 align-bottom`,
            input: `peer cursor-pointer disabled:cursor-default appearance-none
                absolute start-0 top-0 w-full h-full m-0 p-0 opacity-0 z-10
                border border-transparent rounded-xs`,
            box: `flex justify-center items-center rounded-sm w-5 h-5
                border border-surface-300 dark:border-surface-700
                bg-surface-0 dark:bg-surface-950
                text-surface-700 dark:text-surface-0
                peer-enabled:peer-hover:border-surface-400 dark:peer-enabled:peer-hover:border-surface-600
                p-checked:border-primary p-checked:bg-primary p-checked:text-primary-contrast
                peer-enabled:peer-hover:p-checked:bg-primary-emphasis peer-enabled:peer-hover:p-checked:border-primary-emphasis
                peer-focus-visible:outline-1 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-primary peer-focus-visible:outline
                p-disabled:bg-surface-200 dark:p-disabled:bg-surface-400 p-disabled:border-surface-300 dark:p-disabled:border-surface-700 p-disabled:text-surface-700 dark:p-disabled:text-surface-400
                shadow-[0_1px_2px_0_rgba(18,18,23,0.05)] transition-colors duration-200`,
            icon: `text-sm w-[0.875rem] h-[0.875rem] transition-none`
        },
        pcRowRadiobutton: {
            root: `relative inline-flex select-none w-5 h-5`,
            input: `peer cursor-pointer disabled:cursor-default appearance-none absolute start-0 top-0 w-full h-full m-0 p-0 opacity-0 z-10
                border border-transparent rounded-full`,
            box: `flex justify-center items-center rounded-full
                border border-surface-300 dark:border-surface-700
                bg-surface-0 dark:bg-surface-950
                peer-enabled:peer-hover:border-surface-400 dark:peer-enabled:peer-hover:border-surface-600
                p-checked:border-primary p-checked:bg-primary
                peer-enabled:peer-hover:p-checked:bg-primary-emphasis peer-enabled:peer-hover:p-checked:border-primary-emphasis
                peer-focus-visible:outline-1 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-primary peer-focus-visible:outline
                p-filled:bg-surface-50 dark:p-filled:bg-surface-800
                p-invalid:border-red-400 dark:p-invalid:border-red-300
                p-disabled:bg-surface-200 dark:p-disabled:bg-surface-400 p-disabled:border-surface-300 dark:p-disabled:border-surface-700
                shadow-[0_1px_2px_0_rgba(18,18,23,0.05)] transition-colors duration-200
                w-5 h-5`,
            icon: `bg-transparent text-xs w-3 h-3 rounded-full
                transition-all duration-200 backface-hidden scale-[0.1]
                p-checked:bg-primary-contrast p-checked:visible p-checked:scale-100
                p-disabled:bg-surface-700 dark:p-disabled:bg-surface-400`
        },
        pcRowCheckbox: {
            root: `relative inline-flex select-none w-5 h-5 align-bottom`,
            input: `peer cursor-pointer disabled:cursor-default appearance-none
                absolute start-0 top-0 w-full h-full m-0 p-0 opacity-0 z-10
                border border-transparent rounded-xs`,
            box: `flex justify-center items-center rounded-sm w-5 h-5
                border border-surface-300 dark:border-surface-700
                bg-surface-0 dark:bg-surface-950
                text-surface-700 dark:text-surface-0
                peer-enabled:peer-hover:border-surface-400 dark:peer-enabled:peer-hover:border-surface-600
                p-checked:border-primary p-checked:bg-primary p-checked:text-primary-contrast
                peer-enabled:peer-hover:p-checked:bg-primary-emphasis peer-enabled:peer-hover:p-checked:border-primary-emphasis
                peer-focus-visible:outline-1 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-primary peer-focus-visible:outline
                p-disabled:bg-surface-200 dark:p-disabled:bg-surface-400 p-disabled:border-surface-300 dark:p-disabled:border-surface-700 p-disabled:text-surface-700 dark:p-disabled:text-surface-400
                shadow-[0_1px_2px_0_rgba(18,18,23,0.05)] transition-colors duration-200`,
            icon: `text-sm w-[0.875rem] h-[0.875rem] transition-none`
        },
        rowToggleButton: `inline-flex items-center justify-center overflow-hidden relative w-7 h-7 cursor-pointer select-none
            transition-colors duration-200 rounded-full border-none bg-transparent
            text-surface-500 enabled:hover:bg-surface-100 enabled:hover:text-surface-700
            dark:text-surface-400 dark:enabled:hover:bg-surface-800 dark:enabled:hover:text-surface-0
            focus-visible:outline focus-visible:outline-1 focus-visible:outline-offset-2 focus-visible:outline-primary
            p-selected:hover:bg-surface-0 dark:p-selected:hover:bg-surface-900 p-selected:hover:text-primary`,
        rowToggleIcon: ``,
        reorderableRowHandle: ``
    },
    loadingIcon: ``,
    pcPaginator: {
        paginatorContainer: `p-bottom:border-b border-surface-200 dark:border-surface-700`,
        root: `flex items-center justify-center flex-wrap py-2 px-4 rounded-md gap-1
            bg-surface-0 dark:bg-surface-900 text-surface-700 dark:text-surface-0`
    },
    columnResizeIndicator: `w-px absolute z-10 hidden bg-primary`,
    rowReorderIndicatorUp: `absolute hidden`,
    rowReorderIndicatorDown: `absolute hidden`
});

const mergedTheme = computed(() => usePassThrough(theme.value, props.pt ?? {}, { mergeSections: true, mergeProps: ptViewMerge }));
const el = ref<InstanceType<typeof DataTable>>();
defineExpose({
    exportCSV: () => el.value?.exportCSV()
});
</script>
