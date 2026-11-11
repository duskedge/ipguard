// 与后端 JSON 字段一一对应（见 docs/实现指南.md §3）。

export type Level = 'scan' | 'warn' | 'normal'
export type Range = '1h' | '24h' | '7d'
export type LogRange = Range | 'today'
export type Source = 'manual' | 'auto' | 'community'

export interface Paged<T> { rows: T[]; page: number; pages: number; total: number }

export interface IPRow {
  ip: string; tag?: string; geo: string; cc: string; count: number; err_ratio: number; share: number
  last_ts: number; level: Level; host: string; paths: string[]; banned: boolean
}

export interface LogRow {
  id: string; ts: number; ip: string; tag?: string; geo: string; cc: string; method: string; host: string
  path: string; status: number; size: number; ua: string; backend: string; level: Level | ''; banned: boolean
}

export interface CountryRow { name: string; cc: string; req: number; ip: number; domestic: boolean; lat: number; lng: number }
export interface ProvinceRow { province: string; cities: string; city: string; req: number; ip: number; lat: number; lng: number }

export interface GeoResult {
  total: number; unique_ip: number; domestic_req: number; overseas_req: number; lan_req: number
  unknown_req: number; domestic_ip: number; countries: CountryRow[]; provinces: ProvinceRow[]
}

export interface Trend { range: Range; labels: string[]; values: number[]; hint: string; sum: number; peak: number; peak_at: string }

export interface Overview {
  now: number; today_requests: number; yesterday_requests: number; req_delta_pct: number | null
  unique_ip: number; yesterday_ip: number; ip_delta_pct: number | null
  overseas_pct: number; yesterday_overseas_pct: number
  trend: Trend; geo: GeoResult; top_ips: IPRow[]; anomalies: IPRow[]; hosts: number
}

export interface BanCounts { total: number; manual: number; auto: number; community: number }

export interface Ban {
  id: number; target: string; scope: 'ip' | 'range'; geo: string; cc: string; country: string
  source: Source; origin: string; scenario: string; remain_min: number; permanent: boolean; created_ts: number
}

export interface BanList extends Paged<Ban> { counts: BanCounts; countries: string[]; error?: string }

export interface IPDetail {
  ip: string; tag?: string; geo: string; cc: string; province: string
  counts: Record<Range, number>; err_ratio: Record<Range, number>
  last_ts: number; level: Level; host: string; hosts: string[]; paths: string[]; banned: boolean; recent: LogRow[]
}

export interface CrowdSecStatus { configured: boolean; connected: boolean; url: string; error?: string; refreshed_at: number }

export interface Meta {
  user: string; site: string
  build: { version: string; git_commit: string; build_time: string }
  crowdsec: CrowdSecStatus
  geoip: { loaded: boolean; path: string }
  logs: { preview?: { scheme: 'http' | 'https'; port: number }; dir: string; entries: number; hosts: number; stats: { files: number; last_error?: string } }
}

export interface AuditLog {
  id: number; created_at: string; operator: string; client_ip: string; action: 'ban' | 'unban'
  target: string; duration: string; reason: string; source: string; result: 'ok' | 'fail'; error: string
}

export interface NotificationConfig {
  enabled: boolean
  url: string
  headers: string
  body: string
  detail_url: string
  cooldown_min: number
  min_level: 'warn' | 'scan'
}
export interface NotificationSettings extends NotificationConfig {
  has_headers: boolean
  status: { last_attempt: number; last_success: number; error: string }
}

export interface IPTagItem {
  target: string
  tag: string
  note?: string
}

export type HostRuleMode = 'domestic' | 'allowlist'

export interface AddressSet { tag?: string; id: string; name: string; ips: string[]; note?: string; hosts?: string[] }

export interface HostRule {
  host: string
  hosts?: string[]
  enabled: boolean
  mode: HostRuleMode
  allow_sets?: string[]
  allow_ips?: string[]
  allow_tags?: string[]
  note?: string
}

export interface HostRuleStat {
  blocked_count: number
  last_blocked_ip?: string
  last_blocked_at?: number
  host_stats?: Record<string, HostRuleStat>
}

export interface HostRuleView extends HostRule {
  blocked_count: number
  last_blocked_ip?: string
  last_blocked_at?: number
  host_stats?: Record<string, HostRuleStat>
}

export interface GuardLog {
  hits: number; last_ts: number
  id: number; ts: number; ip: string; host: string; path: string; method: string; ua: string
  rule_host: string; mode: HostRuleMode; reason: string; geo: string; cc: string; tag: string
  allow_ips: string; allow_tags: string
}

export interface HostStatRow {
  host: string
  backend: string; count: number; share: number; unique_ip: number
  bytes: number; err_count: number; err_ratio: number; sens_hits: number; last_ts: number; top_paths: string[]
}

export interface PathStatRow {
  host: string
  path: string; count: number; unique_ip: number; bytes: number
  err_count: number; err_ratio: number; top_status: number; sensitive: boolean
}

export interface HostsResult extends Paged<HostStatRow> {
  window_total: number; total_bytes: number; total_hosts: number; total_err: number; total_sens: number
  selected_host?: string; top_paths: PathStatRow[]; err_paths: PathStatRow[]
}

export interface GeoDBFileStatus {
  loaded: boolean; path: string; target: string; type?: string
  build_ts?: number; mod_time?: number; size?: number
}

export interface GeoIPStatus {
  city: GeoDBFileStatus; region: GeoDBFileStatus; cache_entries: number
  updating: boolean; last_update_ts: number; last_error?: string
}


export type AutoBanDuration = '1h' | '6h' | '24h' | '7d' | '30d' | 'forever'

export interface AutoBanConfig {
  guard_enabled: boolean
  guard_window_min: number
  guard_threshold: number
  guard_duration: AutoBanDuration
  scan_enabled: boolean
  scan_min_level: 'scan' | 'warn'
  scan_min_count: number
  scan_duration: AutoBanDuration
  notify_enabled: boolean
  sensitive_paths: string[]
}

export interface AutoBanStatus {
  total_banned: number
  guard_banned: number
  scan_banned: number
  last_ban_at: number
  last_ban_ip: string
  last_ban_reason: string
  error: string
}

export interface AutoBanSettings extends AutoBanConfig {
  default_sensitive_paths: string[]
  sensitive_custom: boolean
  status: AutoBanStatus
}
