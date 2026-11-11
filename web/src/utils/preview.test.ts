import { describe, expect, it } from 'vitest'
import { imagePreviewUrl } from './preview'

const row = { host: 'm.example.com', path: '/image/a.png', method: 'GET', status: 200 }
const address = { scheme: 'https' as const, port: 4443 }
describe('图片预览链接', () => {
  it('使用配置的对外端口，保留路径和查询参数', () => {
    expect(imagePreviewUrl(row, address)).toBe('https://m.example.com:4443/image/a.png')
    expect(imagePreviewUrl({ ...row, path: '/image/A.PNG?v=2' }, { scheme: 'http', port: 8080 })).toBe('http://m.example.com:8080/image/A.PNG?v=2')
    expect(imagePreviewUrl({ ...row, host: 'm.example.com:443' }, address)).toBe('https://m.example.com/image/a.png')
    expect(imagePreviewUrl({ ...row, host: 'm.example.com:8443' }, address)).toBe('https://m.example.com:8443/image/a.png')
  })
  it('路径不能改成其他主机、协议或注入凭据', () => {
    for (const path of ['//evil.test/a.png', '/\\evil.test/a.png', 'https://evil.test/a.png', '/image/a.png\n']) {
      expect(imagePreviewUrl({ ...row, path }, address)).toBe('')
    }
    for (const host of ['user@evil.test', 'm.example.com/evil', 'm.example.com:0', 'm.example.com:65536']) {
      expect(imagePreviewUrl({ ...row, host }, address)).toBe('')
    }
  })
  it('仅为 PNG 图片的成功读取提供预览', () => {
    expect(imagePreviewUrl({ ...row, path: '/api?file=a.png' }, address)).toBe('')
    expect(imagePreviewUrl({ ...row, path: '/a.svg' }, address)).toBe('')
    expect(imagePreviewUrl({ ...row, method: 'DELETE' }, address)).toBe('')
    expect(imagePreviewUrl({ ...row, status: 403 }, address)).toBe('')
  })
})
