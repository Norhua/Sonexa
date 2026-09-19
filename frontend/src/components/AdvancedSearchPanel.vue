<script setup lang="ts">
/**
 * 高级搜索面板。只在搜索页提供。
 *
 * 条件从下拉列表选择后才添加，不默认铺开全部——默认全铺开时用户要先辨认
 * 哪些框是空的，再在其中挑出想用的那一两个；一屏空输入框传达的信息是零。
 *
 * 面板是 Volt 的 Popover：点击外部与 Esc 关闭由它负责——
 * 只有点「搜索」才关会让人以为界面卡住了。
 */
import { computed, ref } from 'vue'
import { ChevronDown, Plus, X } from 'lucide-vue-next'
import Button from '@/volt/Button.vue'
import InputText from '@/volt/InputText.vue'
import Menu from '@/volt/Menu.vue'
import Popover from '@/volt/Popover.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import Select from '@/volt/Select.vue'
import { useLibraryStore } from '@/stores/library'
import {
  FIELD_LABELS, UPSTREAM_FIELDS, useSearchStore, type SearchField,
} from '@/stores/search'

const library = useLibraryStore()
const search = useSearchStore()

const emit = defineEmits<{ run: [] }>()

const panel = ref<InstanceType<typeof Popover> | null>(null)
const addMenu = ref<InstanceType<typeof Menu> | null>(null)

/** 上游用不到标签条件——它筛的是本地收藏的组织方式。 */
const availableFields = computed<SearchField[]>(() => {
  const all: SearchField[] = search.isLibrary
    ? (['title', 'artist', 'album', 'tag'] as SearchField[])
    : UPSTREAM_FIELDS
  return all.filter((f) => !search.conditions.some((c) => c.field === f))
})

/** 「添加条件」下拉的模型：只列还没添加过的字段。 */
const addModel = computed(() =>
  availableFields.value.map((f) => ({
    label: FIELD_LABELS[f],
    command: () => search.addCondition(f),
  })),
)

/** 标签条件的取值域来自本地标签，用下拉而不是让用户手打 id。 */
const tagOptions = computed(() => [
  { id: '', name: '请选择标签…' },
  ...library.tags.map((t) => ({ id: String(t.id), name: `#${t.name}` })),
])

function run() {
  panel.value?.hide()
  emit('run')
}
</script>

<template>
  <div class="shrink-0">
    <SecondaryButton variant="outlined" size="small" @click="panel?.toggle($event)">
      高级
      <span v-if="search.conditions.length > 0" class="text-xs text-muted-color">
        {{ search.conditions.length }}
      </span>
      <ChevronDown class="size-3.5" />
    </SecondaryButton>

    <Popover ref="panel">
      <div class="w-[420px] space-y-3">
        <p v-if="search.conditions.length === 0" class="text-xs text-muted-color">
          还没有添加条件。从下面的「添加条件」中选一项开始。
        </p>

        <!-- 已添加的条件，每条可单独移除 -->
        <div v-for="c in search.conditions" :key="c.field" class="flex items-center gap-2">
          <span class="w-14 shrink-0 text-xs text-muted-color">{{ FIELD_LABELS[c.field] }}</span>

          <Select
            v-if="c.field === 'tag'"
            :model-value="c.value"
            :options="tagOptions"
            option-label="name"
            option-value="id"
            size="small"
            class="flex-1"
            @update:model-value="search.setCondition('tag', $event)"
          />
          <InputText
            v-else
            :model-value="c.value"
            size="small"
            class="flex-1"
            @update:model-value="search.setCondition(c.field, $event ?? '')"
            @keyup.enter="run"
          />

          <SecondaryButton
            variant="text"
            rounded
            size="small"
            title="移除该条件"
            @click="search.removeCondition(c.field)"
          >
            <X class="size-3.5" />
          </SecondaryButton>
        </div>

        <!-- 添加条件 -->
        <div>
          <SecondaryButton
            variant="outlined"
            size="small"
            :disabled="availableFields.length === 0"
            @click="addMenu?.toggle($event)"
          >
            <Plus class="size-3" />
            添加条件
          </SecondaryButton>
          <Menu ref="addMenu" :model="addModel" popup />
        </div>

        <div class="flex justify-end gap-2 border-t pt-3">
          <SecondaryButton
            variant="outlined"
            size="small"
            :disabled="search.conditions.length === 0"
            @click="search.resetConditions()"
          >
            重置
          </SecondaryButton>
          <Button size="small" @click="run">搜索</Button>
        </div>
      </div>
    </Popover>
  </div>
</template>
