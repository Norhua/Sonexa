<script setup lang="ts">
import GroupProposals from '@/components/GroupProposals.vue'
/**
 * 详情抽屉。宽度 340px。
 *
 * 纵向顺序：封面 → 歌名 → 艺术家 → 参与者 → 发行信息 → 厂牌与目录号
 * → 标签 chips → 编辑按钮。
 *
 * 必须优雅处理丰富信息为空的情况（「无需匹配」入库的曲目）：
 * 缺失字段显示占位短横线，不报错、不出现空白区域。
 */
import { computed, ref, watch } from 'vue'
import { X, Pencil } from 'lucide-vue-next'
import { App, display, duration, DASH } from '@/api'
import CoverArt from '@/components/CoverArt.vue'
import { creditParts } from '@/lib/creditParts'
import { lyricsText } from '@/lib/lyrics'
import Button from '@/volt/Button.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import Tag from '@/volt/Tag.vue'
import ContextMenu from '@/volt/ContextMenu.vue'
import { useSettingsStore } from '@/stores/settings'
import { useUiStore } from '@/stores/ui'
import { useCoverSearchStore } from '@/stores/coverSearch'
import type { Models } from '@/api'

const props = defineProps<{
  detail: Models.app.TrackDetail | null
  open: boolean
}>()

const emit = defineEmits<{
  close: []
  edit: [trackId: number]
  viewArtist: [artistId: number]
  coverAdopted: []
}>()

/** 封面操作始终固定到打开菜单时的所属歌集。 */
const menu = ref<InstanceType<typeof ContextMenu>>()
const coverTrigger = ref<HTMLElement>()
const settings = useSettingsStore()
const ui = useUiStore()
const coverSearch = useCoverSearchStore()
const coverError = ref('')
const menuDetail = ref<Models.app.TrackDetail | null>(null)
const menuItems = computed(() => [{
  label: '更新歌曲图片',
  items: [
    { label: '从本地存储中选择', disabled: !menuDetail.value?.track.releaseId, command: pickLocal },
    { label: '从网络搜索', disabled: !menuDetail.value?.track.releaseId || !settings.outboundAllowed, command: searchNetwork },
  ],
}, { separator: true }, {
  label: '清除封面',
  disabled: !menuDetail.value?.track.releaseId,
  command: clearCover,
}])
async function pickLocal() {
  const releaseId = menuDetail.value?.track.releaseId
  if (!releaseId) return
  try {
    const revision = await App.CoverRevision(releaseId)
    const result = await App.PickAndAdoptCover(releaseId, revision)
    if (!result.cancelled) emit('coverAdopted')
  } catch (e) { coverError.value = String(e) }
}
/** 删除该歌集已保存的封面文件；清除后不再自动获取，直到重新选择图片。 */
async function clearCover() {
  const releaseId = menuDetail.value?.track.releaseId
  if (!releaseId) return
  const ok = await ui.confirm({
    title: '清除专辑封面？',
    message: '将删除此专辑已保存的全部封面文件，之后不再自动获取封面，直到重新选择图片。',
    confirmLabel: '清除',
    danger: true,
  })
  if (!ok) return
  try {
    const revision = await App.CoverRevision(releaseId)
    await App.ClearCover(releaseId, revision)
    emit('coverAdopted')
  } catch (e) { coverError.value = String(e) }
}
function searchNetwork() {
  if (!menuDetail.value?.track.releaseId || !settings.outboundAllowed) return
  menu.value?.hide()
  coverTrigger.value?.focus({ preventScroll: true })
  void coverSearch.open(menuDetail.value)
}
function showMenu(event: MouseEvent) {
  // 鼠标右键默认不会聚焦容器；先聚焦，菜单 Escape/取消后才能回到封面。
  if (event.currentTarget instanceof HTMLElement) event.currentTarget.focus({ preventScroll: true })
  menuDetail.value = props.detail
  menu.value?.show(event)
}
function keyboardMenu(event: KeyboardEvent) {
  if (event.key === 'ContextMenu' || (event.shiftKey && event.key === 'F10')) {
    event.preventDefault()
    const rect = (event.currentTarget as HTMLElement).getBoundingClientRect()
    showMenu(new MouseEvent('contextmenu', { clientX: rect.left + 20, clientY: rect.top + 20 }))
  }
}
watch(() => props.open, open => { if (!open) menu.value?.hide() })
watch(() => props.detail?.track.id, () => { coverError.value = ''; menu.value?.hide() })

const meta = computed(() => props.detail?.metadata ?? null)
const lyrics = computed(() => lyricsText(meta.value?.lyrics))
const fallbackCredit = computed(() => creditParts(
  props.detail?.creditText || props.detail?.track.artist || '',
  props.detail?.creditSpans ?? [],
))

/** 音轨总数取当前碟，不以整张发行总数或收藏数替代。 */
const mediumTrackTotal = computed(() => props.detail?.release?.media?.find(
  medium => medium.number === meta.value?.discNumber,
)?.trackTotal)

/** 发行信息：缺失字段及未知位置/总数分别显示占位短横线。 */
const releaseInfo = computed(() => [
  { label: '专辑', value: display(props.detail?.release?.title) },
  { label: '专辑艺术家', value: display(props.detail?.release?.credit) },
  { label: '专辑类型', value: display(props.detail?.release?.primaryType ?? meta.value?.primaryType) },
  { label: '年份', value: display(meta.value?.year) },
  { label: '时长', value: duration(meta.value?.durationMs) },
  { label: '音轨', value: `${display(meta.value?.trackNumber)}/${display(mediumTrackTotal.value)}` },
  { label: '碟号', value: `${display(meta.value?.discNumber)}/${display(props.detail?.release?.discTotal)}` },
  { label: '每碟曲数', value: props.detail?.release?.media?.map(m => `第 ${m.number} 碟 ${display(m.trackTotal)}`).join(' · ') || DASH },
  { label: '风格', value: display(meta.value?.genre) },
  { label: '发行收藏数', value: display(props.detail?.release?.ownedCount) },
  { label: '发行总曲数', value: display(props.detail?.release?.totalCount) },
  { label: '国家/地区', value: display(meta.value?.country) },
  { label: '载体', value: display(meta.value?.mediumFormat) },
])
</script>

<template>
  <aside
    v-if="open && detail"
    class="flex w-[340px] shrink-0 flex-col border-l bg-app-chrome"
  >
    <header class="flex shrink-0 items-center justify-between border-b px-4 py-2">
      <span class="text-xs text-muted-color">详情</span>
      <SecondaryButton variant="text" rounded size="small" title="关闭" @click="emit('close')">
        <X class="size-4" />
      </SecondaryButton>
    </header>

    <div class="min-h-0 flex-1 space-y-4 overflow-auto p-4">
      <!-- 封面 -->
      <div ref="coverTrigger" class="flex justify-center" tabindex="0" role="button" aria-label="更新歌曲图片"
        @contextmenu.prevent="showMenu" @keydown="keyboardMenu">
        <CoverArt
          :source="{
            releaseId: detail.track.releaseId,
            revision: detail.track.coverRevision,
            provider: detail.coverProvider,
            ref: detail.coverRef,
            rg: detail.coverGroupRef,
          }"
          usage="detail"
          icon-class="size-10"
          label-class="text-sm"
          class="aspect-square w-full max-w-[280px] rounded-md shadow"
        />
      </div>
      <!-- 歌名与艺术家 -->
      <div class="flex items-center gap-3">
        <div class="min-w-0 flex-1 break-words">
          <h2 class="text-lg font-semibold leading-tight">{{ detail.track.title }}</h2>
          <p class="mt-1 text-sm text-muted-color">{{ display(detail.track.artist) }}</p>
        </div>
      </div>

      <p v-if="!detail.track.releaseId" class="text-xs text-muted-color">此曲目没有所属专辑，无法更新共享封面</p>
      <p v-if="coverError" role="alert" class="text-xs text-danger-color">{{ coverError }}</p>

      <!-- 参与者 -->
      <section>
        <h3 class="mb-2 text-xs font-medium text-muted-color">参与者</h3>
        <dl class="detail-fields text-sm">
          <!-- 角色统一分组；CV 兜底明确标注，不冒充上游演唱事实 -->
          <div v-for="group in detail.participants" :key="group.role" class="contents">
            <dt class="text-xs text-muted-color">{{ group.label }}</dt>
            <dd class="min-w-0 break-words">
              <template v-for="(artist, i) in group.artists" :key="artist.artistId">
                <span v-if="i > 0">、</span>
                <button
                  class="underline decoration-dotted underline-offset-2 hover:decoration-solid"
                  @click="emit('viewArtist', artist.artistId)"
                >{{ artist.name }}</button>
                <span v-if="artist.attribute" class="text-xs text-muted-color">
                  ({{ artist.attribute }})
                </span>
                <span v-if="artist.source === 'credit_cv'" class="text-xs text-muted-color">（来自 CV 署名）</span>
              </template>
            </dd>
          </div>

          <template v-if="detail.participants.length === 0">
            <!-- 署名说明谁被署名，不推断演唱。仅在无角色时兜底，避免完整列表重复。 -->
            <div v-if="fallbackCredit.length" class="contents">
              <dt class="text-xs text-muted-color">署名</dt>
              <dd class="min-w-0 break-words">
                <template v-for="(part, i) in fallbackCredit" :key="i">
                  <button v-if="part.artistId"
                    class="underline decoration-dotted underline-offset-2 hover:decoration-solid"
                    @click="emit('viewArtist', part.artistId)"
                  >{{ part.text }}</button><span v-else>{{ part.text }}</span>
                </template>
              </dd>
            </div>
          </template>
        </dl>
        <p v-if="detail.participants.length === 0" class="mt-1.5 text-xs text-muted-color">
          <span>暂无参与角色信息</span><span v-if="meta?.matchSource && !settings.providerById(meta.matchSource)?.capabilities?.includes('PerformerRoles')">：当前来源不提供演唱者角色，仍可在编辑页补录</span><span v-else>：尚未提供明确角色信息</span>
        </p>
      </section>

      <GroupProposals v-if="detail.track.releaseId" :release-id="detail.track.releaseId" />
      <!-- 发行信息 -->
      <section>
        <h3 class="mb-2 text-xs font-medium text-muted-color">发行信息</h3>
        <dl class="detail-fields text-sm">
          <div v-for="item in releaseInfo" :key="item.label" class="contents">
            <dt class="text-xs text-muted-color">{{ item.label }}</dt>
            <dd class="min-w-0 break-words">{{ item.value }}</dd>
          </div>
        </dl>
      </section>

      <!-- 厂牌与目录号 -->
      <section>
        <h3 class="mb-2 text-xs font-medium text-muted-color">厂牌与目录号</h3>
        <dl class="detail-fields text-sm">
          <div class="contents">
            <dt class="text-xs text-muted-color">厂牌</dt>
            <dd class="min-w-0 break-words">{{ display(meta?.label) }}</dd>
          </div>
          <div class="contents">
            <dt class="text-xs text-muted-color">目录号</dt>
            <dd class="min-w-0 break-words">{{ display(meta?.catalogNumber) }}</dd>
          </div>
        </dl>
      </section>

      <!-- 歌词：上游事实，只读；默认折叠，避免长文本挤占抽屉 -->
      <section v-if="lyrics">
        <details>
          <summary class="mb-2 cursor-pointer text-xs font-medium text-muted-color">歌词</summary>
          <p class="whitespace-pre-line break-words text-sm leading-relaxed">{{ lyrics }}</p>
        </details>
      </section>

      <!-- 标签 chips。歌单不在抽屉显示 -->
      <section>
        <h3 class="mb-2 text-xs font-medium text-muted-color">标签</h3>
        <div v-if="detail.tags.length > 0" class="flex flex-wrap gap-1.5">
          <Tag
            v-for="tag in detail.tags"
            :key="tag.id"
            severity="secondary"
            rounded
            :value="`#${tag.name}`"
            class="text-xs font-normal"
          />
        </div>
        <p v-else class="text-sm text-muted-color">{{ DASH }}</p>
      </section>
    </div>

    <ContextMenu ref="menu" :model="menuItems" />
    <!-- 底部明确的编辑按钮——右键不应是进入编辑的唯一入口 -->
    <footer class="shrink-0 border-t p-3">
      <Button fluid @click="emit('edit', detail.track.id)">
        <Pencil class="size-4" />
        编辑
      </Button>
    </footer>
  </aside>
</template>

<style scoped>
/* 每个小标题独立计算标签宽度，不再用固定宽度强迫所有段落对齐。 */
.detail-fields {
  display: grid;
  grid-template-columns: max-content minmax(0, 1fr);
  align-items: baseline;
  gap: 0.375rem 0.5rem;
}
.detail-fields button { text-align: left; overflow-wrap: anywhere; }
</style>
