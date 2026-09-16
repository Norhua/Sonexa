<script setup lang="ts">
/**
 * 设置弹窗。
 *
 * 设置不是浏览目的地：把它做成主内容区的一个视图，就意味着每次改一项设置
 * 都要把用户正在看的歌单顶掉，回来还得自己找回去。弹窗关掉即回到原处。
 */
import SettingsView from '@/views/SettingsView.vue'
import Dialog from '@/volt/Dialog.vue'
import { useUiStore } from '@/stores/ui'

const ui = useUiStore()
</script>

<template>
  <Dialog
    v-model:visible="ui.settingsOpen"
    modal
    header="设置"
    :style="{ width: '860px', height: '640px' }"
    content-class="flex min-h-0 flex-1 p-0!"
  >
    <!-- key 让每次打开都重新读一份草稿，不残留上次未保存的改动 -->
    <SettingsView :key="String(ui.settingsOpen)" :anchor="ui.settingsAnchor" />
  </Dialog>
</template>
