import { onBeforeUnmount, onMounted, ref } from 'vue'

/** Right-hand drawer: drag its left edge, or use arrow keys on the separator. */
export function useDrawerWidth() {
  const width = ref(340)
  const maxWidth = ref(720)
  const clamp = (value: number) => Math.max(280, Math.min(maxWidth.value, value))
  function persist() { try { localStorage.setItem('sonexa.detailWidth', String(width.value)) } catch { /* storage can be unavailable */ } }
  function resizeViewport() { maxWidth.value = Math.max(280, Math.min(900, window.innerWidth - 320)); width.value = clamp(width.value) }
  onMounted(() => {
    resizeViewport()
    try { const saved = Number(localStorage.getItem('sonexa.detailWidth')); if (saved >= 280) width.value = clamp(saved) } catch { /* default */ }
    window.addEventListener('resize', resizeViewport)
  })
  let cleanup: (() => void) | undefined
  onBeforeUnmount(() => { cleanup?.(); window.removeEventListener('resize', resizeViewport) })
  function start(event: PointerEvent) {
    if (event.button !== 0) return
    cleanup?.()
    const x = event.clientX, initial = width.value
    const move = (e: PointerEvent) => { width.value = clamp(initial + x - e.clientX) }
    const oldCursor = document.body.style.cursor, oldSelect = document.body.style.userSelect
    document.body.style.cursor = 'col-resize'; document.body.style.userSelect = 'none'
    cleanup = () => {
      window.removeEventListener('pointermove', move); window.removeEventListener('pointerup', end); window.removeEventListener('pointercancel', end)
      document.body.style.cursor = oldCursor; document.body.style.userSelect = oldSelect
      cleanup = undefined
    }
    const end = () => { cleanup?.(); persist() }
    window.addEventListener('pointermove', move); window.addEventListener('pointerup', end); window.addEventListener('pointercancel', end)
    event.preventDefault()
  }
  function keydown(event: KeyboardEvent) {
    if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
    event.preventDefault()
    width.value = clamp(event.key === 'Home' ? 280 : event.key === 'End' ? maxWidth.value : width.value + (event.key === 'ArrowLeft' ? 20 : -20))
    persist()
  }
  return { width, maxWidth, start, keydown }
}
