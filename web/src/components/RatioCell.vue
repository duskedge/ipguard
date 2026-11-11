<script setup lang="ts">
import { pct, ratioCls } from '@/utils/format'

defineProps<{ v: number }>()
</script>

<template>
  <span class="ratio" :class="ratioCls(v)"><i aria-hidden="true"><b :style="{ width: Math.round(v * 100) + '%' }" /></i><span class="ratio-value">{{ pct(v) }}</span></span>
</template>
