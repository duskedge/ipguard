import { createRouter, createWebHashHistory } from 'vue-router'

import { useAuth } from '@/stores/auth'

// 与原型一致使用 hash 路由（#/logs），刷新与嵌入部署都不依赖服务端路由。
export const TITLES: Record<string, string> = {
  dashboard: '概览', logs: '访问日志', ips: 'IP 统计', hosts: '域名统计', geo: '归属地', bans: '封禁管理', settings: '设置',
}

export const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/login', name: 'login', component: () => import('@/views/LoginView.vue') },
    {
      path: '/',
      component: () => import('@/views/ShellLayout.vue'),
      children: [
        { path: '', redirect: '/dashboard' },
        { path: 'dashboard', name: 'dashboard', component: () => import('@/views/DashboardView.vue') },
        { path: 'logs', name: 'logs', component: () => import('@/views/LogsView.vue') },
        { path: 'ips', name: 'ips', component: () => import('@/views/IpsView.vue') },
        { path: 'hosts', name: 'hosts', component: () => import('@/views/HostsView.vue') },
        { path: 'geo', name: 'geo', component: () => import('@/views/GeoView.vue') },
        { path: 'settings', name: 'settings', component: () => import('@/views/SettingsView.vue') },
        { path: 'bans', name: 'bans', component: () => import('@/views/BansView.vue') },
      ],
    },
    { path: '/:pathMatch(.*)*', redirect: '/dashboard' },
  ],
})

router.beforeEach((to) => {
  const auth = useAuth()
  if (to.name !== 'login' && !auth.token) {
    return { name: 'login', query: to.fullPath !== '/' ? { next: to.fullPath } : {} }
  }
  if (to.name === 'login' && auth.token) return { name: 'dashboard' }
})

router.afterEach((to) => {
  const t = TITLES[String(to.name)]
  document.title = t ? `${t} · ipguard` : '登录 · ipguard'
})

// 发版后旧页面拉不到旧 chunk：整页刷新拿新入口（加标记防止死循环）
router.onError((err, to) => {
  const msg = String((err as Error)?.message ?? err)
  if (/dynamically imported module|Importing a module script failed|Failed to fetch/i.test(msg)) {
    const key = 'ipguard-chunk-reload'
    try {
      if (sessionStorage.getItem(key) === to.fullPath) return
      sessionStorage.setItem(key, to.fullPath)
    } catch { /* ignore */ }
    location.hash = '#' + to.fullPath
    location.reload()
  }
})
