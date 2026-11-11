import { defineStore } from 'pinia'
import { ref, watch } from 'vue'

import type { Ban, Meta } from '@/api/types'

export interface Toast { id: number; msg: string; bad: boolean }

/** 全局界面状态：主题、提示、弹窗/抽屉，以及“数据变了”的信号。 */
export const useUi = defineStore('ui', () => {
  const theme = ref<'dark' | 'light'>(
    (document.documentElement.getAttribute('data-theme') as 'dark' | 'light') || 'dark',
  )
  function toggleTheme() {
    theme.value = theme.value === 'light' ? 'dark' : 'light'
    document.documentElement.setAttribute('data-theme', theme.value)
    try { localStorage.setItem('ipguard-theme', theme.value) } catch { /* ignore */ }
  }

  const toasts = ref<Toast[]>([])
  let seq = 0
  function toast(msg: string, bad = false) {
    const id = ++seq
    toasts.value.push({ id, msg, bad })
    setTimeout(() => { toasts.value = toasts.value.filter((t) => t.id !== id) }, 4000)
  }

  // 弹窗：ban（对某 IP）、manual（手动填写）、unban（某条封禁或某 IP）
  const banTarget = ref<string | null>(null)
  const manualOpen = ref(false)
  const unbanBan = ref<Ban | null>(null)
  const unbanIp = ref<string | null>(null)
  const drawerIp = ref<string | null>(null)

  function openBan(ip: string) { banTarget.value = ip }
  function openManual() { manualOpen.value = true }
  function openUnban(b: Ban) { unbanBan.value = b }
  function openUnbanIp(ip: string) { unbanIp.value = ip }
  function openIp(ip: string) { drawerIp.value = ip }
  function closeModal() { banTarget.value = null; manualOpen.value = false; unbanBan.value = null; unbanIp.value = null }
  function closeDrawer() { drawerIp.value = null }

  // 封禁/解封成功后递增，页面据此重新拉数据
  const dataVersion = ref(0)
  function bump() { dataVersion.value++ }

  const meta = ref<Meta | null>(null)

  // 隐藏内网地址（CI runner 等内网轮询会刷屏）。默认开，记在本浏览器
  const hideLan = ref(readPref('ipguard-hide-lan', '1') === '1')
  watch(hideLan, (v) => { try { localStorage.setItem('ipguard-hide-lan', v ? '1' : '0') } catch { /* ignore */ } })

  return {
    theme, toggleTheme, toasts, toast,
    banTarget, manualOpen, unbanBan, unbanIp, drawerIp,
    openBan, openManual, openUnban, openUnbanIp, openIp, closeModal, closeDrawer,
    dataVersion, bump, meta, hideLan,
  }
})

function readPref(k: string, def: string): string {
  try { return localStorage.getItem(k) ?? def } catch { return def }
}
