import axios, { AxiosError } from 'axios'

import type {
  AddressSet, AutoBanConfig, AutoBanSettings, GuardLog, NotificationConfig, NotificationSettings, IPTagItem, HostRule, HostRuleView, AuditLog, BanList, IPDetail, IPRow, GeoResult, GeoIPStatus, HostsResult, LogRange, LogRow, Meta, Overview, Paged, Range, BanCounts,
} from './types'

const TOKEN_KEY = 'ipguard-token'

export function getToken(): string {
  try { return localStorage.getItem(TOKEN_KEY) ?? '' } catch { return '' }
}
export function setToken(t: string) {
  try {
    if (t) localStorage.setItem(TOKEN_KEY, t)
    else localStorage.removeItem(TOKEN_KEY)
  } catch { /* 隐私模式下忽略 */ }
}

const http = axios.create({ baseURL: '/api', timeout: 40_000 })

http.interceptors.request.use((cfg) => {
  const t = getToken()
  if (t) cfg.headers.Authorization = `Bearer ${t}`
  return cfg
})

let onUnauthorized: (() => void) | null = null
export function setUnauthorizedHandler(fn: () => void) { onUnauthorized = fn }

http.interceptors.response.use(undefined, (err: AxiosError) => {
  if (err.response?.status === 401 && !String(err.config?.url).includes('/auth/login')) onUnauthorized?.()
  return Promise.reject(err)
})

/** 取后端返回的中文错误信息。 */
export function errText(e: unknown): string {
  const ax = e as AxiosError<{ error?: string }>
  return ax?.response?.data?.error ?? (ax?.message || '请求失败')
}

type Q = Record<string, string | number | undefined>
const get = <T>(url: string, params?: Q) => http.get<T>(url, { params }).then((r) => r.data)

export const api = {
  login: (username: string, password: string) =>
    http.post<{ token: string; expires_at: number; username: string }>('/auth/login', { username, password }).then((r) => r.data),
  notificationSettings: () => get<NotificationSettings>('/settings/notifications'),
  saveNotifications: (body: NotificationConfig & { clear_headers: boolean }) =>
    http.put<NotificationSettings>('/settings/notifications', body).then((r) => r.data),
  testNotification: (body: NotificationConfig & { clear_headers: boolean }) =>
    http.post('/settings/notifications/test', body).then((r) => r.data),
  autoBanSettings: () => get<AutoBanSettings>('/settings/autoban'),
  saveAutoBan: (body: AutoBanConfig) =>
    http.put<AutoBanSettings>('/settings/autoban', body).then((r) => r.data),
  ipTagSettings: () => get<{ items: IPTagItem[] }>('/settings/ip-tags'),
  saveIPTags: (items: IPTagItem[]) =>
    http.put<{ items: IPTagItem[] }>('/settings/ip-tags', { items }).then((r) => r.data),
  addAddressSetIP: (id: string, ip: string) => http.post<{ added: boolean }>(`/settings/address-sets/${encodeURIComponent(id)}/ips`, { ip }).then((r) => r.data),
  addressSetSettings: () => get<{ sets: AddressSet[] }>('/settings/address-sets'),
  saveAddressSets: (sets: AddressSet[]) => http.put<{ sets: AddressSet[] }>('/settings/address-sets', { sets }).then((r) => r.data),
  hostRuleSettings: () => get<{ rules: HostRuleView[]; hosts: string[]; tags: string[] }>('/settings/host-rules'),
  saveHostRules: (rules: HostRule[]) =>
    http.put<{ rules: HostRuleView[] }>('/settings/host-rules', { rules }).then((r) => r.data),
  geoipSettings: () => get<GeoIPStatus>('/settings/geoip'),
  updateGeoip: (mode: 'download' | 'reload') =>
    http.post<GeoIPStatus>('/settings/geoip/update', { mode }, { timeout: 200_000 }).then((r) => r.data),
  meta: () => get<Meta>('/meta'),
  overview: (trend: Range) => get<{ overview: Overview; bans: BanCounts }>('/overview', { trend }),
  logs: (q: Q & { range: LogRange }) => get<Paged<LogRow>>('/logs', q),
  guardLogs: (q: Q) => get<Paged<GuardLog> & { omitted: number }>('/logs/guard', q),
  logFilters: () => get<{ hosts: string[]; countries: string[]; tags: string[] }>('/logs/filters'),
  hosts: (q: Q) => get<HostsResult>('/hosts', q),
  ips: (q: Q) => get<Paged<IPRow> & { window_total: number; tags?: string[] }>('/ips', q),
  ip: (ip: string) => get<IPDetail>(`/ips/${encodeURIComponent(ip)}`),
  geo: (range: LogRange) => get<GeoResult>('/geo', { range }),
  lookup: (ip: string) => get<{ geo: string; cc: string; banned: boolean }>('/geo/lookup', { ip }),
  bans: (q: Q) => get<BanList>('/bans', q),
  ban: (target: string, duration: string, reason: string) =>
    http.post<{ target: string; scope: string; permanent: boolean }>('/bans', { target, duration, reason }).then((r) => r.data),
  unban: (body: { id?: number; target?: string }) =>
    http.delete<{ removed: string[] }>('/bans', { data: body }).then((r) => r.data),
  audit: (q: Q) => get<{ rows: AuditLog[]; total: number }>('/audit', q),
}
