interface DialogEntry { close: () => void; escape: () => boolean }
const stack: DialogEntry[] = []

/** PrimeVue 4 的每实例 document 监听会一键关闭所有弹窗；这里仅通知最上层。 */
function onKeyDown(event: KeyboardEvent) {
  // Select/Popover 先处理 Escape 并 preventDefault；不能连带关闭它的父弹窗。
  if (event.key !== 'Escape' || event.isComposing || event.defaultPrevented) return
  const top = stack.at(-1)
  if (!top?.escape()) return
  event.preventDefault()
  event.stopImmediatePropagation()
  top.close()
}
export function registerDialog(entry: DialogEntry) {
  if (!stack.length) document.addEventListener('keydown', onKeyDown)
  stack.push(entry)
  return () => {
    const index = stack.indexOf(entry)
    if (index >= 0) stack.splice(index, 1)
    if (!stack.length) document.removeEventListener('keydown', onKeyDown)
  }
}
