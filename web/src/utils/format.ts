// 展示用的纯函数，移植自原型（prototype/index.html）。

const TZ = 'Asia/Shanghai'

export function fmtNum(n: number | null | undefined): string {
  return Number(n ?? 0).toLocaleString('zh-CN')
}

/** 国家码 → 国旗 emoji（区域指示符）；内网、未知返回空串。 */
export function flag(cc: string | null | undefined): string {
  if (!cc || !/^[A-Z]{2}$/.test(cc)) return ''
  return String.fromCodePoint(...[...cc].map((c) => 0x1f1e6 + c.charCodeAt(0) - 65))
}

function partsOf(ts: number): Record<string, string> {
  const map: Record<string, string> = {}
  for (const p of new Intl.DateTimeFormat('en-GB', {
    timeZone: TZ, year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23',
  }).formatToParts(new Date(ts))) map[p.type] = p.value
  return map
}

export function fmtFull(ts: number): string {
  if (!ts) return '—'
  const p = partsOf(ts)
  return `${p.year}-${p.month}-${p.day} ${p.hour}:${p.minute}`
}

export function fmtShort(ts: number): string {
  if (!ts) return '—'
  const p = partsOf(ts)
  return `${p.month}-${p.day} ${p.hour}:${p.minute}:${p.second}`
}

export function fmtClock(ts: number): string {
  const p = partsOf(ts)
  return `${p.hour}:${p.minute}`
}

export function fmtSize(n: number): string {
  if (!n) return '0'
  if (n < 1024) return n + ' B'
  if (n < 1048576) return (n / 1024).toFixed(n < 10240 ? 1 : 0) + ' KB'
  if (n < 1073741824) return (n / 1048576).toFixed(1) + ' MB'
  return (n / 1073741824).toFixed(2) + ' GB'
}

/** 剩余分钟 → 中文时长；permanent 为真时显示“永久”。 */
export function fmtRemain(min: number, permanent = false): string {
  if (permanent || !isFinite(min)) return '永久'
  min = Math.max(0, Math.round(min))
  if (min === 60) return '1 小时'
  if (min === 1440) return '24 小时'
  if (min === 10080) return '7 天'
  if (min < 60) return min + ' 分钟'
  const h = Math.floor(min / 60), m = min % 60
  if (h < 24) return m ? `${h} 小时 ${m} 分` : `${h} 小时`
  const d = Math.floor(h / 24), rh = h % 24
  return rh ? `${d} 天 ${rh} 小时` : `${d} 天`
}

export function pct(x: number): string {
  if (!x) return '0%'
  const n = x * 100
  return (n < 10 ? n.toFixed(1) : String(Math.round(n))) + '%'
}

export function share(n: number, d: number): string {
  if (!d) return '0%'
  return ((n / d) * 100).toFixed(1) + '%'
}

export function signed(n: number | null | undefined): string {
  if (n == null) return '—'
  return (n > 0 ? '+' : '') + n + '%'
}

export function ipToInt(ip: string): number | null {
  const p = String(ip).split('.')
  if (p.length !== 4) return null
  let n = 0
  for (const x of p) {
    if (!/^\d{1,3}$/.test(x)) return null
    const v = Number(x)
    if (v > 255) return null
    n = n * 256 + v
  }
  return n >>> 0
}

const V6 = /^[0-9a-f:]+$/i

/** 是否为合法的 IPv4/IPv6 地址或 CIDR（服务端还会再校验一次）。 */
export function isIpOrCidr(s: string): boolean {
  s = String(s).trim()
  const [addr, bits, extra] = s.split('/')
  if (extra !== undefined || !addr) return false
  if (bits !== undefined && !/^\d{1,3}$/.test(bits)) return false
  if (ipToInt(addr) != null) return bits === undefined || Number(bits) <= 32
  if (addr.includes(':') && V6.test(addr) && (addr.match(/::/g) ?? []).length <= 1) {
    return bits === undefined || Number(bits) <= 128
  }
  return false
}

export function methodCls(m: string): string {
  return m === 'POST' ? 'm-post' : m === 'HEAD' ? 'm-head' : 'm-get'
}

export function codeCls(c: number): string {
  return c >= 500 ? 'c5' : c >= 400 ? 'c4' : c >= 300 ? 'c3' : 'c2'
}

const STATUS_EXPLANATIONS: Record<number, string> = {
  101: '切换协议：服务器同意切换通信协议，常用于 WebSocket 连接。',
  200: '请求成功：服务器已正常处理请求。',
  201: '创建成功：服务器已创建请求的资源。',
  202: '已接受：请求已接收，但处理尚未完成。',
  204: '请求成功：没有需要返回的响应正文。',
  206: '部分内容：返回文件的一部分，常见于视频播放或断点续传。',
  207: '多状态：响应中包含多项操作的结果，常用于 WebDAV，需查看各项结果。',
  301: '永久重定向：资源已迁移，请求应转到响应指定的新地址。',
  302: '临时重定向：本次请求需转到响应指定的其他地址。',
  303: '查看其他地址：客户端应通过 GET 请求获取指定地址的资源。',
  304: '资源未修改：客户端已有缓存，服务器确认文件没有变化，继续使用本地缓存即可。这是正常响应，不是访问失败；不传输响应正文，因此大小为 0。',
  307: '临时重定向：转到其他地址，并保持原请求方法和正文。',
  308: '永久重定向：转到新地址，并保持原请求方法和正文。',
  400: '请求错误：请求格式或参数不符合服务器要求。',
  401: '需要认证：未提供有效的登录凭据，或凭据已过期。',
  403: '禁止访问：服务器拒绝此请求，可能与权限或访问策略有关，仅凭状态码无法确认是否被封禁。',
  404: '未找到：服务器找不到请求的路径或资源。',
  405: '方法不允许：该资源不支持当前请求方法。',
  408: '请求超时：服务器未在规定时间内收到完整请求。',
  409: '资源冲突：请求与资源当前状态冲突。',
  410: '资源已移除：请求的资源已永久不可用。',
  413: '请求体过大：上传内容超过服务器允许的大小。',
  414: '地址过长：请求的 URL 超过服务器允许的长度。',
  415: '媒体类型不支持：服务器不支持请求内容的格式。',
  422: '内容无法处理：请求格式可识别，但内容不符合处理要求。',
  429: '请求过多：触发访问频率限制，请稍后重试。',
  444: '连接已关闭：Nginx 主动断开连接，未返回 HTTP 响应，常用于拒绝不符合规则的请求。',
  499: '客户端断开：Nginx 记录到客户端在响应完成前关闭了连接，可能是取消请求或客户端等待超时。',
  500: '服务器内部错误：服务处理请求时发生异常。',
  501: '尚未实现：服务器不支持处理该请求所需的功能。',
  502: '网关错误：反向代理未从后端服务获得有效响应，需检查后端服务和连接。',
  503: '服务不可用：服务暂时无法处理请求，可能处于维护或过载状态。',
  504: '网关超时：反向代理等待后端服务响应超时。',
}

export function statusTitle(code: number): string {
  const fallback = code >= 100 && code < 200 ? '信息响应：请求已接收，正在继续处理。'
    : code >= 200 && code < 300 ? '成功响应：请求已成功处理。'
    : code >= 300 && code < 400 ? '重定向或缓存响应：需结合响应头确认后续处理。'
    : code >= 400 && code < 500 ? '请求相关错误：需检查请求、认证或访问限制。'
    : code >= 500 && code < 600 ? '服务器相关错误：需检查服务或上游连接。'
    : '非标准状态码：请结合对应服务的日志确认具体含义。'
  return `${code}：${STATUS_EXPLANATIONS[code] ?? fallback}`
}

export function ratioCls(v: number): string {
  return v >= 0.6 ? 'hot' : v >= 0.2 ? 'warn' : ''
}

/** 封禁时长选择 → 提交给后端的 duration 文本 */
export function durationValue(choice: string, customN: number, unit: 'hour' | 'day'): string | null {
  if (choice === 'forever') return 'forever'
  if (choice === 'custom') {
    if (!customN || customN <= 0 || !Number.isFinite(customN)) return null
    return unit === 'day' ? `${Math.round(customN)}d` : `${Math.round(customN)}h`
  }
  return `${choice}` // 分钟数
}

export function durationLabel(choice: string, customN: number, unit: 'hour' | 'day'): string {
  switch (choice) {
    case '60': return '1 小时'
    case '1440': return '24 小时'
    case '10080': return '7 天'
    case 'forever': return '永久'
  }
  return fmtRemain(unit === 'day' ? customN * 1440 : customN * 60)
}

/** 内网/保留地址（与后端 geoip.IsPrivate 一致的 IPv4 部分 + 常见 IPv6）。这些地址不提供封禁按钮。 */
export function isPrivateIp(ip: string): boolean {
  const n = ipToInt(ip)
  if (n == null) return /^(::1$|f[cd]|fe[89ab])/i.test(ip)
  const inNet = (base: string, bits: number) => {
    const b = ipToInt(base)!
    const mask = bits === 0 ? 0 : (~0 << (32 - bits)) >>> 0
    return ((n & mask) >>> 0) === ((b & mask) >>> 0)
  }
  return [['10.0.0.0', 8], ['172.16.0.0', 12], ['192.168.0.0', 16], ['127.0.0.0', 8], ['169.254.0.0', 16], ['100.64.0.0', 10], ['0.0.0.0', 8], ['198.18.0.0', 15]]
    .some(([b, m]) => inNet(b as string, m as number))
}
