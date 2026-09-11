import { defineStore } from 'pinia'
import { ref } from 'vue'

/** 输入对话框的配置。 */
export interface PromptOptions {
  title: string
  /** 输入框上方的说明，可省略。 */
  description?: string
  label?: string
  value?: string
  placeholder?: string
  confirmLabel?: string
}

/** 确认对话框的配置。 */
export interface ConfirmOptions {
  title: string
  message?: string
  confirmLabel?: string
  /** 破坏性操作的确认按钮用醒目色。 */
  danger?: boolean
}

/**
 * 应用级对话框与弹窗的状态。
 *
 * 存在的理由是 Wails 在 macOS 上不支持 window.prompt / window.confirm：
 * Wails 未实现 WKUIDelegate 的 runJavaScriptTextInputPanel / ConfirmPanel，
 * 于是 prompt() 恒返回 null、confirm() 恒返回 false，**不报错也不弹窗**。
 * 新建歌单、重命名、删除曲目等一整批交互就这样静默失效了。
 *
 * 因此：前端一律不得调用 window.prompt / window.confirm / window.alert，
 * 改用这里的 prompt() / confirm()——它们返回 Promise，调用点的写法与原生一致。
 */
export const useUiStore = defineStore('ui', () => {
  const promptOptions = ref<PromptOptions | null>(null)
  const confirmOptions = ref<ConfirmOptions | null>(null)
  /** 设置是弹窗而不是页面：它不是浏览目的地，占用主区会打断当前浏览位置。 */
  const settingsOpen = ref(false)
  /** 设置弹窗打开时定位到的分组。 */
  const settingsAnchor = ref('general')
  const importOpen = ref(false)

  let resolvePrompt: ((value: string | null) => void) | null = null
  let resolveConfirm: ((value: boolean) => void) | null = null

  /** 弹出输入对话框；取消或留空时解析为 null。 */
  function prompt(options: PromptOptions): Promise<string | null> {
    // 同时只允许一个：前一个未决的先按取消结算，避免调用方永远等不到
    resolvePrompt?.(null)
    promptOptions.value = options
    return new Promise((resolve) => {
      resolvePrompt = resolve
    })
  }

  function settlePrompt(value: string | null) {
    promptOptions.value = null
    const resolve = resolvePrompt
    resolvePrompt = null
    resolve?.(value)
  }

  /** 弹出确认对话框。 */
  function confirm(options: ConfirmOptions): Promise<boolean> {
    resolveConfirm?.(false)
    confirmOptions.value = options
    return new Promise((resolve) => {
      resolveConfirm = resolve
    })
  }

  function settleConfirm(value: boolean) {
    confirmOptions.value = null
    const resolve = resolveConfirm
    resolveConfirm = null
    resolve?.(value)
  }

  /** 打开设置弹窗，可指定定位到的分组。 */
  function openSettings(anchor = 'general') {
    settingsAnchor.value = anchor
    settingsOpen.value = true
  }

  return {
    promptOptions,
    confirmOptions,
    settingsOpen,
    settingsAnchor,
    importOpen,
    prompt,
    settlePrompt,
    confirm,
    settleConfirm,
    openSettings,
  }
})
