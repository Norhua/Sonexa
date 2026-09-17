<script setup lang="ts">
/**
 * 头部右侧的排序菜单（搜索框左边那个图标）。
 *
 * 排序在前端就地做：列表已经整批取回，为换一个顺序再往返一次后端
 * 既慢又会把滚动位置丢掉。曲目表格把选中的维度交给 DataTable 排序，
 * 名单栏（艺术家/专辑）自己排——两处共用这一个菜单。
 */
import { computed, ref } from 'vue'
import { ArrowDownUp, Check } from 'lucide-vue-next'
import Menu from '@/volt/Menu.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'

/** 一个可选的排序维度。 */
export interface SortOption {
  key: string
  label: string
}

const props = defineProps<{ options: SortOption[] }>()

const sortKey = defineModel<string>('sortKey', { required: true })
const desc = defineModel<boolean>('desc', { default: false })

const menu = ref<InstanceType<typeof Menu> | null>(null)

const activeLabel = computed(
  () => props.options.find((o) => o.key === sortKey.value)?.label ?? '',
)

/** Menu 的模型：每项带上自己的 key，勾选与升降序在 #item 里渲染。 */
const model = computed(() =>
  props.options.map((o) => ({ label: o.label, key: o.key, command: () => pick(o.key) })),
)

function pick(key: string) {
  // 再点一次当前维度就反向，与 Finder / Apple Music 的行为一致
  if (key === 'default') {
    sortKey.value = key
    desc.value = false
  } else if (sortKey.value === key) desc.value = !desc.value
  else {
    sortKey.value = key
    desc.value = false
  }
}
</script>

<template>
  <!-- no-drag：父级头部是拖拽区，--wails-draggable 会继承下来，
       不显式关掉的话点一下按钮就开始拖窗口 -->
  <div class="shrink-0" style="--wails-draggable: no-drag">
    <SecondaryButton
      variant="text"
      rounded
      size="small"
      :title="`排序：${activeLabel}${desc ? '（降序）' : ''}`"
      @click="menu?.toggle($event)"
    >
      <ArrowDownUp class="size-4" />
    </SecondaryButton>

    <Menu ref="menu" :model="model" popup>
      <template #item="{ item, props: itemProps }">
        <a v-bind="itemProps.action" class="flex items-center gap-2">
          <Check :class="['size-3.5', sortKey === item.key ? '' : 'invisible']" />
          <span class="flex-1">{{ item.label }}</span>
          <span v-if="sortKey === item.key && item.key !== 'default'" class="text-xs text-muted-color">
            {{ desc ? '降序' : '升序' }}
          </span>
        </a>
      </template>
    </Menu>
  </div>
</template>
