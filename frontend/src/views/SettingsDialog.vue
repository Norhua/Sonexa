<script setup lang="ts">
/**
 * 设置弹窗。
 *
 * 设置不是浏览目的地：把它做成主内容区的一个视图，就意味着每次改一项设置
 * 都要把用户正在看的歌单顶掉，回来还得自己找回去。弹窗关掉即回到原处。
 */
import { X } from 'lucide-vue-next'
import SettingsView from '@/views/SettingsView.vue'
import { useUiStore } from '@/stores/ui'

const ui = useUiStore()
</script>

<template>
  <div
    v-if="ui.settingsOpen"
    class="fixed inset-0 z-[60] flex items-center justify-center bg-black/40 p-8"
    @click.self="ui.settingsOpen = false"
  >
    <div class="flex h-[640px] max-h-full w-[860px] max-w-full flex-col rounded-lg border bg-card shadow-xl">
      <header class="flex shrink-0 items-center justify-between border-b px-4 py-2">
        <h2 class="text-sm font-semibold">设置</h2>
        <button class="rounded p-1 hover:bg-accent" title="关闭" @click="ui.settingsOpen = false">
          <X class="size-4" />
        </button>
      </header>

      <!-- key 让每次打开都重新读一份草稿，不残留上次未保存的改动 -->
      <SettingsView :key="String(ui.settingsOpen)" :anchor="ui.settingsAnchor" />
    </div>
  </div>
</template>
