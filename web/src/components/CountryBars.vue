<script setup lang="ts">
import { computed } from 'vue'

import { flag, fmtNum } from '@/utils/format'

const props = defineProps<{ items: { name: string; cc?: string; req: number; domestic?: boolean }[] }>()
const max = computed(() => Math.max(1, ...props.items.map((i) => i.req)))
</script>

<template>
  <div class="bars">
    <div v-for="it in items" :key="it.name" class="bar">
      <span class="bar-name" :title="it.name">{{ flag(it.cc) }} {{ it.name }}</span>
      <span class="track"><span class="fill" :class="{ cn: it.domestic }" :style="{ width: ((it.req / max) * 100).toFixed(1) + '%' }" /></span>
      <span class="bar-n">{{ fmtNum(it.req) }}</span>
    </div>
    <p v-if="!items.length" class="note">暂无数据</p>
  </div>
</template>
