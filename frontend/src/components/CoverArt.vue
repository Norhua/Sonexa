<script setup lang="ts">
/**
 * 封面图，带四种加载状态。
 *
 *   无来源     默认图片图标      这首曲目没有可取图的发行标识
 *   加载中     旋转动画          请求已发出，后端可能正在出网取图
 *   失败       断图图标          取图出错（网络、超时、上游 5xx）
 *   无封面     nil 字样          降级链走完都是 404，上游确实没有这张封面
 *
 * 后两者必须分开呈现：「上游没有这张封面」是常态（大量发行在 Cover Art Archive
 * 上没有图），而「取图出错」是可以重试的故障，把它们画成同一个样子，
 * 用户就分不清该不该去点「重新获取封面」。
 *
 * 而 <img> 的 error 事件读不到状态码，因此失败时向后端问一次成因（见 coverStatus）。
 */
import { computed, ref, watch } from 'vue'
import { Image as ImageIcon, ImageOff, LoaderCircle } from 'lucide-vue-next'
import { adoptedCoverUrl, coverUrl, COVER_THUMB_SIZE, type CoverSource } from '@/api'
import { coverOutcome } from '@/lib/coverStatus'

const props = withDefaults(
  defineProps<{
    /** 临时缩略图的来源标识。 */
    source?: CoverSource | null
    /** 已采用封面的落盘路径；非空时优先于 source。 */
    adoptedPath?: string | null
    /** 取图尺寸。列表与详情用同一个值，换尺寸就是另一个缓存键。 */
    size?: number
    /** 图标尺寸类，随容器大小给。 */
    iconClass?: string
    /** nil 字样的字号类。 */
    labelClass?: string
  }>(),
  { size: COVER_THUMB_SIZE, iconClass: 'size-4', labelClass: 'text-[10px]' },
)

type State = 'none' | 'loading' | 'ok' | 'empty' | 'error'
const state = ref<State>('none')

const src = computed(() =>
  props.adoptedPath ? adoptedCoverUrl(props.adoptedPath) : coverUrl(props.source, props.size),
)

// 地址变了就回到加载中：行被复用（表格滚动、切换曲目）时，
// 上一张图的状态不能留在新的一行上。
watch(src, (url) => (state.value = url ? 'loading' : 'none'), { immediate: true })

function onError() {
  if (props.adoptedPath) {
    // 已采用的封面是本地文件，读不到只可能是文件没了，不是「上游没有」
    state.value = 'error'
    return
  }
  void coverOutcome(props.source, props.size).then((o) => {
    // 等待期间这一行可能已经换了图，此时不要把旧结果盖上去
    if (state.value === 'loading') state.value = o
  })
}
</script>

<template>
  <div class="flex items-center justify-center overflow-hidden bg-surface-100 text-muted-color dark:bg-surface-800">
    <img
      v-if="src"
      v-show="state === 'ok'"
      :src="src"
      loading="lazy"
      alt=""
      class="size-full object-cover"
      @load="state = 'ok'"
      @error="onError"
    />
    <ImageIcon v-if="state === 'none'" :class="iconClass" />
    <LoaderCircle v-else-if="state === 'loading'" :class="[iconClass, 'animate-spin']" />
    <ImageOff v-else-if="state === 'error'" :class="[iconClass, 'text-danger-color']" />
    <span v-else-if="state === 'empty'" :class="[labelClass, 'font-mono leading-none opacity-60']">
      nil
    </span>
  </div>
</template>
