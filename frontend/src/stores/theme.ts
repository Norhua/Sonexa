import { defineStore } from 'pinia'
import { ref, watchEffect } from 'vue'

export type Theme = 'light' | 'dark'

/**
 * 按需 Pinia：只有需要跨视图共享的状态才建 store。
 * 主题是其中之一（顶栏切换、全局生效）。
 */
export const useThemeStore = defineStore('theme', () => {
  const theme = ref<Theme>((localStorage.getItem('sonexa.theme') as Theme) ?? 'dark')

  function toggle() {
    theme.value = theme.value === 'dark' ? 'light' : 'dark'
  }

  watchEffect(() => {
    document.documentElement.classList.toggle('dark', theme.value === 'dark')
    localStorage.setItem('sonexa.theme', theme.value)
  })

  return { theme, toggle }
})
