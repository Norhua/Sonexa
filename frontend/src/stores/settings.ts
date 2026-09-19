import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { App, type Models } from '@/api'

/**
 * 设置与出网前置条件。
 *
 * 出网禁用态是全应用共享的状态——导入、上游搜索、封面获取三处入口
 * 都据此禁用，且共用同一句原因说明。
 */
export const useSettingsStore = defineStore('settings', () => {
  const settings = ref<Models.config.Settings | null>(null)
  const providers = ref<Models.app.ProviderInfo[]>([])
  const gate = ref<Models.app.OutboundGate>({
    allowed: false,
    reason: '',
    settingsAnchor: 'upstream',
  })
  const loading = ref(false)
  const error = ref('')

  /** 出网是否可用。未配置 MusicBrainz 联系方式时全局禁用。 */
  const outboundAllowed = computed(() => gate.value.allowed)
  /** 全部禁用入口共用的原因说明。 */
  const outboundReason = computed(() => gate.value.reason)
  /** 首次启动引导是否仍需展示。 */
  const needsOnboarding = computed(
    () => settings.value !== null && !settings.value.onboardingDone && !gate.value.allowed,
  )

  async function load() {
    loading.value = true
    error.value = ''
    try {
      const [s, p, g] = await Promise.all([
        App.GetSettings(),
        App.ListProviders(),
        App.CheckOutbound(),
      ])
      settings.value = s
      providers.value = p ?? []
      gate.value = g
    } catch (e) {
      error.value = String(e)
    } finally {
      loading.value = false
    }
  }

  async function save(next: Models.config.Settings) {
    await App.SaveSettings(next)
    // 保存后立即重读：填写联系方式后出网入口应立即可用，无需重启
    await load()
  }

  async function completeOnboarding() {
    await App.CompleteOnboarding()
    await load()
  }

  /** 需要地区参数的上游是否在给定集合中被勾选。 */
  function anyNeedsCountry(selected: string[]): boolean {
    return providers.value.some((p) => selected.includes(p.id) && p.needsCountry)
  }

  /** 取某上游的信息。 */
  function providerById(id: string) {
    return providers.value.find((p) => p.id === id)
  }

  return {
    settings,
    providers,
    gate,
    loading,
    error,
    outboundAllowed,
    outboundReason,
    needsOnboarding,
    load,
    save,
    completeOnboarding,
    anyNeedsCountry,
    providerById,
  }
})
