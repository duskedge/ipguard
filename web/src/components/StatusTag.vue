<script setup lang="ts">
defineProps<{ level: string; banned?: boolean }>()
</script>

<template>
  <span v-if="banned" class="tag muted">已封禁</span>
  <span v-else-if="level === 'scan'" class="tag danger" title="短时间大量请求敏感路径">可疑</span>
  <span v-else-if="level === 'warn'" class="tag warn" title="4xx/5xx 占比偏高或命中敏感路径">可疑</span>
  <span v-else class="tag ok">正常</span>
</template>
