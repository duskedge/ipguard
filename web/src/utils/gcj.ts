// WGS-84 → GCJ-02。腾讯底图是 GCJ-02 坐标系，不转换的话国内的点会偏几百米。
export function wgsToGcj(lat: number, lng: number): [number, number] {
  if (lng < 72.004 || lng > 137.8347 || lat < 17.5 || lat > 55.8271) return [lat, lng]
  const a = 6378245.0, ee = 0.00669342162296594323
  const tLat = (x: number, y: number) => {
    let r = -100 + 2 * x + 3 * y + 0.2 * y * y + 0.1 * x * y + 0.2 * Math.sqrt(Math.abs(x))
    r += ((20 * Math.sin(6 * x * Math.PI) + 20 * Math.sin(2 * x * Math.PI)) * 2) / 3
    r += ((20 * Math.sin(y * Math.PI) + 40 * Math.sin((y / 3) * Math.PI)) * 2) / 3
    r += ((160 * Math.sin((y / 12) * Math.PI) + 320 * Math.sin((y * Math.PI) / 30)) * 2) / 3
    return r
  }
  const tLng = (x: number, y: number) => {
    let r = 300 + x + 2 * y + 0.1 * x * x + 0.1 * x * y + 0.1 * Math.sqrt(Math.abs(x))
    r += ((20 * Math.sin(6 * x * Math.PI) + 20 * Math.sin(2 * x * Math.PI)) * 2) / 3
    r += ((20 * Math.sin(x * Math.PI) + 40 * Math.sin((x / 3) * Math.PI)) * 2) / 3
    r += ((150 * Math.sin((x / 12) * Math.PI) + 300 * Math.sin((x / 30) * Math.PI)) * 2) / 3
    return r
  }
  let dLat = tLat(lng - 105, lat - 35)
  let dLng = tLng(lng - 105, lat - 35)
  const radLat = (lat / 180) * Math.PI
  let magic = Math.sin(radLat)
  magic = 1 - ee * magic * magic
  const sq = Math.sqrt(magic)
  dLat = (dLat * 180) / (((a * (1 - ee)) / (magic * sq)) * Math.PI)
  dLng = (dLng * 180) / ((a / sq) * Math.cos(radLat) * Math.PI)
  return [lat + dLat, lng + dLng]
}
