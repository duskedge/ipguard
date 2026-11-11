<script setup lang="ts">
import { onBeforeUnmount, onMounted } from 'vue'

import BanModal from '@/components/BanModal.vue'
import IpDrawer from '@/components/IpDrawer.vue'
import UnbanModal from '@/components/UnbanModal.vue'
import { useUi } from '@/stores/ui'

const ui = useUi()
function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') { ui.closeModal(); ui.closeDrawer() }
}
onMounted(() => document.addEventListener('keydown', onKey))
onBeforeUnmount(() => document.removeEventListener('keydown', onKey))
</script>

<template>
  <RouterView />
  <IpDrawer v-if="ui.drawerIp" :ip="ui.drawerIp" />
  <BanModal v-if="ui.banTarget || ui.manualOpen" :target="ui.banTarget" />
  <UnbanModal v-if="ui.unbanBan || ui.unbanIp" :ban="ui.unbanBan" :ip="ui.unbanIp" />
  <div id="toasts">
    <div v-for="t in ui.toasts" :key="t.id" class="toast" :class="{ bad: t.bad }" role="status">{{ t.msg }}</div>
  </div>
</template>
