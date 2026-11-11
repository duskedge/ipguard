<script setup lang="ts">
import { Bell, Check, ChevronDown, Database, Download, GlobeLock, Layers, Plus, RefreshCw, Send, ShieldAlert, Tag, Trash2, X } from 'lucide-vue-next'
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { api, errText } from '@/api'
import type { AddressSet, AutoBanDuration, AutoBanSettings, GeoIPStatus, HostRuleMode, HostRuleStat, HostRuleView, IPTagItem, NotificationSettings } from '@/api/types'
import IpListInput from '@/components/IpListInput.vue'
import PathListInput from '@/components/PathListInput.vue'
import MultiSelectBox from '@/components/MultiSelectBox.vue'
import SelectBox, { type SelectOption } from '@/components/SelectBox.vue'
import { useUi } from '@/stores/ui'
import { fmtFull, fmtNum, fmtSize } from '@/utils/format'

const ui = useUi()
const route = useRoute()
const router = useRouter()
const settingsTabs = [
  { value: 'domains', label: '域名访问控制', icon: GlobeLock },
  { value: 'autoban', label: '自动封禁', icon: ShieldAlert },
  { value: 'address-sets', label: '放行地址集', icon: Layers },
  { value: 'ip-tags', label: 'IP 标识', icon: Tag },
  { value: 'notifications', label: '推送通知', icon: Bell },
  { value: 'geoip', label: 'GeoIP 数据库', icon: Database },
] as const
type SettingsTab = typeof settingsTabs[number]['value']
const activeTab = computed<SettingsTab>(() => settingsTabs.find((tab) => tab.value === route.query.section)?.value ?? 'domains')
function selectSettingsTab(tab: SettingsTab) {
  closeHostDropdown()
  router.replace({ query: { ...route.query, section: tab } })
  nextTick(() => {
    document.getElementById(`settings-tab-${tab}`)?.scrollIntoView({ block: 'nearest', inline: 'nearest' })
  })
}
function onSettingsTabKeydown(event: KeyboardEvent, index: number) {
  let next = index
  if (event.key === 'ArrowRight') next = (index + 1) % settingsTabs.length
  else if (event.key === 'ArrowLeft') next = (index + settingsTabs.length - 1) % settingsTabs.length
  else if (event.key === 'Home') next = 0
  else if (event.key === 'End') next = settingsTabs.length - 1
  else return
  event.preventDefault()
  const tab = settingsTabs[next]!
  selectSettingsTab(tab.value)
  document.getElementById(`settings-tab-${tab.value}`)?.focus()
}
const tagItems = ref<IPTagItem[]>([])
const tagLoading = ref(true)
const tagSaving = ref(false)
const tagError = ref('')

interface EditableHostRule {
  hosts: string[]
  hostInput: string
  enabled: boolean
  mode: HostRuleMode
  allowIpsText: string
  allowSets: string[]
  allowTags: string[]
  note: string
  blockedCount: number
  lastBlockedIp: string
  lastBlockedAt: number
  hostStats: Record<string, HostRuleStat>
}

const hostRules = ref<EditableHostRule[]>([])
const knownHosts = ref<string[]>([])
const hostRuleLoading = ref(true)
const hostRuleSaving = ref(false)
const hostRuleError = ref('')
const addressEditors = ref<Record<number, InstanceType<typeof IpListInput> | null>>({})
function setAddressEditor(idx: number, el: unknown) {
  addressEditors.value[idx] = el as InstanceType<typeof IpListInput> | null
}

function normalizeDomainToken(raw: string): string {
  let s = raw.trim().toLowerCase()
  s = s.replace(/^https?:\/\//, '')
  const slashIdx = s.indexOf('/')
  if (slashIdx >= 0) s = s.slice(0, slashIdx)
  const colonIdx = s.indexOf(':')
  if (colonIdx >= 0) s = s.slice(0, colonIdx)
  return s.replace(/\.$/, '')
}

function splitDomainInput(raw: string): string[] {
  const out: string[] = []
  for (const part of raw.split(/[\s,;，；]+/)) {
    const h = normalizeDomainToken(part)
    if (h && !out.includes(h)) out.push(h)
  }
  return out
}

function effectiveRuleHosts(rule: EditableHostRule): string[] {
  const out = [...rule.hosts]
  for (const h of splitDomainInput(rule.hostInput)) {
    if (!out.includes(h)) out.push(h)
  }
  return out
}

function ruleDisplayLabel(rule: EditableHostRule): string {
  const list = effectiveRuleHosts(rule)
  return list.length ? list.join(', ') : ''
}

function ruleStatTooltip(rule: EditableHostRule): string {
  const lines = [`累计拦截 ${rule.blockedCount} 次`]
  if (rule.hosts.length > 1) {
    const parts = rule.hosts
      .map((h) => `${h}: ${rule.hostStats[h]?.blocked_count ?? 0} 次`)
    lines.push(`按域名分布（${parts.join('，')}）`)
  }
  if (rule.lastBlockedIp) {
    lines.push(`最近拦截：${rule.lastBlockedIp}（${fmtFull(rule.lastBlockedAt)}）`)
  }
  lines.push('点击查看近 7 天采样记录（支持按域名单独筛选）')
  return lines.join('；')
}

interface EditableAddressSet extends AddressSet { ipsText: string; key: number }
const addressSets = ref<AddressSet[]>([])
const setItems = ref<EditableAddressSet[]>([])
const setsLoading = ref(true)
const setsSaving = ref(false)
const setsError = ref('')
let nextSetKey = 0
const setEditors = ref<Record<number, InstanceType<typeof IpListInput> | null>>({})
function applySets(sets: AddressSet[]) {
  addressSets.value = sets
  setItems.value = sets.map((a) => ({ ...a, ipsText: a.ips.join('\n'), key: nextSetKey++ }))
}
async function loadAddressSets() {
  setsLoading.value = true
  try { applySets((await api.addressSetSettings()).sets) }
  catch (e) { setsError.value = errText(e) }
  finally { setsLoading.value = false }
}
function addAddressSet() {
  setItems.value.push({ id: '', name: '', ips: [], ipsText: '', note: '', hosts: [], key: nextSetKey++ })
}
function referencingHosts(id: string): string[] {
  const fromRules = hostRules.value
    .filter((r) => r.allowSets.includes(id))
    .flatMap((r) => {
      const hosts = effectiveRuleHosts(r)
      return hosts.length ? hosts : ['未保存域名']
    })
  return [...new Set([...(addressSets.value.find((a) => a.id === id)?.hosts ?? []), ...fromRules])]
}
async function saveAddressSets() {
  for (const a of setItems.value) {
    const editor = setEditors.value[a.key]
    if (editor && !editor.commit()) { editor.focus(); return }
  }
  setsSaving.value = true; setsError.value = ''
  try {
    const sets = setItems.value.map((a) => ({ id: a.id, name: a.name.trim(), tag: a.tag?.trim(), ips: a.ipsText.split(/[\s,;，；]+/).filter(Boolean), note: a.note?.trim() }))
    applySets((await api.saveAddressSets(sets)).sets)
    ui.toast('地址集已保存，引用域名同步生效')
  } catch (e) { setsError.value = errText(e) }
  finally { setsSaving.value = false }
}

const modeOptions: SelectOption[] = [
  { value: 'domestic', label: '仅限国内 IP（中国大陆 + 内网）' },
  { value: 'allowlist', label: '仅限指定 IP / 标识（+ 内网）' },
]

const notifyLevelOptions: SelectOption[] = [
  { value: 'warn', label: '可疑与扫描异常' },
  { value: 'scan', label: '仅扫描异常' },
]

// ── 自定义域名多选输入+下拉组件状态 ─────────────────────────────────────────
const openHostIdx = ref<number | null>(null)
const hostFilterTyped = ref(false)
const hostActiveIdx = ref(0)
const hostDropUp = ref(false)
const hostPopupStyle = ref<Record<string, string>>({})
const hostComboRefs = ref<Record<number, HTMLElement | null>>({})
const hostInputRefs = ref<Record<number, HTMLInputElement | null>>({})
const hostListRefs = ref<Record<number, HTMLElement | null>>({})

function setHostComboRef(idx: number, el: unknown) {
  hostComboRefs.value[idx] = el as HTMLElement | null
}
function setHostInputRef(idx: number, el: unknown) {
  hostInputRefs.value[idx] = el as HTMLInputElement | null
}
function setHostListRef(idx: number, el: unknown) {
  hostListRefs.value[idx] = el as HTMLElement | null
}

function filteredHostsFor(rule: EditableHostRule): string[] {
  const k = hostFilterTyped.value ? rule.hostInput.trim().toLowerCase() : ''
  if (!k) return knownHosts.value
  return knownHosts.value.filter((h) => h.toLowerCase().includes(k))
}

function customHostCandidate(rule: EditableHostRule): string {
  const tokens = splitDomainInput(rule.hostInput)
  if (!tokens.length) return ''
  const newOnes = tokens.filter((t) => !rule.hosts.includes(t) && !knownHosts.value.includes(t))
  return newOnes.join(', ')
}

function isHostUsedElsewhere(idx: number, host: string): boolean {
  const h = host.toLowerCase()
  return hostRules.value.some((r, i) => i !== idx && r.hosts.includes(h))
}

function canSelectAllFor(idx: number): boolean {
  const rule = hostRules.value[idx]
  if (!rule) return false
  return filteredHostsFor(rule).some((h) => !rule.hosts.includes(h) && !isHostUsedElsewhere(idx, h))
}

function selectAllAvailableHosts(idx: number) {
  const rule = hostRules.value[idx]
  if (!rule) return
  for (const h of filteredHostsFor(rule)) {
    if (!rule.hosts.includes(h) && !isHostUsedElsewhere(idx, h)) {
      rule.hosts.push(h)
    }
  }
  rule.hostInput = ''
  hostFilterTyped.value = false
  nextTick(() => updateHostDropdownPos())
}

function clearRuleHosts(idx: number) {
  const rule = hostRules.value[idx]
  if (!rule) return
  rule.hosts = []
  rule.hostInput = ''
  hostFilterTyped.value = false
  nextTick(() => updateHostDropdownPos())
}

function updateHostDropdownPos() {
  const idx = openHostIdx.value
  if (idx === null) return
  const wrap = hostComboRefs.value[idx]
  const r = wrap?.getBoundingClientRect()
  if (!r) return
  const vw = window.innerWidth
  const vv = window.visualViewport
  const vvTop = vv?.offsetTop ?? 0
  const vh = vv?.height ?? window.innerHeight
  const vvBottom = vvTop + vh
  const inputEl = hostInputRefs.value[idx]
  if (document.activeElement !== inputEl && (r.bottom < vvTop || r.top > vvBottom)) {
    closeHostDropdown()
    return
  }
  const width = Math.max(Math.min(r.width, vw - 16), Math.min(280, vw - 16))
  const left = Math.max(8, Math.min(r.left, vw - width - 8))
  const spaceBelow = vvBottom - r.bottom - 12
  const spaceAbove = r.top - vvTop - 12
  hostDropUp.value = spaceBelow < 220 && spaceAbove > spaceBelow && document.activeElement !== inputEl
  hostPopupStyle.value = {
    position: 'fixed',
    left: `${left}px`,
    width: `${width}px`,
    minWidth: '0',
    top: hostDropUp.value ? 'auto' : `${Math.max(vvTop + 8, r.bottom + 4)}px`,
    bottom: hostDropUp.value ? `${window.innerHeight - r.top + 4}px` : 'auto',
  }
}

async function openHostDropdown(idx: number, typed = false) {
  if (openHostIdx.value !== null && openHostIdx.value !== idx) {
    commitHostInput(openHostIdx.value)
  }
  openHostIdx.value = idx
  hostFilterTyped.value = typed
  const rule = hostRules.value[idx]
  if (rule) {
    const list = filteredHostsFor(rule)
    const cur = rule.hostInput.trim().toLowerCase()
    const found = cur ? list.findIndex((h) => h.toLowerCase() === cur) : 0
    hostActiveIdx.value = Math.max(0, found)
  }
  updateHostDropdownPos()
  await nextTick()
  scrollHostOptIntoView(idx)
}

function closeHostDropdown() {
  if (openHostIdx.value !== null) {
    commitHostInput(openHostIdx.value)
  }
  openHostIdx.value = null
  hostFilterTyped.value = false
}

function focusHostInput(idx: number) {
  hostInputRefs.value[idx]?.focus()
  openHostDropdown(idx, false)
}

function commitHostInput(idx: number): boolean {
  const rule = hostRules.value[idx]
  if (!rule) return true
  const tokens = splitDomainInput(rule.hostInput)
  if (!tokens.length) {
    rule.hostInput = ''
    return true
  }
  for (const h of tokens) {
    if (isHostUsedElsewhere(idx, h)) {
      ui.toast(`域名 ${h} 已在其他规则中配置`, false)
      continue
    }
    if (!rule.hosts.includes(h)) {
      rule.hosts.push(h)
    }
  }
  rule.hostInput = ''
  hostFilterTyped.value = false
  nextTick(() => updateHostDropdownPos())
  return true
}

function toggleHost(idx: number, host: string) {
  const rule = hostRules.value[idx]
  if (!rule) return
  const h = normalizeDomainToken(host)
  if (!h) return
  const pos = rule.hosts.indexOf(h)
  if (pos >= 0) {
    rule.hosts.splice(pos, 1)
  } else {
    if (isHostUsedElsewhere(idx, h)) {
      ui.toast(`域名 ${h} 已在其他规则中配置`, false)
      return
    }
    rule.hosts.push(h)
  }
  if (hostFilterTyped.value) {
    rule.hostInput = ''
    hostFilterTyped.value = false
  }
  nextTick(() => updateHostDropdownPos())
}

function removeRuleHost(idx: number, host: string) {
  const rule = hostRules.value[idx]
  if (!rule) return
  rule.hosts = rule.hosts.filter((h) => h !== host)
  nextTick(() => updateHostDropdownPos())
}

function scrollHostOptIntoView(idx: number) {
  nextTick(() => {
    const listEl = hostListRefs.value[idx]
    listEl?.querySelector<HTMLElement>(`[data-i="${hostActiveIdx.value}"]`)?.scrollIntoView({ block: 'nearest' })
  })
}

function onHostKeydown(idx: number, e: KeyboardEvent) {
  if (e.isComposing || e.keyCode === 229) return
  const rule = hostRules.value[idx]
  if (!rule) return
  const list = filteredHostsFor(rule)
  if (e.key === 'Backspace' && !rule.hostInput && rule.hosts.length) {
    e.preventDefault()
    rule.hosts.pop()
    nextTick(() => updateHostDropdownPos())
    return
  }
  if (e.key === ',' || e.key === '，' || e.key === ';') {
    if (rule.hostInput.trim()) {
      e.preventDefault()
      commitHostInput(idx)
    }
    return
  }
  if (openHostIdx.value !== idx) {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault()
      openHostDropdown(idx, false)
    } else if (e.key === 'Enter' && rule.hostInput.trim()) {
      e.preventDefault()
      commitHostInput(idx)
    }
    return
  }
  if (e.key === 'Escape') {
    e.preventDefault()
    closeHostDropdown()
    return
  }
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    if (list.length) {
      hostActiveIdx.value = Math.min(list.length - 1, hostActiveIdx.value + 1)
      scrollHostOptIntoView(idx)
    }
    return
  }
  if (e.key === 'ArrowUp') {
    e.preventDefault()
    if (list.length) {
      hostActiveIdx.value = Math.max(0, hostActiveIdx.value - 1)
      scrollHostOptIntoView(idx)
    }
    return
  }
  if (e.key === 'Enter') {
    e.preventDefault()
    const raw = rule.hostInput.trim().toLowerCase()
    const picked = list[hostActiveIdx.value]
    if (raw && (!picked || picked.toLowerCase() !== raw)) {
      commitHostInput(idx)
    } else if (picked) {
      toggleHost(idx, picked)
    }
    return
  }
  if (e.key === 'Tab') {
    closeHostDropdown()
  }
}

function onHostPaste(idx: number, e: ClipboardEvent) {
  const rule = hostRules.value[idx]
  if (!rule) return
  const text = e.clipboardData?.getData('text') ?? ''
  if (splitDomainInput(text).length < 2) return
  e.preventDefault()
  rule.hostInput = `${rule.hostInput} ${text}`
  commitHostInput(idx)
}

function onDocPointerDown(e: Event) {
  if (openHostIdx.value === null) return
  const wrap = hostComboRefs.value[openHostIdx.value]
  if (wrap && !wrap.contains(e.target as Node)) {
    closeHostDropdown()
  }
}

function onHostViewportChange(e: Event) {
  if (openHostIdx.value === null) return
  const list = hostListRefs.value[openHostIdx.value]
  if (list?.contains(e.target as Node)) return
  updateHostDropdownPos()
}

onMounted(() => {
  document.addEventListener('scroll', onHostViewportChange, true)
  window.addEventListener('resize', updateHostDropdownPos)
  window.visualViewport?.addEventListener('resize', updateHostDropdownPos)
  window.visualViewport?.addEventListener('scroll', updateHostDropdownPos)
  document.addEventListener('pointerdown', onDocPointerDown)
})
onBeforeUnmount(() => {
  document.removeEventListener('scroll', onHostViewportChange, true)
  window.removeEventListener('resize', updateHostDropdownPos)
  window.visualViewport?.removeEventListener('resize', updateHostDropdownPos)
  window.visualViewport?.removeEventListener('scroll', updateHostDropdownPos)
  document.removeEventListener('pointerdown', onDocPointerDown)
})

const availableTags = computed(() => {
  const set = new Set<string>()
  for (const a of addressSets.value) { if (a.tag) set.add(a.tag) }
  for (const item of tagItems.value) {
    const t = item.tag.trim()
    if (t) set.add(t)
  }
  for (const r of hostRules.value) {
    for (const t of r.allowTags) {
      if (t) set.add(t)
    }
  }
  return Array.from(set)
})

const totalBlockedCount = computed(() => hostRules.value.reduce((sum, rule) => sum + rule.blockedCount, 0))

const enabledHostRulesCount = computed(() => hostRules.value.filter((r) => r.enabled && effectiveRuleHosts(r).length > 0).length)

function toEditableHostRule(r: HostRuleView): EditableHostRule {
  const hosts = r.hosts?.length ? r.hosts.map(normalizeDomainToken).filter(Boolean) : splitDomainInput(r.host || '')
  return {
    hosts: [...new Set(hosts)],
    hostInput: '',
    enabled: r.enabled,
    mode: r.mode || 'domestic',
    allowIpsText: (r.allow_ips ?? []).join('\n'),
    allowTags: [...(r.allow_tags ?? [])],
    allowSets: [...(r.allow_sets ?? [])],
    note: r.note ?? '',
    blockedCount: r.blocked_count ?? 0,
    lastBlockedIp: r.last_blocked_ip ?? '',
    lastBlockedAt: r.last_blocked_at ?? 0,
    hostStats: { ...(r.host_stats ?? {}) },
  }
}

async function loadHostRules() {
  hostRuleLoading.value = true
  hostRuleError.value = ''
  try {
    const res = await api.hostRuleSettings()
    hostRules.value = res.rules.map(toEditableHostRule)
    knownHosts.value = res.hosts ?? []
  } catch (e) {
    hostRuleError.value = errText(e)
  } finally {
    hostRuleLoading.value = false
  }
}

function addHostRule(mode: HostRuleMode = 'domestic') {
  hostRules.value.push({
    hosts: [],
    hostInput: '',
    enabled: true,
    mode,
    allowIpsText: '',
    allowTags: [],
    allowSets: [],
    note: '',
    blockedCount: 0,
    lastBlockedIp: '',
    lastBlockedAt: 0,
    hostStats: {},
  })
}

function removeHostRule(idx: number) {
  if (openHostIdx.value === idx) {
    openHostIdx.value = null
    hostFilterTyped.value = false
  }
  hostRules.value.splice(idx, 1)
}

async function saveHostRules() {
  for (let idx = 0; idx < hostRules.value.length; idx++) {
    commitHostInput(idx)
    const editor = addressEditors.value[idx]
    if (editor && !editor.commit()) { editor.focus(); return }
  }
  closeHostDropdown()
  hostRuleSaving.value = true
  hostRuleError.value = ''
  try {
    const payload = hostRules.value
      .map((r) => {
        const hosts = effectiveRuleHosts(r)
        return {
          host: hosts.join(', '),
          hosts,
          enabled: r.enabled,
          mode: r.mode,
          allow_ips: r.allowIpsText
            .split(/[\s,;，；]+/)
            .map((s) => s.trim())
            .filter(Boolean),
          allow_tags: r.allowTags,
          allow_sets: r.allowSets,
          note: r.note.trim(),
        }
      })
      .filter((r) => r.hosts.length > 0)
    const res = await api.saveHostRules(payload)
    hostRules.value = res.rules.map(toEditableHostRule)
    addressSets.value = addressSets.value.map((a) => ({
      ...a,
      hosts: res.rules.filter((r) => r.allow_sets?.includes(a.id)).flatMap((r) => r.hosts?.length ? r.hosts : [r.host]),
    }))
    ui.toast('域名规则已保存；启用范围约 2 秒内同步到 Nginx')
  } catch (e) {
    hostRuleError.value = errText(e)
  } finally {
    hostRuleSaving.value = false
  }
}

async function loadTags() {
  tagLoading.value = true
  tagError.value = ''
  try {
    const res = await api.ipTagSettings()
    tagItems.value = res.items.map((x) => ({ target: x.target, tag: x.tag, note: x.note ?? '' }))
  } catch (e) {
    tagError.value = errText(e)
  } finally {
    tagLoading.value = false
  }
}

function addTagRow() {
  tagItems.value.push({ target: '', tag: '', note: '' })
}

function removeTagRow(idx: number) {
  tagItems.value.splice(idx, 1)
}

async function saveTags() {
  tagSaving.value = true
  tagError.value = ''
  try {
    const cleaned = tagItems.value
      .map((x) => ({ target: x.target.trim(), tag: x.tag.trim(), note: (x.note ?? '').trim() }))
      .filter((x) => x.target !== '' || x.tag !== '')
    const res = await api.saveIPTags(cleaned)
    tagItems.value = res.items.map((x) => ({ target: x.target, tag: x.tag, note: x.note ?? '' }))
    ui.bump()
    ui.toast('IP 标识已保存')
  } catch (e) {
    tagError.value = errText(e)
  } finally {
    tagSaving.value = false
  }
}

const settings = reactive<NotificationSettings>({
  enabled: false, url: '', headers: '', body: '', detail_url: '', cooldown_min: 30, min_level: 'warn',
  has_headers: false, status: { last_attempt: 0, last_success: 0, error: '' },
})
const loading = ref(true)
const loaded = ref(false)
const savedEnabled = ref(false)
const saving = ref(false)
const testing = ref(false)
const error = ref('')
const feedback = ref('')
const clearHeaders = ref(false)
const editHeaders = ref(false)
const headersVisible = ref(false)
const busy = computed(() => saving.value || testing.value || loading.value || !loaded.value)
const variables = ['title', 'content', 'url', 'ip', 'geo', 'level', 'count', 'err_ratio', 'host', 'paths', 'timestamp']
const sample = computed<Record<string, string>>(() => ({
  title: 'ipguard 发现扫描行为',
  content: 'IP：203.0.113.10（模拟地址）\n异常等级：扫描\n近 24 小时请求：30，4xx/5xx 占比：90.0%\n主要域名：example.com\n路径：/.env、/.git/config',
  url: settings.detail_url, ip: '203.0.113.10', geo: '模拟地址', level: '扫描', count: '30', err_ratio: '90.0%',
  host: 'example.com', paths: '/.env、/.git/config', timestamp: '2026-11-04 16:10:00',
}))
const preview = computed(() => {
  if (!settings.body.trim()) return '不发送消息正文，将向接收地址发送 GET 请求。'
  try {
    const walk = (v: unknown): unknown => {
      if (typeof v === 'string') return v.replace(/#\{(\w+)\}/g, (match, key) => sample.value[key] ?? match)
      if (Array.isArray(v)) return v.map(walk)
      if (v && typeof v === 'object') return Object.fromEntries(Object.entries(v).map(([k, value]) => [k, walk(value)]))
      return v
    }
    return JSON.stringify(walk(JSON.parse(settings.body)), null, 2)
  } catch { return '消息模板不是有效 JSON，请检查引号、逗号和括号。' }
})

async function load() {
  loading.value = true
  error.value = ''
  try { Object.assign(settings, await api.notificationSettings()); savedEnabled.value = settings.enabled; loaded.value = true }
  catch (e) { error.value = errText(e) }
  finally { loading.value = false }
}
const banDurationOptions: SelectOption[] = [
  { value: '1h', label: '1 小时（临时冷却）' },
  { value: '6h', label: '6 小时' },
  { value: '24h', label: '24 小时（推荐：高频拦截）' },
  { value: '7d', label: '7 天（推荐：敏感路径扫描）' },
  { value: '30d', label: '30 天' },
  { value: 'forever', label: '永久封禁' },
]

const scanLevelOptions: SelectOption[] = [
  { value: 'scan', label: '仅扫描异常（命中敏感路径，推荐）' },
  { value: 'warn', label: '可疑及扫描异常（含高错误率突发探测）' },
]

const autoBan = reactive<AutoBanSettings>({
  guard_enabled: false,
  guard_window_min: 5,
  guard_threshold: 20,
  guard_duration: '24h',
  scan_enabled: false,
  scan_min_level: 'scan',
  scan_min_count: 15,
  scan_duration: '7d',
  notify_enabled: true,
  sensitive_paths: [],
  default_sensitive_paths: [],
  sensitive_custom: false,
  status: {
    total_banned: 0,
    guard_banned: 0,
    scan_banned: 0,
    last_ban_at: 0,
    last_ban_ip: '',
    last_ban_reason: '',
    error: '',
  },
})
const autoBanLoading = ref(true)
const autoBanSaving = ref(false)
const autoBanError = ref('')
const autoBanSavedActive = ref(false)
const sensitiveEditor = ref<InstanceType<typeof PathListInput> | null>(null)
const sensitiveIsDefault = computed(() => {
  const a = autoBan.sensitive_paths, d = autoBan.default_sensitive_paths
  return a.length === d.length && a.every((p) => d.includes(p))
})
function resetSensitive() { autoBan.sensitive_paths = [...autoBan.default_sensitive_paths] }

async function loadAutoBan() {
  autoBanLoading.value = true
  autoBanError.value = ''
  try {
    const res = await api.autoBanSettings()
    Object.assign(autoBan, res)
    autoBanSavedActive.value = res.guard_enabled || res.scan_enabled
  } catch (e) {
    autoBanError.value = errText(e)
  } finally {
    autoBanLoading.value = false
  }
}

async function saveAutoBan() {
  if (sensitiveEditor.value && !sensitiveEditor.value.commit()) return
  if (!autoBan.sensitive_paths.length) { autoBanError.value = '敏感路径至少保留 1 条，可点击“恢复默认”'; return }
  autoBanSaving.value = true
  autoBanError.value = ''
  try {
    const res = await api.saveAutoBan({
      guard_enabled: autoBan.guard_enabled,
      guard_window_min: autoBan.guard_window_min,
      guard_threshold: autoBan.guard_threshold,
      guard_duration: autoBan.guard_duration,
      scan_enabled: autoBan.scan_enabled,
      scan_min_level: autoBan.scan_min_level,
      scan_min_count: autoBan.scan_min_count,
      scan_duration: autoBan.scan_duration,
      notify_enabled: autoBan.notify_enabled,
      sensitive_paths: autoBan.sensitive_paths,
    })
    Object.assign(autoBan, res)
    autoBanSavedActive.value = res.guard_enabled || res.scan_enabled
    ui.toast('自动封禁策略已保存')
  } catch (e) {
    autoBanError.value = errText(e)
  } finally {
    autoBanSaving.value = false
  }
}

const geoStatus = ref<GeoIPStatus | null>(null)
const geoLoading = ref(true)
const geoBusy = ref<'download' | 'reload' | ''>('')
const geoError = ref('')

async function loadGeoIP() {
  geoLoading.value = true
  geoError.value = ''
  try {
    geoStatus.value = await api.geoipSettings()
  } catch (e) {
    geoError.value = errText(e)
  } finally {
    geoLoading.value = false
  }
}

async function runGeoIPUpdate(mode: 'download' | 'reload') {
  geoBusy.value = mode
  geoError.value = ''
  try {
    geoStatus.value = await api.updateGeoip(mode)
    ui.bump()
    ui.toast(mode === 'download' ? 'GeoIP 数据库已在线更新并热重载' : '本地 GeoIP 数据库已重新加载')
  } catch (e) {
    geoError.value = errText(e)
  } finally {
    geoBusy.value = ''
  }
}

onMounted(() => {
  loadAutoBan()
  loadGeoIP()
  loadAddressSets()
  loadHostRules()
  loadTags()
  load()
})
function payload() {
  return { enabled: settings.enabled, url: settings.url.trim(), headers: editHeaders.value ? settings.headers : '',
    body: settings.body.trim(), detail_url: settings.detail_url.trim(), cooldown_min: settings.cooldown_min,
    min_level: settings.min_level, clear_headers: clearHeaders.value }
}
async function save() {
  saving.value = true; error.value = ''; feedback.value = ''
  try {
    Object.assign(settings, await api.saveNotifications(payload()))
    savedEnabled.value = settings.enabled
    editHeaders.value = false; clearHeaders.value = false; headersVisible.value = false
    feedback.value = '通知设置已保存'; ui.toast(feedback.value)
  } catch (e) { error.value = errText(e) }
  finally { saving.value = false }
}
async function test() {
  testing.value = true; error.value = ''; feedback.value = ''
  try { await api.testNotification(payload()); feedback.value = '测试通知已发送，请查看接收端。测试不会保存配置。' }
  catch (e) { error.value = errText(e) }
  finally { testing.value = false }
}
</script>

<template>
  <div class="notification-settings">
    <div class="settings-tabs" role="tablist" aria-label="设置分类">
      <button v-for="(tab, index) in settingsTabs" :id="`settings-tab-${tab.value}`" :key="tab.value" type="button" role="tab" :aria-selected="activeTab === tab.value" :aria-controls="`settings-panel-${tab.value}`" :tabindex="activeTab === tab.value ? 0 : -1" @click="selectSettingsTab(tab.value)" @keydown="onSettingsTabKeydown($event, index)"><component :is="tab.icon" :size="16" />{{ tab.label }}</button>
    </div>
    <section v-show="activeTab === 'domains'" id="settings-panel-domains" role="tabpanel" aria-labelledby="settings-tab-domains">
    <!-- ── 1. 域名访问控制 ─────────────────────────────────────────────────── -->
    <div class="settings-intro">
      <div>

        <p>按域名设置国内访问或指定来源白名单，支持一条规则同时勾选多个域名；不符合规则的请求返回 403，内网始终放行。</p>
      </div>
      <span class="tag" :class="enabledHostRulesCount ? 'ok' : 'muted'">
        {{ hostRuleLoading ? '正在加载' : enabledHostRulesCount ? `已启用 ${enabledHostRulesCount} 条` : '暂未启用' }}
      </span>
    </div>
    <p v-if="hostRuleError" class="banner bad" role="alert">{{ hostRuleError }}</p>
    <form class="card settings-section settings-table-card" @submit.prevent="saveHostRules">
      <header class="card-h">
        <h2>域名规则列表</h2>
        <span class="meta">每条规则可多选域名或输入泛域名（如 <code>*.example.com</code>）；内网地址始终放行</span>
      </header>
      <div class="settings-table-toolbar">
        <div class="settings-foot-btns">
          <button type="button" class="btn" :disabled="hostRuleSaving || hostRuleLoading" @click="addHostRule('domestic')">
            <Plus :size="15" />国内访问规则
          </button>
          <button type="button" class="btn" :disabled="hostRuleSaving || hostRuleLoading" @click="addHostRule('allowlist')">
            <Plus :size="15" />指定来源规则
          </button>
        </div>
        <div class="settings-save-group"><span class="settings-save-scope" id="domain-save-scope">统一保存所有新增、修改和删除</span>
          <button type="submit" aria-describedby="domain-save-scope" class="btn primary" :disabled="hostRuleSaving || hostRuleLoading">
            {{ hostRuleSaving ? '保存中…' : '保存全部规则' }}
          </button>
        </div>
      </div>
      <div class="settings-table-scroll" role="region" aria-label="域名规则表格，可横向滚动" tabindex="0">
        <table class="settings-data host-rules-table">
          <colgroup><col style="width:88px"><col style="width:248px"><col style="width:236px"><col style="width:120px"><col style="width:160px"><col style="width:260px"><col style="width:136px"><col style="width:116px"><col style="width:88px"></colgroup>
          <thead><tr><th scope="col">状态</th><th scope="col" title="支持从下拉列表多选域名，或直接输入完整域名/泛域名后按回车添加">域名（支持多选）</th><th scope="col">访问策略</th><th scope="col" title="可选择多个标识，匹配任一标识即可放行">按标识放行</th><th scope="col">引用地址集</th><th scope="col" title="输入 IP 或 CIDR，按回车添加；可连续添加多个地址">直接放行 IP / 网段</th><th scope="col">备注</th><th scope="col" class="stat-column">累计拦截</th><th scope="col" class="action-column">操作</th></tr></thead>
          <tbody>
            <tr v-if="hostRuleLoading"><td colspan="9" class="settings-empty" role="status">正在加载域名规则…</td></tr>
            <tr v-else-if="!hostRules.length"><td colspan="9" class="settings-empty">暂无域名规则，点击上方按钮添加。</td></tr>
            <tr v-for="(rule, idx) in hostRuleLoading ? [] : hostRules" :key="idx" class="rule-editor" :class="{ 'rule-disabled': !rule.enabled }">
              <td>
                <label class="rule-state-toggle">
                  <input v-model="rule.enabled" type="checkbox" role="switch" :aria-label="`${ruleDisplayLabel(rule) || '此域名'}的访问控制`">
                  <span class="switch-track" aria-hidden="true"></span><span>{{ rule.enabled ? '启用' : '停用' }}</span>
                </label>
              </td>
              <td class="rule-domain">
                <div
                  :ref="(el) => setHostComboRef(idx, el)"
                  class="host-combo"
                  :class="{ open: openHostIdx === idx }"
                >
                  <div class="host-combo-box" @click="focusHostInput(idx)">
                    <span v-for="h in rule.hosts" :key="h" class="host-token">
                      <span class="host-token-val mono" :title="h">{{ h }}</span>
                      <RouterLink
                        v-if="rule.hosts.length > 1 && rule.hostStats[h]?.blocked_count"
                        :to="{ name: 'logs', query: { tab: 'guard', rule: h, range: '7d' } }"
                        class="host-token-stat mono"
                        :title="`${h} 累计拦截 ${rule.hostStats[h]!.blocked_count} 次，点击仅看该域名拦截记录`"
                        @mousedown.stop
                        @click.stop
                      >
                        {{ rule.hostStats[h]!.blocked_count }}
                      </RouterLink>
                      <button
                        type="button"
                        :aria-label="`移除域名 ${h}`"
                        @mousedown.prevent
                        @click.stop="removeRuleHost(idx, h)"
                      >
                        <X :size="12" />
                      </button>
                    </span>
                    <input
                      :id="`rule-host-${idx}`"
                      :ref="(el) => setHostInputRef(idx, el)"
                      v-model="rule.hostInput"
                      class="host-combo-input mono"
                      :class="{ compact: rule.hosts.length > 0 && openHostIdx !== idx && !rule.hostInput }"
                      :placeholder="rule.hosts.length ? (openHostIdx === idx ? '搜索或输入…' : '+') : '选择或输入域名（可多选）'"
                      aria-label="域名"
                      autocomplete="off"
                      spellcheck="false"
                      @focus="openHostDropdown(idx, false)"
                      @click.stop="openHostDropdown(idx, false)"
                      @input="openHostDropdown(idx, true)"
                      @keydown="onHostKeydown(idx, $event)"
                      @paste="onHostPaste(idx, $event)"
                    >
                    <button
                      type="button"
                      class="host-combo-btn"
                      tabindex="-1"
                      aria-label="展开域名列表"
                      @mousedown.prevent
                      @click.stop="openHostIdx === idx ? closeHostDropdown() : openHostDropdown(idx, false)"
                    >
                      <ChevronDown class="host-combo-caret" :size="14" />
                    </button>
                  </div>
                  <div v-if="openHostIdx === idx && (knownHosts.length || customHostCandidate(rule))" class="sel-pop" :class="{ up: hostDropUp }" :style="hostPopupStyle">
                    <div class="sel-head">
                      <span>已选 {{ rule.hosts.length }} 个域名 · 可连续勾选</span>
                      <span class="sel-head-actions">
                        <button
                          v-if="canSelectAllFor(idx)"
                          type="button"
                          class="sel-act-btn"
                          @mousedown.prevent
                          @click="selectAllAvailableHosts(idx)"
                        >
                          全选可用
                        </button>
                        <button
                          v-if="rule.hosts.length"
                          type="button"
                          class="sel-act-btn"
                          @mousedown.prevent
                          @click="clearRuleHosts(idx)"
                        >
                          清空
                        </button>
                      </span>
                    </div>
                    <ul :ref="(el) => setHostListRef(idx, el)" class="sel-list" role="listbox" aria-multiselectable="true">
                      <li
                        v-if="customHostCandidate(rule)"
                        class="sel-opt sel-opt-custom mono"
                        @mousedown.prevent
                        @click="commitHostInput(idx)"
                      >
                        <span class="sel-label">+ 添加自定义域名：<strong>{{ customHostCandidate(rule) }}</strong></span>
                        <span class="sel-used-badge">回车添加</span>
                      </li>
                      <li
                        v-for="(h, hi) in filteredHostsFor(rule)"
                        :key="h"
                        :data-i="hi"
                        role="option"
                        :aria-selected="rule.hosts.includes(h)"
                        class="sel-opt mono"
                        :class="{
                          active: hi === hostActiveIdx,
                          selected: rule.hosts.includes(h),
                          disabled: isHostUsedElsewhere(idx, h),
                        }"
                        @mouseenter="hostActiveIdx = hi"
                        @mousedown.prevent
                        @click="toggleHost(idx, h)"
                      >
                        <span class="sel-opt-main">
                          <input
                            type="checkbox"
                            :checked="rule.hosts.includes(h)"
                            :disabled="isHostUsedElsewhere(idx, h)"
                            tabindex="-1"
                            aria-hidden="true"
                          >
                          <span class="sel-label" :title="h">{{ h }}</span>
                        </span>
                        <span v-if="isHostUsedElsewhere(idx, h)" class="sel-used-badge">已在其他规则</span>
                        <span v-else-if="rule.hostStats[h]?.blocked_count" class="sel-stat-badge">拦截 {{ rule.hostStats[h]!.blocked_count }} 次</span>
                        <Check v-if="rule.hosts.includes(h) && !isHostUsedElsewhere(idx, h)" :size="14" />
                      </li>
                      <li v-if="!filteredHostsFor(rule).length && !customHostCandidate(rule)" class="sel-empty">
                        无匹配域名，可输入完整域名或泛域名后按回车添加
                      </li>
                    </ul>
                  </div>
                </div>
              </td>
              <td class="rule-policy"><SelectBox :model-value="rule.mode" :options="modeOptions" aria-label="访问策略" block fixed-popup @update:model-value="rule.mode = $event as HostRuleMode" /></td>
              <td><MultiSelectBox v-if="availableTags.length" v-model="rule.allowTags" :options="availableTags.map((tag) => ({ value: tag, label: tag }))" ariaLabel="按标识放行" placeholder="选择标识" /><span v-else class="table-hint">暂无标识</span></td>
              <td><MultiSelectBox v-if="addressSets.length" v-model="rule.allowSets" :options="addressSets.map((a) => ({ value: a.id, label: a.name, description: `${a.ips.length} 个 IP / 网段` }))" ariaLabel="引用地址集" placeholder="选择地址集" /><button v-else type="button" class="btn sm" @click="selectSettingsTab('address-sets')">{{ setsLoading ? '加载中…' : '创建地址集' }}</button></td>
              <td><IpListInput :id="`rule-ips-${idx}`" :ref="(el) => setAddressEditor(idx, el)" v-model="rule.allowIpsText" /><p :id="`rule-ips-${idx}-help`" hidden>{{ rule.mode === 'domestic' ? '额外白名单；国内与内网默认放行' : '地址或选中标识，满足其一即可访问' }}</p></td>
              <td><input :id="`rule-note-${idx}`" v-model="rule.note" class="field" aria-label="域名规则备注" maxlength="100" placeholder="备注（选填）"></td>
              <td class="stat-column">
                <RouterLink class="rule-stat" :to="{ name: 'logs', query: { tab: 'guard', rule: effectiveRuleHosts(rule).join(','), range: '7d' } }" :title="ruleStatTooltip(rule)">
                  <span><strong :class="{ 'has-blocks': rule.blockedCount > 0 }">{{ rule.blockedCount.toLocaleString() }}</strong> 次</span><span class="rule-stat-link">{{ rule.hosts.length > 1 ? '查看各域名' : '查看记录' }}</span>
                </RouterLink>
              </td>
              <td class="action-column"><button type="button" class="btn sm ghost-danger" :aria-label="`删除域名规则 ${ruleDisplayLabel(rule) || idx + 1}`" @click="removeHostRule(idx)"><Trash2 :size="14" />删除</button></td>
            </tr>
          </tbody>
          <tfoot v-if="!hostRuleLoading && hostRules.length"><tr><td colspan="7"><span>共 {{ hostRules.length }} 条规则，已启用 {{ enabledHostRulesCount }} 条</span><span class="table-summary-note">累计次数跨部署保留；“查看记录”为近 7 天采样明细</span></td><td class="stat-column"><span class="table-summary-count">合计 <strong>{{ totalBlockedCount.toLocaleString() }}</strong> 次</span></td><td></td></tr></tfoot>
        </table>
      </div>
      <footer class="settings-table-foot">
        <span class="settings-help">
          启停域名规则约 2 秒内同步，无需重启 Nginx。每条规则支持勾选多个域名共用同一放行策略；直接 IP、地址集与标识满足其一即可放行。
        </span>
      </footer>
    </form>

    </section>
    <section v-show="activeTab === 'autoban'" id="settings-panel-autoban" role="tabpanel" aria-labelledby="settings-tab-autoban">
      <div class="settings-intro">
        <div>
          <p>联动 CrowdSec 自动处置两类高危信号：域名访问控制高频 403 拦截与敏感路径扫描，把应用层拒绝升级为防火墙直接丢包；内网、IP 标识与放行地址集始终豁免。</p>
        </div>
        <span class="tag" :class="autoBanSavedActive ? 'ok' : 'muted'">
          {{ autoBanLoading ? '正在加载' : autoBanSavedActive ? '自动封禁已开启' : '自动封禁未开启' }}
        </span>
      </div>
      <p v-if="autoBanError || autoBan.status.error" class="banner bad" role="alert">{{ autoBanError || autoBan.status.error }}</p>

      <form class="card form-card" @submit.prevent="saveAutoBan">
        <!-- 运行状态概览 -->
        <div class="autoban-overview">
          <div class="autoban-stat">
            <span class="autoban-stat-label">累计自动封禁</span>
            <strong class="autoban-stat-val mono">{{ fmtNum(autoBan.status.total_banned) }}</strong>
          </div>
          <div class="autoban-stat">
            <span class="autoban-stat-label">403 高频升级</span>
            <strong class="autoban-stat-val mono">{{ fmtNum(autoBan.status.guard_banned) }}</strong>
          </div>
          <div class="autoban-stat">
            <span class="autoban-stat-label">扫描异常触发</span>
            <strong class="autoban-stat-val mono">{{ fmtNum(autoBan.status.scan_banned) }}</strong>
          </div>
          <div class="autoban-stat autoban-stat-last">
            <span class="autoban-stat-label">最近自动封禁</span>
            <template v-if="autoBan.status.last_ban_ip">
              <strong class="autoban-last-ip mono" :title="autoBan.status.last_ban_ip">{{ autoBan.status.last_ban_ip }}</strong>
              <span class="autoban-last-meta" :title="autoBan.status.last_ban_reason">{{ fmtFull(autoBan.status.last_ban_at) }} · {{ autoBan.status.last_ban_reason || '—' }}</span>
            </template>
            <template v-else>
              <strong class="autoban-last-ip muted-text">尚无触发记录</strong>
              <span class="autoban-last-meta">策略开启后，满足阈值的陌生 IP 会在此显示</span>
            </template>
          </div>
          <div class="autoban-overview-actions">
            <button class="btn sm" type="button" :disabled="autoBanLoading || autoBanSaving" @click="loadAutoBan"><RefreshCw :size="13" />刷新</button>
            <RouterLink class="btn sm" :to="{ name: 'bans', query: { source: 'auto' } }">查看封禁记录</RouterLink>
          </div>
        </div>

        <fieldset class="form-fieldset" :disabled="autoBanLoading || autoBanSaving">
          <div class="form-cols">
          <div class="form-group">
            <div class="form-group-h"><h2>域名高频 403 拦截升级封禁</h2><span>实时滑动窗口：陌生 IP 在时间窗内触发域名访问控制（403）达到阈值时自动封禁</span></div>
            <div class="form-row">
              <span class="form-label">启用</span>
              <div class="form-ctrl">
                <label class="form-switch"><input v-model="autoBan.guard_enabled" type="checkbox" role="switch" aria-label="启用高频拦截自动升级封禁"><span class="switch-track" aria-hidden="true"></span><span>{{ autoBan.guard_enabled ? '已开启' : '已关闭' }}</span></label>
              </div>
            </div>
            <div class="form-row">
              <label class="form-label" for="autoban-guard-window">触发条件</label>
              <div class="form-ctrl">
                <div class="settings-inline">
                  <input id="autoban-guard-window" v-model.number="autoBan.guard_window_min" class="field autoban-num" type="number" min="1" max="60" required>
                  <span>分钟内拦截达到</span>
                  <input id="autoban-guard-threshold" v-model.number="autoBan.guard_threshold" class="field autoban-num" type="number" min="3" max="1000" required aria-label="触发封禁拦截次数">
                  <span>次</span>
                </div>
                <p class="settings-help">建议 <code>5</code> 分钟 <code>20</code> 次；偶发误触不会触发封禁。</p>
              </div>
            </div>
            <div class="form-row">
              <span class="form-label">封禁时长</span>
              <div class="form-ctrl">
                <div class="form-narrow"><SelectBox :model-value="autoBan.guard_duration" :options="banDurationOptions" aria-label="域名拦截自动封禁时长" block @update:model-value="autoBan.guard_duration = $event as AutoBanDuration" /></div>
                <p class="settings-help">决策来源记为 <code>ipguard-auto</code>，经 bouncer 同步到 ROS 防火墙。</p>
              </div>
            </div>
          </div>

          <div class="form-group">
            <div class="form-group-h"><h2>敏感路径扫描自动封禁</h2><span>每分钟后台巡检：高频探测下方「敏感路径特征」的 IP</span></div>
            <div class="form-row">
              <span class="form-label">启用</span>
              <div class="form-ctrl">
                <label class="form-switch"><input v-model="autoBan.scan_enabled" type="checkbox" role="switch" aria-label="启用扫描异常自动封禁"><span class="switch-track" aria-hidden="true"></span><span>{{ autoBan.scan_enabled ? '已开启' : '已关闭' }}</span></label>
              </div>
            </div>
            <div class="form-row">
              <span class="form-label">触发等级</span>
              <div class="form-ctrl">
                <div class="form-narrow"><SelectBox :model-value="autoBan.scan_min_level" :options="scanLevelOptions" aria-label="扫描封禁触发等级" block @update:model-value="autoBan.scan_min_level = $event as 'scan' | 'warn'" /></div>
              </div>
            </div>
            <div class="form-row">
              <label class="form-label" for="autoban-scan-count">最少请求</label>
              <div class="form-ctrl">
                <div class="settings-inline">
                  <span>近 1 小时至少</span>
                  <input id="autoban-scan-count" v-model.number="autoBan.scan_min_count" class="field autoban-num" type="number" min="3" max="1000" required>
                  <span>次请求</span>
                </div>
              </div>
            </div>
            <div class="form-row">
              <span class="form-label">封禁时长</span>
              <div class="form-ctrl">
                <div class="form-narrow"><SelectBox :model-value="autoBan.scan_duration" :options="banDurationOptions" aria-label="扫描自动封禁时长" block @update:model-value="autoBan.scan_duration = $event as AutoBanDuration" /></div>
              </div>
            </div>
          </div>
          </div>

          <div class="form-group">
            <div class="form-group-h">
              <h2>敏感路径特征</h2>
              <span>请求路径包含任一片段（不区分大小写）且返回 4xx/5xx 时计为扫描特征；同时决定 IP 统计里的“可疑 / 扫描”等级</span>
            </div>
            <div class="form-row">
              <label class="form-label" for="autoban-sensitive">路径片段</label>
              <div class="form-ctrl form-ctrl-wide">
                <PathListInput id="autoban-sensitive" ref="sensitiveEditor" v-model="autoBan.sensitive_paths" :defaults="autoBan.default_sensitive_paths" />
                <div class="sensitive-foot">
                  <p class="settings-help">共 {{ autoBan.sensitive_paths.length }} 条{{ sensitiveIsDefault ? '，与内置默认一致' : '，已自定义（高亮为新增项）' }}。支持粘贴多行批量添加；保存后立即生效，无需重启。</p>
                  <button class="btn sm" type="button" :disabled="sensitiveIsDefault" @click="resetSensitive">恢复默认</button>
                </div>
              </div>
            </div>
          </div>

          <div class="form-group">
            <div class="form-group-h"><h2>推送联动与防误封保护</h2><span>以下地址永不自动封禁</span></div>
            <div class="form-row">
              <span class="form-label">封禁后推送</span>
              <div class="form-ctrl">
                <label class="form-switch"><input v-model="autoBan.notify_enabled" type="checkbox" role="switch" aria-label="自动封禁生效时发送推送通知"><span class="switch-track" aria-hidden="true"></span><span>{{ autoBan.notify_enabled ? '已开启' : '已关闭' }}</span></label>
                <p class="settings-help">复用「推送通知」已配置的接收地址，封禁成功后推送处置结果。</p>
              </div>
            </div>
            <div class="form-row">
              <span class="form-label">豁免规则</span>
              <div class="form-ctrl form-ctrl-wide">
                <ul class="autoban-safe-grid">
                  <li><strong>内网与保留地址</strong><span>局域网、回环及保留网段始终跳过。</span></li>
                  <li><strong>IP 标识</strong><span>命中任一标识的地址 / 网段跳过（当前 {{ tagItems.length }} 条）。</span></li>
                  <li><strong>放行地址集</strong><span>存在于任一地址集的 IP / 网段跳过（当前 {{ addressSets.length }} 个）。</span></li>
                  <li><strong>不回溯 · 限速</strong><span>开启前的旧日志不补封；每分钟最多自动封禁 10 个 IP。</span></li>
                </ul>
              </div>
            </div>
          </div>

          <div class="form-foot">
            <div class="form-foot-btns">
              <button class="btn primary" type="submit" :disabled="autoBanLoading || autoBanSaving">{{ autoBanSaving ? '保存中…' : '保存自动封禁策略' }}</button>
            </div>
            <span class="settings-help">自动封禁记录可在「封禁管理 → 自动封禁」与操作审计（操作人 auto-policy）中核查并手动解封。</span>
          </div>
        </fieldset>
      </form>
    </section>
    <section v-show="activeTab === 'address-sets'" id="settings-panel-address-sets" role="tabpanel" aria-labelledby="settings-tab-address-sets">
      <div class="settings-intro"><p>将常用 IP / 网段整理为地址集，在域名规则中勾选引用。修改集合，引用它的域名一起生效。</p></div>
      <p v-if="setsError" class="banner bad" role="alert">{{ setsError }}</p>
      <form class="card settings-section settings-table-card" @submit.prevent="saveAddressSets">
        <header class="card-h"><h2>放行地址集</h2><span class="meta">可选统一标识，在日志和统计中显示</span></header>
        <div class="settings-table-toolbar"><button type="button" class="btn" :disabled="setsLoading || setsSaving" @click="addAddressSet"><Plus :size="16" />新增地址集</button><div class="settings-save-group"><span class="settings-save-scope">统一保存本页全部地址集</span><button type="submit" class="btn primary" :disabled="setsLoading || setsSaving">{{ setsSaving ? '保存中…' : '保存全部地址集' }}</button></div></div>
        <div class="settings-table-scroll" role="region" aria-label="放行地址集表格，可横向滚动" tabindex="0">
          <table class="settings-data address-sets-table"><colgroup><col style="width:160px"><col style="width:320px"><col style="width:150px"><col style="width:180px"><col style="width:240px"><col style="width:88px"></colgroup><thead><tr><th scope="col">集合名称</th><th scope="col">IP / 网段</th><th scope="col">IP 标识</th><th scope="col">引用域名</th><th scope="col">备注</th><th scope="col" class="action-column">操作</th></tr></thead><tbody>
            <tr v-if="setsLoading"><td colspan="6" class="settings-empty">正在加载地址集…</td></tr>
            <tr v-else-if="!setItems.length"><td colspan="6" class="settings-empty">暂无地址集，点击上方“新增地址集”，保存后即可在域名规则中引用。</td></tr>
            <tr v-for="(a, idx) in setsLoading ? [] : setItems" :key="a.key">
              <td><input v-model="a.name" class="field" aria-label="集合名称" placeholder="如：可信服务器" maxlength="40" required></td>
              <td><IpListInput :id="`address-set-${a.key}`" :ref="(el) => { setEditors[a.key] = el as InstanceType<typeof IpListInput> | null }" v-model="a.ipsText" /></td>
              <td><input v-model="a.tag" class="field" aria-label="地址集 IP 标识" maxlength="24" placeholder="标识（选填）"></td>
              <td><div v-if="referencingHosts(a.id).length" class="set-hosts"><span v-for="host in referencingHosts(a.id)" :key="host" class="set-host-token" :title="host">{{ host }}</span></div><span v-else class="table-hint">尚未引用</span></td>
              <td><input v-model="a.note" class="field" aria-label="地址集备注" maxlength="100" placeholder="用途说明（选填）"></td>
              <td class="action-column"><button type="button" class="btn sm ghost-danger" :disabled="!!a.id && referencingHosts(a.id).length > 0" :title="referencingHosts(a.id).length ? '请先移除域名引用并保存规则，再删除地址集' : '删除地址集'" @click="setItems.splice(idx, 1)"><Trash2 :size="14" />删除</button></td>
            </tr>
          </tbody></table>
        </div>
        <footer class="settings-table-foot"><span class="settings-help">每个集合可包含多个 IP 或 CIDR；回车添加，支持批量粘贴。标识应用于集合内所有地址，单独配置的 IP 标识优先。已被域名引用的集合需先解除引用才能删除。</span></footer>
      </form>
    </section>
    <section v-show="activeTab === 'ip-tags'" id="settings-panel-ip-tags" role="tabpanel" aria-labelledby="settings-tab-ip-tags">
    <!-- ── 2. IP 标识 ──────────────────────────────────────────────────────── -->
    <div class="settings-intro">
      <div>

        <p>为常用地址（如公司、家里、服务器）打上标识，在日志与统计中醒目展示，并支持按标识单独筛选或排除。</p>
      </div>
      <span class="tag" :class="tagItems.length ? 'ip-tag' : 'muted'">
        {{ tagLoading ? '正在加载' : `已配置 ${tagItems.length} 条` }}
      </span>
    </div>
    <p v-if="tagError" class="banner bad" role="alert">{{ tagError }}</p>
    <form class="card settings-section settings-table-card" @submit.prevent="saveTags">
      <header class="card-h">
        <h2>常用 IP / 网段列表</h2>
        <span class="meta">支持单 IP（如 <code>198.51.100.189</code>）或 CIDR 网段（如 <code>198.51.100.0/24</code>）</span>
      </header>
      <div class="settings-table-scroll" role="region" aria-label="IP 标识表格，可横向滚动" tabindex="0">
        <table class="settings-data ip-tags-table">
          <colgroup><col style="width:32%"><col style="width:22%"><col style="width:140px"><col><col style="width:88px"></colgroup>
          <thead><tr><th scope="col">IP / 网段</th><th scope="col">标识名称</th><th scope="col" class="stat-column">标识预览</th><th scope="col">备注</th><th scope="col" class="action-column">操作</th></tr></thead>
          <tbody>
            <tr v-if="tagLoading"><td colspan="5" class="settings-empty" role="status">正在加载 IP 标识…</td></tr>
            <tr v-else-if="!tagItems.length"><td colspan="5" class="settings-empty">暂无 IP 标识，点击下方「添加条目」配置常用地址。</td></tr>
            <tr v-for="(item, idx) in tagLoading ? [] : tagItems" :key="idx" class="ip-tag-row">
              <td><input :id="`tag-target-${idx}`" v-model="item.target" class="field mono" placeholder="如 198.51.100.189 或 198.51.100.0/24" aria-label="IP 或网段"></td>
              <td><input :id="`tag-name-${idx}`" v-model="item.tag" class="field" maxlength="24" placeholder="如 公司、家里" aria-label="标识名称"></td>
              <td class="stat-column"><span v-if="item.tag.trim()" class="tag-preview" :title="item.tag.trim()"><Tag :size="13" /><span>{{ item.tag.trim() }}</span></span><span v-else class="table-hint">—</span></td>
              <td><input :id="`tag-note-${idx}`" v-model="item.note" class="field" maxlength="100" placeholder="用途说明（选填）" aria-label="备注"></td>
              <td class="action-column"><button type="button" class="btn sm ghost-danger" :aria-label="`删除 IP 标识 ${item.tag || idx + 1}`" @click="removeTagRow(idx)"><Trash2 :size="14" />删除</button></td>
            </tr>
          </tbody>
        </table>
      </div>
      <footer class="settings-table-foot">
        <div class="settings-foot-btns">
          <button type="button" class="btn" :disabled="tagSaving || tagLoading" @click="addTagRow">
            <Plus :size="15" />添加条目
          </button>
        </div>
          <button type="submit" class="btn primary" :disabled="tagSaving || tagLoading">
            {{ tagSaving ? '保存中…' : '保存 IP 标识' }}
          </button>
        <span class="settings-help">
          在日志与 IP 统计中按标识筛选；搜索 <code>公司</code> 仅看该标识，<code>!公司</code> 排除该标识。
        </span>
      </footer>
    </form>

    </section>
    <section v-show="activeTab === 'notifications'" id="settings-panel-notifications" role="tabpanel" aria-labelledby="settings-tab-notifications">
    <!-- ── 3. 推送通知 ─────────────────────────────────────────────────────── -->
    <div class="settings-intro">
      <div><p>发现可疑访问时主动提醒，不必一直打开面板。</p></div>
      <span class="tag" :class="savedEnabled ? 'ok' : 'muted'">{{ loading ? '正在加载' : savedEnabled ? '自动推送已开启' : '自动推送未开启' }}</span>
    </div>
    <p v-if="error" class="banner bad" role="alert">{{ error }}</p>
    <button v-if="!loaded && !loading" class="btn" type="button" @click="load">重新加载设置</button>
    <p v-if="feedback" class="banner" role="status">{{ feedback }}</p>
    <form class="card form-card" @submit.prevent="save">
      <fieldset class="form-fieldset" :disabled="busy">
        <div class="form-split">
          <div class="form-main">
        <div class="form-group">
          <div class="form-group-h"><h2>提醒规则</h2><span>每分钟检查一次，与概览中的“最近异常”使用相同判定规则</span></div>
          <div class="form-row">
            <span class="form-label">启用异常推送</span>
            <div class="form-ctrl">
              <label class="form-switch"><input v-model="settings.enabled" type="checkbox" role="switch"><span class="switch-track" aria-hidden="true"></span><span>{{ settings.enabled ? '已开启' : '已关闭' }}</span></label>
            </div>
          </div>
          <div class="form-row">
            <span class="form-label">提醒范围</span>
            <div class="form-ctrl">
              <div class="form-narrow"><SelectBox :model-value="settings.min_level" :options="notifyLevelOptions" aria-label="提醒范围" block @update:model-value="settings.min_level = $event as 'warn' | 'scan'" /></div>
              <p class="settings-help">仅推送判定为可疑或扫描的 IP；内网与已封禁地址不提醒。</p>
            </div>
          </div>
          <div class="form-row">
            <label class="form-label" for="notify-cooldown">重复提醒间隔</label>
            <div class="form-ctrl">
              <div class="settings-inline"><input id="notify-cooldown" v-model.number="settings.cooldown_min" class="field autoban-num" type="number" min="1" max="1440" required><span>分钟</span></div>
              <p class="settings-help">同一 IP 有新访问且间隔已到才再次提醒；从可疑升级为扫描时立即提醒。启动或启用前的历史访问不补发。</p>
            </div>
          </div>
        </div>

        <div class="form-group">
          <div class="form-group-h"><h2>接收与消息</h2><span>支持任意接收 JSON 的 Webhook 通知接口</span></div>
          <div class="form-row">
            <label class="form-label" for="notify-url">接收地址</label>
            <div class="form-ctrl"><input id="notify-url" v-model="settings.url" class="field" type="url" required placeholder="https://push.example.com/api/v1/message"></div>
          </div>
          <div class="form-row">
            <label class="form-label" for="notify-detail">详情链接</label>
            <div class="form-ctrl"><input id="notify-detail" v-model="settings.detail_url" class="field" type="url" placeholder="https://ipguard.example.com/#/ips"><p class="settings-help">点击消息后打开的面板地址，请填写手机可访问的地址。</p></div>
          </div>
          <div class="form-row">
            <label class="form-label" for="notify-headers">请求头</label>
            <div class="form-ctrl">
              <p v-if="settings.has_headers && !editHeaders && !clearHeaders" class="settings-help form-help-top">已保存请求头，凭据不会回显。保存其他设置会保留原值。</p>
              <div class="settings-inline settings-header-actions"><label><input v-model="editHeaders" type="checkbox" :disabled="clearHeaders">{{ settings.has_headers ? '替换请求头' : '填写请求头' }}</label><label v-if="settings.has_headers"><input v-model="clearHeaders" type="checkbox" :disabled="editHeaders">清除已保存的请求头</label></div>
              <template v-if="editHeaders"><textarea id="notify-headers" v-model="settings.headers" class="field settings-code settings-secret" :class="{ 'secret-hidden': !headersVisible }" rows="3" spellcheck="false" autocomplete="off" placeholder="Authorization: Bearer 你的 Token"></textarea><label class="settings-help"><input v-model="headersVisible" type="checkbox">显示请求头</label></template>
              <p class="settings-help">每行一个 Header: value。Content-Type 默认 application/json；无需填写 Host、Connection。</p>
            </div>
          </div>
          <div class="form-row">
            <label class="form-label" for="notify-body">消息模板</label>
            <div class="form-ctrl">
              <textarea id="notify-body" v-model="settings.body" class="field settings-code" rows="10" spellcheck="false"></textarea>
              <p class="settings-help">有效 JSON 使用 POST；留空使用 GET。变量放在字符串内，会自动处理引号和换行。</p>
              <div class="settings-variables"><code v-for="v in variables" :key="v">{{ '#{' + v + '}' }}</code></div>
            </div>
          </div>
        </div>
          </div>
          <aside class="form-aside">
            <div class="template-preview-h"><h2>消息预览</h2><span>模拟扫描行为，仅用于预览</span></div>
            <div class="notification-sample"><span class="tag danger">扫描</span><h4>{{ sample.title }}</h4><p>{{ sample.content }}</p><span class="settings-help">查看访问明细</span></div>
            <details open><summary>查看请求正文（JSON）</summary><pre>{{ preview }}</pre></details>
            <div class="aside-status-box">
              <div class="template-preview-h"><h2>自动推送状态</h2><button class="btn sm ghost" type="button" :disabled="busy" @click="load"><RefreshCw :size="13" />刷新</button></div>
              <div class="aside-status-grid">
                <div><span>上次尝试</span><strong class="mono">{{ fmtFull(settings.status.last_attempt) }}</strong></div>
                <div><span>上次成功</span><strong class="mono">{{ fmtFull(settings.status.last_success) }}</strong></div>
              </div>
              <p v-if="settings.status.error" class="fail-text" role="alert">{{ settings.status.error }}</p>
              <p v-else class="settings-help">保存设置后，新的异常访问会在下一分钟巡检时主动推送。</p>
            </div>
          </aside>
        </div>

        <div class="form-foot">
          <div class="form-foot-btns">
            <button class="btn primary" type="submit">{{ saving ? '保存中…' : '保存设置' }}</button>
            <button class="btn" type="button" @click="test"><Send :size="14" />{{ testing ? '正在发送…' : '发送测试通知' }}</button>
          </div>
          <span class="settings-help">测试通知使用当前填写的配置立即发送，不会自动保存。</span>
        </div>
      </fieldset>
    </form>
    </section>
    <section v-show="activeTab === 'geoip'" id="settings-panel-geoip" role="tabpanel" aria-labelledby="settings-tab-geoip">
      <div class="settings-intro">
        <div>
          <p>全球城市库（dbip-city-lite）与国内省市/运营商库（ip2region）的状态查看与在线热更新。系统后台也会每月自动巡检更新，无需重启容器。</p>
        </div>
        <span class="tag" :class="geoStatus?.city.loaded ? 'ok' : 'warn'">
          {{ geoLoading ? '正在加载' : geoStatus?.city.loaded ? `已缓存 ${fmtNum(geoStatus.cache_entries)} 个 IP` : '城市库未加载' }}
        </span>
      </div>
      <p v-if="geoError || geoStatus?.last_error" class="banner bad" role="alert">{{ geoError || geoStatus?.last_error }}</p>
      <div class="card settings-section settings-table-card">
        <header class="card-h">
          <h2>归属地数据库状态</h2>
          <span class="meta">
            {{ geoStatus?.last_update_ts ? `最近更新 / 重载：${fmtFull(geoStatus.last_update_ts)}` : '优先使用持久化目录 data/geoip/，缺失时回退容器内置库' }}
          </span>
        </header>
        <div class="settings-table-toolbar">
          <div class="settings-foot-btns">
            <button
              type="button"
              class="btn primary"
              :disabled="!!geoBusy || geoStatus?.updating"
              @click="runGeoIPUpdate('download')"
            >
              <Download :size="15" />
              {{ geoBusy === 'download' || geoStatus?.updating ? '正在下载并热重载…' : '在线拉取最新库并热重载' }}
            </button>
            <button
              type="button"
              class="btn"
              :disabled="!!geoBusy || geoStatus?.updating"
              @click="runGeoIPUpdate('reload')"
            >
              <RefreshCw :size="15" />
              {{ geoBusy === 'reload' ? '正在重载…' : '重新加载本地库文件' }}
            </button>
          </div>
          <span class="settings-save-scope">热重载会立即清空内存中的归属地缓存并按新库重新计算</span>
        </div>
        <div class="settings-table-scroll" role="region" aria-label="GeoIP 数据库状态表格">
          <table class="settings-data ip-tags-table">
            <colgroup>
              <col style="width:180px">
              <col style="width:100px">
              <col style="width:160px">
              <col style="width:110px">
              <col style="width:160px">
              <col>
            </colgroup>
            <thead>
              <tr>
                <th scope="col">数据库名称</th>
                <th scope="col">状态</th>
                <th scope="col">库版本 / 类型</th>
                <th scope="col">文件大小</th>
                <th scope="col">文件更新时间</th>
                <th scope="col">当前生效路径</th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="geoLoading">
                <td colspan="6" class="settings-empty">正在读取 GeoIP 数据库状态…</td>
              </tr>
              <template v-else-if="geoStatus">
                <tr>
                  <td>
                    <strong>全球城市库</strong>
                    <div class="place">dbip-city-lite.mmdb</div>
                  </td>
                  <td>
                    <span class="tag" :class="geoStatus.city.loaded ? 'ok' : 'danger'">
                      {{ geoStatus.city.loaded ? '已加载' : '未加载' }}
                    </span>
                  </td>
                  <td>
                    <div>{{ geoStatus.city.type || '—' }}</div>
                    <div v-if="geoStatus.city.build_ts" class="place mono">构建：{{ fmtFull(geoStatus.city.build_ts) }}</div>
                  </td>
                  <td class="mono">{{ geoStatus.city.size ? fmtSize(geoStatus.city.size) : '—' }}</td>
                  <td class="mono">{{ geoStatus.city.mod_time ? fmtFull(geoStatus.city.mod_time) : '—' }}</td>
                  <td>
                    <code class="mono">{{ geoStatus.city.path || '—' }}</code>
                    <div v-if="geoStatus.city.target && geoStatus.city.path !== geoStatus.city.target" class="place">
                      更新写入目标：<span class="mono">{{ geoStatus.city.target }}</span>
                    </div>
                  </td>
                </tr>
                <tr>
                  <td>
                    <strong>国内省市与运营商库</strong>
                    <div class="place">ip2region.xdb</div>
                  </td>
                  <td>
                    <span class="tag" :class="geoStatus.region.loaded ? 'ok' : 'warn'">
                      {{ geoStatus.region.loaded ? '已加载' : '未加载' }}
                    </span>
                  </td>
                  <td>{{ geoStatus.region.type || 'ip2region v4' }}</td>
                  <td class="mono">{{ geoStatus.region.size ? fmtSize(geoStatus.region.size) : '—' }}</td>
                  <td class="mono">{{ geoStatus.region.mod_time ? fmtFull(geoStatus.region.mod_time) : '—' }}</td>
                  <td>
                    <code class="mono">{{ geoStatus.region.path || '—' }}</code>
                    <div v-if="geoStatus.region.target && geoStatus.region.path !== geoStatus.region.target" class="place">
                      更新写入目标：<span class="mono">{{ geoStatus.region.target }}</span>
                    </div>
                  </td>
                </tr>
              </template>
            </tbody>
          </table>
        </div>
        <footer class="settings-table-foot">
          <span class="settings-help">
            <strong>工作原理：</strong>国外及全球经纬度使用 <code>dbip-city-lite.mmdb</code>（每月初发布新版）；中国大陆省市与运营商（电信/联通/移动等）使用 <code>ip2region.xdb</code> 修正。在线更新会将新版下载到持久化目录 <code>data/geoip/</code>，跨容器升级保留。
          </span>
        </footer>
      </div>
    </section>
  </div>
</template>

<style scoped>
.notification-settings { width: 100%; max-width: 100%; min-width: 0; }
.notification-settings > section { width: 100%; max-width: 100%; min-width: 0; }
.settings-tabs { display: flex; gap: 24px; border-bottom: 1px solid var(--line); margin: -2px 0 22px; max-width: 100%; overflow-x: auto; scrollbar-width: none; -webkit-overflow-scrolling: touch; }
.settings-tabs::-webkit-scrollbar { display: none; }
.settings-tabs button { flex: none; display: flex; align-items: center; gap: 7px; padding: 12px 2px; margin-bottom: -1px; border: 0; border-bottom: 2px solid transparent; background: transparent; color: var(--dim); font: inherit; font-weight: 550; white-space: nowrap; }
.settings-tabs button:hover { color: var(--text); }
.settings-tabs button[aria-selected=true] { color: var(--accent-text); border-bottom-color: var(--accent); }
@media (max-width: 600px) { .settings-tabs { gap: 16px; margin-bottom: 16px; } .settings-tabs button { font-size: 12.5px; gap: 5px; padding: 10px 2px; } }
.settings-intro { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-bottom: 14px; }
.settings-intro p { max-width: 80ch; margin: 0; color: var(--dim); font-size: 13px; }
.settings-layout { display: grid; grid-template-columns: minmax(0, 1fr) 290px; gap: 28px; align-items: start; }
.settings-form { padding: 0; margin: 0; border: 0; min-width: 0; }
.settings-section { margin-bottom: 16px; }
h3 { margin: 0; font-size: 14px; }
.settings-fields { padding: 6px 20px 18px; }
.settings-row { display: grid; grid-template-columns: 108px minmax(0, 1fr); gap: 16px; margin-top: 18px; align-items: start; }
.settings-row > label { padding-top: 8px; font-size: 13px; }
.settings-row .field { width: 100%; }
textarea.field { height: auto; resize: vertical; min-height: 80px; padding: 10px; line-height: 1.6; }
.settings-code { font-family: var(--mono); font-size: 12px; }
.secret-hidden { -webkit-text-security: disc; }
.settings-help { font-size: 12px; line-height: 1.65; color: var(--dim); margin: 6px 0 0; overflow-wrap: anywhere; }
.settings-help code, .card-h .meta code { font-family: var(--mono); font-size: 11.5px; color: var(--text); background: var(--sunken); padding: 1px 5px; border-radius: 4px; }
.settings-toggle { display: flex; align-items: center; justify-content: space-between; gap: 20px; padding: 14px 0; border-bottom: 1px solid var(--line); cursor: pointer; }
.settings-toggle strong { font-size: 13px; font-weight: 550; }
.settings-toggle small { display: block; margin-top: 5px; color: var(--dim); font-size: 12px; }
input[type=checkbox] { accent-color: var(--accent); }
.settings-toggle input { width: 18px; height: 18px; flex-shrink: 0; }
.settings-inline { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.settings-inline input[type=number] { width: 100px; }
.settings-header-actions { margin-bottom: 8px; font-size: 12px; }
.settings-variables { display: flex; flex-wrap: wrap; gap: 5px 9px; margin-top: 8px; color: var(--dim); font-size: 11px; }
.settings-actions { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; padding: 2px 0 12px; }
.settings-actions .settings-help { flex-basis: 100%; }
.settings-preview { min-width: 0; border-left: 1px solid var(--line); padding-left: 24px; }
.notification-sample { padding: 16px; border: 1px solid var(--line); border-top: 3px solid var(--warn); border-radius: 6px; background: var(--elev); margin: 16px 0; }
.notification-sample h4 { font-size: 14px; line-height: 1.5; margin: 12px 0 8px; }
.notification-sample p { white-space: pre-line; font-size: 12px; color: var(--dim); line-height: 1.9; overflow-wrap: anywhere; }
.settings-preview summary { font-size: 12px; cursor: pointer; color: var(--accent-text); }
.settings-preview pre { white-space: pre-wrap; overflow-wrap: anywhere; font-size: 11px; color: var(--dim); background: var(--sunken); padding: 12px; max-height: 350px; overflow: auto; }
.settings-delivery { border-top: 1px solid var(--line); margin-top: 24px; padding-top: 18px; }
.settings-delivery .btn { margin-top: 10px; }
/* ── 单卡片分组表单（推送通知 / 自动封禁）：分组上下排列，每行“标签 | 控件” ── */
.form-card { margin-bottom: 24px; box-shadow: none; overflow: visible; min-width: 0; }
.form-fieldset { min-width: 0; margin: 0; padding: 0; border: 0; }
.form-group { padding: 20px 24px 22px; border-top: 1px solid var(--line); }
.form-card > .form-fieldset > .form-group:first-child { border-top: 0; }
.form-group-h { display: flex; align-items: baseline; gap: 12px; flex-wrap: wrap; margin-bottom: 6px; }
.form-group-h h2 { margin: 0; font-size: 14px; font-weight: 600; }
.form-group-h > span { color: var(--dim); font-size: 12px; }
.form-group-h code { font-family: var(--mono); font-size: 11.5px; color: var(--text); background: var(--sunken); padding: 1px 5px; border-radius: 4px; }
.form-row { display: grid; grid-template-columns: 140px minmax(0, 1fr); gap: 20px; align-items: start; padding-top: 14px; }
.form-label { padding-top: 8px; font-size: 13px; color: var(--text); }
.form-ctrl { min-width: 0; max-width: 100%; }
.form-ctrl-wide { max-width: none; }
.sensitive-foot { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; }
.sensitive-foot .btn { margin-top: 6px; flex-shrink: 0; }
.form-ctrl > .field { width: 100%; }
.form-narrow { max-width: 440px; }
.form-help-top { margin: 0 0 6px; }
.form-switch { position: relative; display: inline-flex; align-items: center; gap: 8px; min-height: 34px; cursor: pointer; color: var(--dim); font-size: 12.5px; }
.form-switch input { position: absolute; opacity: 0; width: 28px; height: 18px; margin: 0; }
.form-switch input:checked + .switch-track { background: var(--accent); }
.form-switch input:checked + .switch-track::after { transform: translateX(11px); }
.form-switch input:checked ~ span:last-child { color: var(--accent-text); font-weight: 550; }
.form-switch input:focus-visible + .switch-track { outline: 2px solid var(--accent-text); outline-offset: 3px; }
.form-foot { display: flex; align-items: center; justify-content: space-between; gap: 12px 20px; flex-wrap: wrap; padding: 14px 24px; border-top: 1px solid var(--line); background: var(--sunken); border-radius: 0 0 10px 10px; }
.form-foot-btns { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.form-foot > .settings-help { margin: 0; }
.form-foot-status { display: flex; align-items: center; gap: 6px 16px; flex-wrap: wrap; color: var(--dim); font-size: 12px; }
.form-foot-status b { color: var(--text); font-weight: 500; }
.form-split { display: grid; grid-template-columns: minmax(0, 1fr) minmax(340px, 420px); align-items: stretch; }
.form-main { min-width: 0; }
.form-main > .form-group:first-child { border-top: 0; }
.form-aside { min-width: 0; padding: 20px 24px; border-left: 1px solid var(--line); background: color-mix(in srgb, var(--sunken) 55%, transparent); display: flex; flex-direction: column; }
.form-aside .notification-sample { margin: 12px 0; }
.form-aside summary { font-size: 12px; cursor: pointer; color: var(--accent-text); user-select: none; }
.form-aside pre { white-space: pre-wrap; overflow-wrap: anywhere; font-size: 11px; color: var(--dim); background: var(--elev); padding: 10px 12px; margin: 8px 0 0; max-height: 220px; overflow: auto; border-radius: 6px; border: 1px solid var(--line); }
.aside-status-box { margin-top: 20px; padding-top: 18px; border-top: 1px solid var(--line); }
.aside-status-box .template-preview-h { justify-content: space-between; }
.aside-status-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; margin-top: 10px; }
.aside-status-grid > div { padding: 10px 12px; border: 1px solid var(--line); border-radius: 6px; background: var(--elev); }
.aside-status-grid span { display: block; font-size: 11px; color: var(--dim); }
.aside-status-grid strong { display: block; margin-top: 4px; font-size: 12px; font-weight: 600; color: var(--text); }
.template-preview-h { display: flex; align-items: baseline; gap: 10px; flex-wrap: wrap; }
.template-preview-h h2 { margin: 0; font-size: 14px; font-weight: 600; }
.template-preview-h span { color: var(--dim); font-size: 12px; }
.form-cols { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); }
.form-cols > .form-group { border-top: 0; }
.form-cols > .form-group + .form-group { border-left: 1px solid var(--line); }
.form-cols .form-row { grid-template-columns: 96px minmax(0, 1fr); }
/* ── 自动封禁 ─────────────────────────────────────────────────────────────── */
.autoban-overview { display: grid; grid-template-columns: repeat(3, minmax(110px, 170px)) minmax(0, 1fr) auto; align-items: center; border-bottom: 1px solid var(--line); background: var(--sunken); border-radius: 10px 10px 0 0; }
.autoban-overview + .form-fieldset > .form-group:first-child { border-top: 0; }
.autoban-stat { min-width: 0; padding: 14px 24px; border-right: 1px solid var(--line); }
.autoban-stat-label { display: block; font-size: 11.5px; color: var(--dim); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.autoban-stat-val { display: block; margin-top: 4px; font-size: 20px; line-height: 1.3; font-weight: 650; color: var(--text); font-variant-numeric: tabular-nums; }
.autoban-last-ip { display: block; margin-top: 4px; font-size: 14px; line-height: 1.5; font-weight: 600; color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.autoban-last-ip.muted-text { color: var(--faint); font-weight: 500; }
.autoban-last-meta { display: block; margin-top: 2px; font-size: 11.5px; color: var(--dim); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.autoban-overview-actions { display: flex; align-items: center; gap: 8px; padding: 12px 24px; }
.autoban-overview-actions .btn { white-space: nowrap; }
.autoban-num { width: 80px !important; text-align: center; }
.autoban-safe-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 10px; list-style: none; margin: 0; padding: 0; }
.autoban-safe-grid li { min-width: 0; padding: 10px 12px; border: 1px solid var(--line); border-left: 3px solid var(--ok); border-radius: 6px; background: var(--sunken); }
.autoban-safe-grid strong { display: block; font-size: 12.5px; font-weight: 600; color: var(--text); }
.autoban-safe-grid span { display: block; margin-top: 4px; font-size: 11.5px; line-height: 1.6; color: var(--dim); }
@media (max-width: 1280px) {
  .autoban-overview { grid-template-columns: repeat(3, minmax(0, 1fr)); }
  .autoban-stat:nth-child(3) { border-right: 0; }
  .autoban-stat-last { grid-column: 1 / -1; border-right: 0; border-top: 1px solid var(--line); }
  .autoban-overview-actions { grid-column: 1 / -1; border-top: 1px solid var(--line); padding: 10px 24px; }
  .autoban-safe-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
@media (max-width: 1280px) { .form-cols { grid-template-columns: minmax(0, 1fr); } .form-cols > .form-group + .form-group { border-left: 0; border-top: 1px solid var(--line); } .form-cols .form-row { grid-template-columns: 140px minmax(0, 1fr); } }
@media (max-width: 1100px) { .form-split { grid-template-columns: minmax(0, 1fr); } .form-aside { border-left: 0; border-top: 1px solid var(--line); } }
@media (max-width: 760px) {
  .form-group { padding: 16px 14px 18px; }
  .form-row, .form-cols .form-row { grid-template-columns: minmax(0, 1fr); gap: 6px; padding-top: 12px; }
  .form-aside { padding: 16px 14px; }
  .form-label { padding-top: 0; font-weight: 550; }
  .form-narrow { max-width: none; }
  .form-foot { padding: 12px 14px; }
  .form-foot-btns { width: 100%; }
  .form-foot-btns > .btn { flex: 1 1 0; justify-content: center; }
}
@media (max-width: 600px) {
  .autoban-stat { padding: 12px 14px; }
  .autoban-stat-val { font-size: 17px; }
  .autoban-overview-actions { padding: 10px 14px; }
  .autoban-overview-actions .btn { flex: 1 1 0; justify-content: center; }
  .autoban-safe-grid { grid-template-columns: minmax(0, 1fr); }
}

/* 设置列表保持一条规则一行；列宽按编辑内容分配，统计与操作独立对齐。 */
.settings-table-card { margin-bottom: 24px; overflow: visible; box-shadow: none; min-width: 0; max-width: 100%; }
.settings-table-card > .card-h { padding: 14px 18px; gap: 12px; flex-wrap: wrap; }
.settings-table-card > .card-h .meta { line-height: 1.7; }
.settings-table-scroll { margin: 16px 18px; border: 1px solid var(--line); border-radius: 7px; overflow-x: auto; -webkit-overflow-scrolling: touch; max-width: calc(100% - 36px); }
.settings-data { width: 100%; table-layout: fixed; border-collapse: separate; border-spacing: 0; font-size: 12.5px; }
.address-sets-table { min-width: 1000px; }
.set-hosts { display: flex; flex-wrap: wrap; align-items: center; gap: 5px; }
.set-host-token { display: inline-flex; align-items: center; max-width: 100%; min-height: 23px; padding: 2px 7px; border: 1px solid var(--line); border-radius: 4px; background: var(--sunken); color: var(--text); font-family: var(--mono); font-size: 12px; line-height: 18px; overflow-wrap: anywhere; }
.host-rules-table { min-width: 1452px; }
.ip-tags-table { min-width: 780px; }
.settings-data th { background: var(--sunken); color: var(--dim); text-align: left; font-size: 12px; font-weight: 600; height: 44px; padding: 12px; line-height: 20px; vertical-align: middle; border-bottom: 1px solid var(--line); white-space: nowrap; }
.settings-data td { padding: 13px 12px; vertical-align: middle; border-bottom: 1px solid var(--line); }
.settings-data tbody tr:last-child td { border-bottom: 0; }
.settings-data tbody tr:hover { background: var(--hover); }
.settings-data .stat-column { text-align: center; }
.settings-data .action-column { text-align: center; padding: 12px; }
.settings-data .action-column .btn { display: inline-flex; justify-content: center; align-items: center; width: 60px; height: 32px; padding: 0 6px; gap: 5px; vertical-align: middle; }
.settings-data .field, .settings-data :deep(.sel-btn) { width: 100%; min-width: 0; height: 34px; background: var(--elev); }
.settings-data .field:hover { border-color: var(--line-strong); }
.settings-data .field { font-size: 12.5px; }
.settings-data :deep(.sel-btn) { font-size: 12px; }
.rule-disabled td:not(:first-child):not(:last-child) > :not(.rule-stat) { opacity: .55; }
.rule-stat { display: inline-flex; flex-direction: column; align-items: center; gap: 3px; color: var(--dim); font-size: 11px; white-space: nowrap; }
.rule-stat strong { color: var(--text); font-size: 15px; font-weight: 600; font-variant-numeric: tabular-nums; }
.rule-stat .has-blocks { color: var(--warn); }
.rule-stat-link { color: var(--accent-text); font-size: 11px; }
.settings-data tfoot td { background: var(--sunken); border-top: 1px solid var(--line); border-bottom: 0; padding-top: 10px; padding-bottom: 10px; font-size: 11.5px; color: var(--dim); }
.table-summary-note { color: var(--faint); margin-left: 20px; }
.table-summary-count { white-space: nowrap; }
.table-summary-count strong { color: var(--text); font-variant-numeric: tabular-nums; }
.rule-state-toggle { position: relative; display: inline-flex; align-items: center; gap: 6px; cursor: pointer; color: var(--dim); font-size: 12px; white-space: nowrap; }
.rule-state-toggle input { position: absolute; opacity: 0; width: 28px; height: 18px; margin: 0; }
.switch-track { width: 28px; height: 17px; border-radius: 10px; background: var(--line-strong); padding: 2px; }
.switch-track::after { content: ''; display: block; width: 13px; height: 13px; border-radius: 50%; background: #fff; }
.rule-state-toggle input:checked + .switch-track { background: var(--accent); }
.rule-state-toggle input:checked + .switch-track::after { transform: translateX(11px); }
.rule-state-toggle input:focus-visible + .switch-track { outline: 2px solid var(--accent-text); outline-offset: 3px; }
.table-hint { color: var(--dim); font-size: 12px; line-height: 1.7; }
.settings-data .settings-empty { text-align: center; padding: 28px 18px; color: var(--dim); }
.tag-preview { display: inline-flex; align-items: center; gap: 6px; min-height: 28px; max-width: 100%; padding: 4px 9px; border: 1px solid color-mix(in srgb, var(--info) 25%, var(--line)); border-radius: 5px; color: var(--info); background: var(--info-soft); font-size: 12px; line-height: 1.5; font-weight: 550; vertical-align: middle; }
.tag-preview svg { flex-shrink: 0; }
.tag-preview > span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

/* ── 自定义域名多选下拉组合框 ─────────────────────────────────────────────── */
.host-combo { position: relative; width: 100%; }
.host-combo-box {
  position: relative;
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 5px;
  padding: 5px 28px 5px 6px;
  min-height: 36px;
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--elev);
  cursor: text;
}
.host-combo-box:hover { border-color: var(--line-strong); }
.host-combo.open .host-combo-box,
.host-combo-box:focus-within {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-soft);
}
.host-token {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  max-width: 100%;
  min-height: 23px;
  padding: 2px 4px 2px 7px;
  border-radius: 4px;
  background: var(--accent-soft);
  border: 1px solid color-mix(in srgb, var(--accent) 24%, transparent);
  color: var(--accent-text);
}
.host-token-val {
  font-size: 11.5px;
  line-height: 1.45;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.host-token-stat {
  display: inline-flex;
  align-items: center;
  padding: 0 4px;
  height: 16px;
  border-radius: 3px;
  background: var(--warn-soft);
  color: var(--warn);
  font-size: 10.5px;
  font-weight: 600;
  text-decoration: none;
  flex-shrink: 0;
}
.host-token-stat:hover {
  filter: brightness(0.95);
}
.host-token button {
  display: grid;
  place-items: center;
  width: 16px;
  height: 16px;
  flex-shrink: 0;
  padding: 0;
  border: 0;
  border-radius: 3px;
  background: transparent;
  color: var(--accent-text);
  opacity: .75;
}
.host-token button:hover {
  opacity: 1;
  color: var(--danger);
  background: var(--danger-soft);
}
.host-combo-input {
  flex: 1 1 88px;
  min-width: 0;
  width: 88px;
  max-width: 100%;
  height: 24px;
  padding: 0 3px;
  border: 0;
  background: transparent;
  color: var(--text);
  font-size: 11.5px;
  outline: none;
}
.host-combo-input.compact {
  flex: 0 1 20px;
  width: 20px;
}
.host-combo-btn {
  position: absolute; right: 2px; top: 50%; transform: translateY(-50%);
  width: 24px; height: 26px; border: 0; background: transparent; color: var(--faint);
  display: grid; place-items: center; border-radius: 6px; padding: 0;
}
.host-combo-btn:hover { color: var(--text); }
.host-combo-caret { transition: transform .15s; }
.host-combo.open .host-combo-caret { transform: rotate(180deg); color: var(--accent-text); }

.host-combo .sel-pop {
  position: absolute; left: 0; top: calc(100% + 4px); z-index: 100;
  min-width: 100%; max-width: min(380px, 92vw); width: max-content;
  background: var(--elev); border: 1px solid var(--line); border-radius: 8px;
  box-shadow: var(--pop-shadow); padding: 6px;
}
.host-combo .sel-pop.up { top: auto; bottom: calc(100% + 4px); }
.host-combo .sel-head {
  display: flex; align-items: center; justify-content: space-between; gap: 8px;
  padding: 4px 6px 6px; border-bottom: 1px solid var(--line); margin-bottom: 4px;
  font-size: 11px; color: var(--dim);
}
.host-combo .sel-head-actions { display: inline-flex; align-items: center; gap: 6px; }
.host-combo .sel-act-btn {
  border: 0; background: transparent; color: var(--accent-text);
  font-size: 11px; font-weight: 550; padding: 1px 4px; border-radius: 4px; cursor: pointer;
}
.host-combo .sel-act-btn:hover { background: var(--accent-soft); }
.host-combo .sel-list { list-style: none; margin: 0; padding: 0; max-height: 250px; overflow: auto; }
.host-combo .sel-opt {
  display: flex; align-items: center; gap: 8px; justify-content: space-between;
  min-height: 31px; padding: 4px 8px; border-radius: 6px; cursor: pointer;
  font-size: 12px; color: var(--text); white-space: nowrap;
}
.host-combo .sel-opt-main { display: flex; align-items: center; gap: 7px; min-width: 0; flex: 1; }
.host-combo .sel-opt-main input[type=checkbox] { margin: 0; flex-shrink: 0; pointer-events: none; }
.host-combo .sel-opt.active { background: var(--hover); }
.host-combo .sel-opt.selected { color: var(--accent-text); font-weight: 560; background: color-mix(in srgb, var(--accent-soft) 55%, transparent); }
.host-combo .sel-opt.selected.active { background: var(--accent-soft); }
.host-combo .sel-opt.disabled { opacity: .5; cursor: not-allowed; }
.host-combo .sel-opt-custom { color: var(--accent-text); background: color-mix(in srgb, var(--accent-soft) 45%, transparent); margin-bottom: 3px; }
.host-combo .sel-opt-custom:hover { background: var(--accent-soft); }
.host-combo .sel-used-badge {
  font-family: var(--sans); font-size: 10.5px; color: var(--faint);
  background: var(--sunken); padding: 1px 5px; border-radius: 4px; flex-shrink: 0;
}
.host-combo .sel-stat-badge {
  font-family: var(--sans); font-size: 10.5px; color: var(--warn);
  background: var(--warn-soft); padding: 1px 5px; border-radius: 4px; flex-shrink: 0;
}
.host-combo .sel-label { overflow: hidden; text-overflow: ellipsis; }
.host-combo .sel-empty { padding: 10px 8px; color: var(--faint); font-size: 12px; }

/* ── 放行标识胶囊按钮 ─────────────────────────────────────────────────────── */
.settings-table-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 14px; flex-wrap: wrap; padding: 16px 18px 0; }
.settings-save-group { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.settings-save-scope { color: var(--dim); font-size: 11.5px; }
.settings-table-foot { display: flex; align-items: center; justify-content: space-between; gap: 14px; flex-wrap: wrap; padding: 16px 24px; border-top: 1px solid var(--line); border-radius: 0 0 10px 10px; }
.settings-foot-btns { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.settings-table-foot > .settings-help { flex-basis: 100%; margin: 0; font-size: 11.5px; }
@media (max-width: 760px) {
  .settings-intro { align-items: flex-start; flex-direction: column; gap: 8px; }
  .settings-table-toolbar { padding: 14px 12px 0; gap: 10px; }
  .settings-foot-btns { width: 100%; }
  .settings-foot-btns > .btn { flex: 1 1 auto; justify-content: center; }
  .settings-save-group { width: 100%; justify-content: space-between; }
  .settings-table-scroll { margin: 12px; max-width: calc(100% - 24px); }
  .settings-table-foot { padding: 14px 12px; }
  .settings-table-card > .card-h { padding: 14px 12px; }
}
@media (max-width: 420px) {
  .settings-table-foot > .primary, .settings-save-group > .primary { width: 100%; justify-content: center; }
}
@media (min-width: 1600px) {
  .settings-layout { grid-template-columns: minmax(0, 1fr) 320px; }
  .settings-form { display: grid; grid-template-columns: minmax(300px, .85fr) minmax(420px, 1.15fr); gap: 16px; align-items: stretch; }
  .settings-section { margin-bottom: 0; }
  .settings-actions { grid-column: 1 / -1; padding-top: 4px; }
  .settings-preview { position: sticky; top: 16px; }
}
@media (max-width: 1100px) { .settings-layout { grid-template-columns: minmax(0, 1fr); } .settings-preview { border-left: 0; padding-left: 0; } }
@media (max-width: 600px) { .settings-row { grid-template-columns: minmax(0, 1fr); gap: 6px; } .settings-fields { padding-left: 14px; padding-right: 14px; } .settings-intro { align-items: flex-start; flex-direction: column; } }
</style>
