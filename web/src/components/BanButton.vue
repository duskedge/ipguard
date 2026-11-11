<script setup lang="ts">
import { useUi } from '@/stores/ui'
import { isPrivateIp } from '@/utils/format'

defineProps<{ ip: string; banned?: boolean }>()
const ui = useUi()
</script>

<template>
  <button v-if="banned" type="button" class="btn sm ghost-danger" @click="ui.openUnbanIp(ip)">解封</button>
  <button v-else-if="!isPrivateIp(ip)" type="button" class="btn sm ghost-danger" @click="ui.openBan(ip)">封禁</button>
</template>
