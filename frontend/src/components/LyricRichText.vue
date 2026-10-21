<script setup lang="ts">
import { computed } from 'vue'
import { lyricRubySegments } from '@/lib/lyricsPresentation'
import type { LyricData } from '@/lib/lyricsPresentation'
import type { Models } from '@/api'
const props = defineProps<{ text: string; ruby?: LyricData<Models.lyricdoc.Ruby>[] }>()
const segments = computed(() => lyricRubySegments(props.text, props.ruby))
</script>
<template><span><template v-for="(segment, i) in segments" :key="i"><ruby v-if="segment.reading">{{ segment.text }}<rp>(</rp><rt>{{ segment.reading }}</rt><rp>)</rp></ruby><template v-else>{{ segment.text }}</template></template></span></template>
