<script setup lang="ts">
import { computed } from 'vue'

import { fmtNum } from '@/utils/format'

const props = defineProps<{ values: number[]; labels: string[] }>()
const W = 720, H = 228, pad = { l: 52, r: 18, t: 14, b: 26 }
const iw = W - pad.l - pad.r, ih = H - pad.t - pad.b

const g = computed(() => {
  const values = props.values.length ? props.values : [0]
  const labels = props.labels.length ? props.labels : ['']
  const maxV = Math.max(...values, 1)
  const pow = Math.pow(10, Math.floor(Math.log10(maxV)))
  const n = maxV / pow
  const max = (n <= 1 ? 1 : n <= 2 ? 2 : n <= 5 ? 5 : 10) * pow
  const x = (i: number) => pad.l + iw * (values.length === 1 ? 0.5 : i / (values.length - 1))
  const y = (v: number) => pad.t + ih * (1 - v / max)
  const pts = values.map((v, i) => [x(i), y(v)] as const)
  const d = pts.map((p, i) => `${i ? 'L' : 'M'}${p[0].toFixed(1)},${p[1].toFixed(1)}`).join('')
  const last = pts[pts.length - 1]
  const base = (pad.t + ih).toFixed(1)
  const area = `${d}L${last[0].toFixed(1)},${base}L${pts[0][0].toFixed(1)},${base}Z`
  const grids = [0, 0.5, 1].map((r) => ({ y: y(max * r), label: fmtNum(Math.round(max * r)) }))
  const step = Math.ceil(labels.length / 8)
  const xl = labels.flatMap((lb, i) => {
    if (i % step !== 0) return []
    const isLast = i + step >= labels.length
    return [{
      x: isLast ? Math.min(x(i), W - 4) : x(i), text: lb,
      anchor: i === 0 ? 'start' : isLast ? 'end' : 'middle',
    }]
  })
  return { pts, d, area, grids, xl, values, labels }
})
</script>

<template>
  <svg :viewBox="`0 0 ${W} ${H}`" class="chart" role="img" aria-label="访问趋势">
    <template v-for="(gr, i) in g.grids" :key="'g' + i">
      <line class="grid" :x1="pad.l" :y1="gr.y" :x2="W - pad.r" :y2="gr.y" />
      <text class="ax" :x="pad.l - 6" :y="gr.y + 3" text-anchor="end">{{ gr.label }}</text>
    </template>
    <path class="area" :d="g.area" />
    <path class="line" :d="g.d" />
    <circle v-for="(p, i) in g.pts" :key="'p' + i" class="dot" :cx="p[0]" :cy="p[1]" r="2.5">
      <title>{{ g.labels[i] }} · {{ fmtNum(g.values[i]) }}</title>
    </circle>
    <text v-for="(l, i) in g.xl" :key="'x' + i" class="ax" :x="l.x" :y="H - 6" :text-anchor="l.anchor">{{ l.text }}</text>
  </svg>
</template>
