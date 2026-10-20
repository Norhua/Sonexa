import { ref, shallowRef } from 'vue'

/** One registered list drives options, default selection and dispatch validation. */
export function useSourceSelection<T extends { id: string }>() {
  const options = shallowRef<T[]>([])
  const selected = ref<string[]>([])
  let initialized = false

  function update(list: T[]) {
    options.value = list
    const available = new Set(list.map(source => source.id))
    selected.value = initialized
      ? [...new Set(selected.value)].filter(id => available.has(id))
      : [...available]
    initialized = true
  }

  function choose(only?: string) {
    const available = new Set(options.value.map(source => source.id))
    if (only !== undefined) {
      if (!available.has(only)) throw new Error(`来源未登记或不可用：${only}`)
      return [only]
    }
    selected.value = [...new Set(selected.value)].filter(id => available.has(id))
    return [...selected.value]
  }

  return { options, selected, update, choose }
}
