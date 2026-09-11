<script setup lang="ts">
import { cn } from '@/lib/utils'
import { buttonVariants, type ButtonVariants } from '.'

const props = withDefaults(
  defineProps<{
    variant?: ButtonVariants['variant']
    size?: ButtonVariants['size']
    class?: string
    disabled?: boolean
    title?: string
  }>(),
  { variant: 'default', size: 'default' },
)
</script>

<template>
  <button
    :class="cn(buttonVariants({ variant: props.variant, size: props.size }), props.class)"
    :disabled="props.disabled"
    :title="props.title"
  >
    <slot />
  </button>
</template>
