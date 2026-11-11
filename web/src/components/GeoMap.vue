<script setup lang="ts">
import L from 'leaflet'
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'

import world from '@/assets/world-land.json'
import { useUi } from '@/stores/ui'
import { fmtNum } from '@/utils/format'
import { wgsToGcj } from '@/utils/gcj'

export interface MapPoint { name: string; sub?: string; lat: number; lng: number; req: number; ip: number; domestic: boolean }

const props = defineProps<{ kind: 'world' | 'china'; points: MapPoint[] }>()
const ui = useUi()
const el = ref<HTMLElement | null>(null)
const baseMode = ref<'vector' | 'tile'>(props.kind === 'china' ? 'tile' : 'vector')

let map: L.Map | null = null
let tiles: L.TileLayer | null = null
const lands: L.GeoJSON[] = []
let graticule: L.LayerGroup | null = null
let circles: L.CircleMarker[] = []
let halos: L.CircleMarker[] = []
let ro: ResizeObserver | null = null

function palette() {
  return ui.theme !== 'light'
    ? {
        land: '#1e293b',
        landStroke: '#3b4f6b',
        grid: 'rgba(148, 163, 184, 0.10)',
        equator: 'rgba(148, 163, 184, 0.22)',
        style: 4,
        stroke: '#0e131b',
        domestic: '#6366f1',
      }
    : {
        land: '#f8fafc',
        landStroke: '#94a3b8',
        grid: 'rgba(71, 85, 105, 0.10)',
        equator: 'rgba(71, 85, 105, 0.22)',
        style: 9,
        stroke: '#ffffff',
        domestic: '#4f46e5',
      }
}

function amber() {
  return (getComputedStyle(document.documentElement).getPropertyValue('--amber') || '#f59e0b').trim()
}

function esc(s: string) {
  return s.replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]!)
}

function buildGraticule() {
  if (!map) return
  if (graticule) map.removeLayer(graticule)
  const pal = palette()
  const group = L.layerGroup([], { pane: 'land' })
  for (let lat = -60; lat <= 60; lat += 30) {
    const isEq = lat === 0
    L.polyline([[lat, -180], [lat, 180]], {
      pane: 'land',
      interactive: false,
      color: isEq ? pal.equator : pal.grid,
      weight: isEq ? 1.2 : 1,
      dashArray: isEq ? '4 4' : undefined,
    }).addTo(group)
  }
  for (let lng = -150; lng <= 150; lng += 30) {
    L.polyline([[-80, lng], [80, lng]], {
      pane: 'land',
      interactive: false,
      color: pal.grid,
      weight: 1,
    }).addTo(group)
  }
  graticule = group.addTo(map)
}

function syncBaseLayer() {
  if (!map) return
  if (tiles) {
    map.removeLayer(tiles)
    tiles = null
  }
  const pane = map.getPane('land')
  if (baseMode.value === 'tile') {
    if (pane) pane.style.display = 'none'
    // 高德中文矢量瓦片（支持 z=2~18 全球中文标注，国内 CDN 直连无需 API Key）
    tiles = L.tileLayer('https://webrd0{s}.is.autonavi.com/appmaptile?lang=zh_cn&size=1&scale=1&style=7&x={x}&y={y}&z={z}', {
      subdomains: '1234',
      minZoom: 2,
      maxZoom: 18,
      attribution: '&copy; <a href="https://ditu.amap.com/">高德地图</a>',
    }).addTo(map)
  } else {
    if (pane) pane.style.display = ''
  }
}

function frameWorld() {
  if (!map || props.kind !== 'world') return
  const width = map.getSize().x
  if (width < 80) return
  const zoom = Math.max(2, Math.min(3, Math.round(Math.log2(width / 256))))
  map.setView([22, 18], zoom, { animate: false })
}

function resetView() {
  if (!map) return
  if (props.kind === 'china') {
    map.fitBounds([[18.2, 97.5], [45.8, 126.2]], { padding: [32, 32] })
  } else {
    frameWorld()
  }
}

function drawPoints() {
  if (!map) return
  circles.forEach((c) => c.remove())
  halos.forEach((h) => h.remove())
  const pal = palette()
  const width = map.getSize().x
  const max = Math.max(1, ...props.points.map((p) => p.req))
  const pts = [...props.points].sort((a, b) => b.req - a.req)
  const labeled: L.Point[] = []

  halos = []
  circles = pts.map((p, i) => {
    const coord = baseMode.value === 'tile' && p.domestic
      ? wgsToGcj(p.lat, p.lng)
      : ([p.lat, p.lng] as [number, number])
    const xy = map!.latLngToContainerPoint(coord)
    const permanent = i < 12 && labeled.every((q) => q.distanceTo(xy) > 72)
    if (permanent) labeled.push(xy)

    const color = p.domestic ? pal.domestic : amber()
    const radius = props.kind === 'china' ? 5 + 8 * Math.sqrt(p.req / max) : 5 + 13 * Math.sqrt(p.req / max)

    // 外层光晕圈，增强视觉层次
    const halo = L.circleMarker(coord, {
      radius: radius + 5,
      stroke: false,
      fillColor: color,
      fillOpacity: 0.22,
      interactive: false,
    }).addTo(map!)
    halos.push(halo)

    const m = L.circleMarker(coord, {
      radius,
      color: pal.stroke,
      weight: 1.75,
      fillColor: color,
      fillOpacity: 0.95,
    }).addTo(map!)
    ;(m as L.CircleMarker & { _domestic?: boolean })._domestic = p.domestic

    const title = p.sub && p.sub !== p.name ? `${p.name} · ${p.sub}` : p.name
    // 靠近右边界时标签朝左展开，防止“中国 636”被地图右边缘截断
    const dir = xy.x > width - 115 ? 'left' : 'right'
    const offset: [number, number] = dir === 'left' ? [-Math.round(radius + 4), 0] : [Math.round(radius + 4), 0]
    m.bindTooltip(`${esc(title)}  ${fmtNum(p.req)}`, {
      permanent,
      direction: dir,
      offset,
      className: 'map-tip',
      opacity: 1,
    })
    m.bindPopup(`<strong>${esc(title)}</strong><br>${fmtNum(p.req)} 次请求 · ${fmtNum(p.ip)} 个独立 IP`)
    return m
  })
}

function mount() {
  if (!el.value) return
  map = L.map(el.value, {
    scrollWheelZoom: true,
    zoomControl: true,
    attributionControl: true,
    worldCopyJump: true,
    zoomSnap: 1,
    zoomDelta: 1,
    minZoom: props.kind === 'china' ? 4 : 2,
    maxZoom: 18,
  })
  map.zoomControl.setPosition('topright')
  map.createPane('land')
  const pane = map.getPane('land')!
  pane.style.zIndex = '150'
  pane.style.pointerEvents = 'none'

  if (props.kind === 'china') map.fitBounds([[18.2, 97.5], [45.8, 126.2]], { padding: [32, 32] })
  else map.setView([22, 18], 2)

  buildGraticule()
  const pal = palette()
  const style = {
    fillColor: pal.land,
    color: pal.landStroke,
    weight: 1,
    opacity: 0.85,
    fillOpacity: 1,
    lineJoin: 'round' as const,
  }
  for (const f of (world as GeoJSON.FeatureCollection).features) {
    try {
      lands.push(L.geoJSON(f, { interactive: false, style, pane: 'land' }).addTo(map))
    } catch { /* 个别多边形异常时跳过 */ }
  }
  syncBaseLayer()
  requestAnimationFrame(() => {
    map?.invalidateSize()
    frameWorld()
    drawPoints()
  })
  map.on('zoomend', () => {
    drawPoints()
  })
  ro = new ResizeObserver(() => {
    map?.invalidateSize()
    drawPoints()
  })
  ro.observe(el.value)
}

function restyle() {
  if (!map) return
  const pal = palette()
  buildGraticule()
  lands.forEach((l) => l.setStyle({ fillColor: pal.land, color: pal.landStroke }))
  syncBaseLayer()
  drawPoints()
}

onMounted(() => nextTick(mount))
onBeforeUnmount(() => { ro?.disconnect(); map?.remove(); map = null })
watch(() => ui.theme, restyle)
watch(baseMode, () => { syncBaseLayer(); drawPoints() })
watch(() => props.points, drawPoints, { deep: true })
</script>

<template>
  <div class="geo-map-wrap">
    <div ref="el" class="geo-map" role="region" :aria-label="kind === 'world' ? '来源地图' : '国内来源地图'" />
    <div class="map-toolbar">
      <div class="seg map-seg">
        <button type="button" :aria-pressed="baseMode === 'vector'" @click="baseMode = 'vector'">轮廓图</button>
        <button type="button" :aria-pressed="baseMode === 'tile'" @click="baseMode = 'tile'">街景图</button>
      </div>
      <button type="button" class="btn sm map-reset" @click="resetView">复位</button>
    </div>
  </div>
</template>
