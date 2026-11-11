import { describe, expect, it } from 'vitest'

import { durationValue, flag, fmtRemain, fmtSize, isIpOrCidr, isPrivateIp, pct, share } from './format'

describe('format', () => {
  it('fmtRemain', () => {
    expect(fmtRemain(42)).toBe('42 分钟')
    expect(fmtRemain(60)).toBe('1 小时')
    expect(fmtRemain(192)).toBe('3 小时 12 分')
    expect(fmtRemain(6 * 1440 + 240)).toBe('6 天 4 小时')
    expect(fmtRemain(10, true)).toBe('永久')
  })
  it('isIpOrCidr', () => {
    for (const ok of ['1.2.3.4', '203.0.113.0/24', '2001:db8::/48', '::1']) expect(isIpOrCidr(ok)).toBe(true)
    for (const bad of ['', '1.2.3', '1.2.3.4/33', '1.2.3.256', 'abc', '1.2.3.4/2/3']) expect(isIpOrCidr(bad)).toBe(false)
  })
  it('isPrivateIp', () => {
    for (const p of ['192.168.1.8', '10.1.2.3', '172.20.0.1', '127.0.0.1', '100.64.1.1', 'fd00::1']) expect(isPrivateIp(p)).toBe(true)
    for (const p of ['8.8.8.8', '172.32.0.1', '192.0.2.214', '2001:db8::1']) expect(isPrivateIp(p)).toBe(false)
  })
  it('misc', () => {
    expect(flag('CN')).toBe('🇨🇳')
    expect(flag('LAN')).toBe('')
    expect(fmtSize(2048)).toBe('2.0 KB')
    expect(pct(0.94)).toBe('94%')
    expect(pct(0.012)).toBe('1.2%')
    expect(share(1, 4)).toBe('25.0%')
    expect(durationValue('custom', 12, 'hour')).toBe('12h')
    expect(durationValue('custom', 0, 'day')).toBeNull()
    expect(durationValue('1440', 0, 'day')).toBe('1440')
  })
})
