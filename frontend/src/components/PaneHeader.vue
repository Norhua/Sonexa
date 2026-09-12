<script setup lang="ts">
/**
 * 各栏顶部的统一头部外壳。
 *
 * 只负责三件事：固定高度、内容垂直居中、可拖拽窗口。三栏页面的
 * 「大标题」与「操作区」分处两栏，靠共用这一个高度常量保持在同一水平线上——
 * 各写各的 padding 必然漂移。
 *
 * 没有下边框：头部与下方列表在视觉上连成一片，是 Apple Music 那种通透感的来源。
 */
</script>

<template>
  <!-- --wails-draggable 会继承给子元素，因此内部的输入框与按钮必须各自
       声明 no-drag（见 ViewSearchBox / SortMenu），否则点一下就开始拖窗口 -->
  <header
    class="flex h-12 shrink-0 items-center gap-3 px-4"
    style="--wails-draggable: drag"
  >
    <slot />
  </header>
</template>
