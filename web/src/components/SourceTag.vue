<script setup lang="ts">
const MAP: Record<string, [string, string]> = { manual: ['手动', 'manual'], auto: ['自动', 'auto'], community: ['社区', 'community'] }
defineProps<{ source: string }>()
</script>

<template>
  <span class="tag" :class="(MAP[source] ?? [source, 'muted'])[1]">{{ (MAP[source] ?? [source])[0] }}</span>
</template>
