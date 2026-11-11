<script setup lang="ts">
import { useUi } from '@/stores/ui'
import { flag } from '@/utils/format'

defineProps<{ ip: string; tag?: string; geo: string; cc: string; banned?: boolean }>()
const ui = useUi()
</script>

<template>
  <div class="ipstack">
    <button type="button" class="link mono" @click="ui.openIp(ip)">{{ ip }}</button>
    <span v-if="tag" class="tag ip-tag">{{ tag }}</span>
    <span v-if="banned" class="tag muted">已封禁</span>
    <div class="place">{{ flag(cc) }} {{ geo }}</div>
  </div>
</template>
