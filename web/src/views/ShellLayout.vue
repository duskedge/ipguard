<script setup lang="ts">
import { Ban, FileText, Globe, LayoutDashboard, List, Moon, Plus, Server, Settings, Shield, Sun } from 'lucide-vue-next'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { api } from '@/api'
import { TITLES } from '@/router'
import { useAuth } from '@/stores/auth'
import { useUi } from '@/stores/ui'

const ui = useUi()
const auth = useAuth()
const route = useRoute()
const router = useRouter()
const mainEl = ref<HTMLElement | null>(null)

const NAV = [
  { name: 'dashboard', label: '概览', icon: LayoutDashboard },
  { name: 'logs', label: '访问日志', icon: FileText },
  { name: 'ips', label: 'IP 统计', icon: List },
  { name: 'hosts', label: '域名统计', icon: Server },
  { name: 'geo', label: '归属地', icon: Globe },
  { name: 'bans', label: '封禁管理', icon: Ban },
  { name: 'settings', label: '设置', icon: Settings },
]

const title = computed(() => TITLES[String(route.name)] ?? '')
const userName = computed(() => ui.meta?.user || auth.user || 'admin')
const cs = computed(() => ui.meta?.crowdsec)
const csText = computed(() => {
  if (!cs.value) return '正在连接 CrowdSec'
  if (!cs.value.configured) return 'CrowdSec 未配置'
  return cs.value.connected ? 'CrowdSec 已连接' : 'CrowdSec 连接失败'
})
const csAddr = computed(() => (cs.value?.url || '').replace(/^https?:\/\//, ''))
const version = computed(() => ui.meta?.build.version ?? '')

async function loadMeta() {
  try { ui.meta = await api.meta() } catch { /* 401 会被拦截器处理 */ }
}
let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => { loadMeta(); timer = setInterval(loadMeta, 30_000) })
onBeforeUnmount(() => clearInterval(timer))
watch(() => ui.dataVersion, loadMeta)
watch(() => route.name, () => {
  if (mainEl.value) mainEl.value.scrollTop = 0
  window.scrollTo({ top: 0 })
})

function logout() {
  auth.logout()
  ui.closeModal()
  ui.closeDrawer()
  router.replace({ name: 'login' })
}
</script>

<template>
  <div id="shell">
    <aside class="sidebar">
      <div class="brand">
        <span class="mark"><Shield :size="18" :stroke-width="1.75" /></span>
        <span><strong>ipguard</strong><em>{{ ui.meta?.site || 'npm-guard' }}</em></span>
      </div>
      <nav class="nav" aria-label="主导航">
        <RouterLink
          v-for="n in NAV" :key="n.name" :to="{ name: n.name }" class="nav-item"
          :class="{ active: route.name === n.name }" :aria-current="route.name === n.name ? 'page' : undefined"
        >
          <component :is="n.icon" class="i" :size="16" :stroke-width="1.75" /><span>{{ n.label }}</span>
        </RouterLink>
      </nav>
      <div class="side-foot" :title="cs?.error || ''">
        <span class="dot" :class="{ off: cs && cs.configured && !cs.connected, idle: !cs || !cs.configured }" />
        <span>{{ csText }}<small>{{ csAddr || '—' }}</small></span>
      </div>
    </aside>
    <div class="content">
      <header class="topbar">
        <div class="crumb">{{ title }}</div>
        <div class="top-actions">
          <button v-if="route.name === 'bans'" type="button" class="btn primary" @click="ui.openManual()">
            <Plus class="i" :size="16" />手动封禁
          </button>
        </div>
        <button type="button" class="btn sm ghost" @click="ui.toggleTheme()">
          <Sun v-if="ui.theme !== 'light'" class="i" :size="16" /><Moon v-else class="i" :size="16" />
          {{ ui.theme !== 'light' ? '浅色' : '深色' }}
        </button>
        <div class="who">
          <span class="avatar">{{ userName.slice(0, 1).toUpperCase() }}</span>
          <span id="user-name">{{ userName }}</span>
        </div>
        <button type="button" class="btn sm ghost" @click="logout">退出</button>
        <span v-if="version" class="ver" :title="`commit ${ui.meta?.build.git_commit} · ${ui.meta?.build.build_time}`">{{ version }}</span>
      </header>
      <main id="main" ref="mainEl">
        <RouterView />
      </main>
    </div>
  </div>
</template>
