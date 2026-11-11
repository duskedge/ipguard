import type { LogRow } from '@/api/types'

export interface PreviewAddress { scheme: 'http' | 'https'; port: number }

/** 只拼接日志域名和相对路径，后端地址不参与；禁止路径改变目标主机或协议。 */
export function imagePreviewUrl(row: Pick<LogRow, 'host' | 'path' | 'method' | 'status'>, address: PreviewAddress): string {
  if (row.method !== 'GET' || row.status < 200 || row.status >= 400) return ''
  if (!/\.png(?:[?#]|$)/i.test(row.path.split('?')[0] ?? '')) return ''
  if (!['http', 'https'].includes(address.scheme) || !Number.isInteger(address.port) || address.port < 1 || address.port > 65535) return ''
  if (!row.host || /[\s/@\\?#]/.test(row.host) || !row.path.startsWith('/') || row.path.startsWith('//') || /[\\\u0000-\u001f]/.test(row.path)) return ''
  try {
    const base = new URL(`${address.scheme}://${row.host}`)
    const explicitPort = row.host.match(/(?:\]|[^:]):(\d+)$/)?.[1]
    if (explicitPort && (Number(explicitPort) < 1 || Number(explicitPort) > 65535)) return ''
    if (!explicitPort) base.port = String(address.port)
    const url = new URL(row.path, base)
    return url.origin === base.origin ? url.href : ''
  } catch { return '' }
}
