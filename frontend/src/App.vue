<script setup lang="ts">
import { Moon, Sun, Music4 } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { useThemeStore } from '@/stores/theme'

const themeStore = useThemeStore()
</script>

<template>
  <div class="flex h-full flex-col bg-background text-foreground">
    <header class="flex h-12 shrink-0 items-center gap-3 border-b px-4">
      <Music4 class="size-5" />
      <span class="font-semibold">Sonexa</span>
      <div class="flex-1" />
      <Button variant="ghost" size="icon" :title="themeStore.theme === 'dark' ? '切换到亮色' : '切换到暗色'" @click="themeStore.toggle()">
        <Moon v-if="themeStore.theme === 'dark'" />
        <Sun v-else />
      </Button>
    </header>
    <main class="flex flex-1 items-center justify-center">
      <div class="flex flex-col items-center gap-4">
        <p class="text-muted-foreground">Sonexa 应用外壳待实现</p>
        <div class="flex gap-2">
          <Button variant="default">默认按钮</Button>
          <Button variant="secondary">次要</Button>
          <Button variant="outline">描边</Button>
          <Button variant="ghost">幽灵</Button>
          <Button variant="destructive">危险</Button>
        </div>
      </div>
    </main>
  </div>
</template>
