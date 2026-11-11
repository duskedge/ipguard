<script setup lang="ts">
// 手动刷新当前页面数据：递增 ui.dataVersion，所有 useLoader 都会重新拉取。
import { RefreshCw } from 'lucide-vue-next'
import { ref } from 'vue'

import { useUi } from '@/stores/ui'

const ui = useUi()
const spinning = ref(false)
let t: ReturnType<typeof setTimeout> | undefined

function refresh() {
  ui.bump()
  spinning.value = true
  clearTimeout(t)
  t = setTimeout(() => { spinning.value = false }, 700)
}
</script>

<template>
  <button type="button" class="btn sm refresh" title="刷新数据" aria-label="刷新数据" @click="refresh">
    <RefreshCw class="i" :class="{ spin: spinning }" :size="14" />刷新
  </button>
</template>

<style scoped>
.refresh { height: 34px; }
.spin { animation: spin .7s linear; }
@keyframes spin { to { transform: rotate(360deg); } }
</style>
