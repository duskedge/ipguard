<script setup lang="ts">
import { computed } from 'vue'

import type { GeoResult } from '@/api/types'
import { fmtNum } from '@/utils/format'

const props = defineProps<{ geo: GeoResult }>()
const r = 52, c = 2 * Math.PI * r, gap = 2.5

const parts = computed(() => {
  const g = props.geo
  const list = [
    { cls: 'cn', name: '国内', value: g.domestic_req },
    { cls: 'os', name: '国外', value: g.overseas_req },
    { cls: 'lan', name: '内网', value: g.lan_req },
  ]
  if (g.unknown_req) list.push({ cls: 'unk', name: '未知', value: g.unknown_req })
  return list
})
const total = computed(() => parts.value.reduce((s, p) => s + p.value, 0))
const arcs = computed(() => {
  let off = 0
  return parts.value.map((p) => {
    const full = total.value ? (c * p.value) / total.value : 0
    const len = Math.max(0, full - (p.value ? gap : 0))
    const a = { cls: p.cls, dash: `${len.toFixed(2)} ${(c - len).toFixed(2)}`, off: (-off).toFixed(2) }
    off += full
    return a
  })
})
const p0 = computed(() => (total.value ? ((props.geo.domestic_req / total.value) * 100).toFixed(1) : '0.0'))
</script>

<template>
  <div class="donut-wrap">
    <svg viewBox="0 0 156 156" class="donut" role="img" :aria-label="`国内请求占 ${p0}%`">
      <circle r="52" cx="78" cy="78" fill="none" stroke="var(--sunken)" stroke-width="13" />
      <g transform="rotate(-90 78 78)">
        <circle
          v-for="a in arcs" :key="a.cls" class="seg" :class="a.cls" r="52" cx="78" cy="78"
          :stroke-dasharray="a.dash" :stroke-dashoffset="a.off"
        />
      </g>
      <text class="donut-n" x="78" y="74" text-anchor="middle">{{ p0 }}%</text>
      <text class="donut-s" x="78" y="92" text-anchor="middle">国内</text>
    </svg>
    <div class="legend">
      <div v-for="p in parts" :key="p.cls" class="row">
        <i class="sw" :class="p.cls" /><span>{{ p.name }}</span><b>{{ fmtNum(p.value) }}</b>
      </div>
    </div>
  </div>
</template>
