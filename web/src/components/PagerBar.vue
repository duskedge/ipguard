<script setup lang="ts">
import { computed } from 'vue'

import { fmtNum } from '@/utils/format'

const props = withDefaults(defineProps<{
  page: number
  pages: number
  total: number
  size: number
  sizes?: number[]
}>(), {
  sizes: () => [10, 20, 50, 100],
})
const emit = defineEmits<{
  go: [p: number]
  'update:size': [s: number]
}>()

const from = computed(() => (props.total ? (props.page - 1) * props.size + 1 : 0))
const to = computed(() => Math.min(props.page * props.size, props.total))

const sizeList = computed(() => {
  const set = new Set(props.sizes)
  set.add(props.size)
  return Array.from(set).sort((a, b) => a - b)
})

function onSizeChange(e: Event) {
  const val = Number((e.target as HTMLSelectElement).value)
  if (val > 0 && val !== props.size) {
    emit('update:size', val)
    emit('go', 1)
  }
}

// 页数可能上千（社区黑名单），只显示首尾与当前页附近
const items = computed<(number | '…')[]>(() => {
  const { page, pages } = props
  if (pages <= 9) return Array.from({ length: pages }, (_, i) => i + 1)
  const out: (number | '…')[] = [1]
  const lo = Math.max(2, page - 2), hi = Math.min(pages - 1, page + 2)
  if (lo > 2) out.push('…')
  for (let i = lo; i <= hi; i++) out.push(i)
  if (hi < pages - 1) out.push('…')
  out.push(pages)
  return out
})
</script>

<template>
  <div v-if="total" class="pager">
    <div class="pager-meta">
      <span>第 {{ fmtNum(from) }}–{{ fmtNum(to) }} 条，共 {{ fmtNum(total) }} 条</span>
      <label class="pager-size">
        <select :value="size" class="pager-select" aria-label="每页显示条数" @change="onSizeChange">
          <option v-for="s in sizeList" :key="s" :value="s">{{ s }} 条/页</option>
        </select>
      </label>
    </div>
    <div class="pager-btns">
      <button type="button" class="btn sm ghost" :disabled="page <= 1" @click="emit('go', page - 1)">上一页</button>
      <template v-for="(it, i) in items" :key="i">
        <span v-if="it === '…'" class="btn sm ghost" style="border-color:transparent;cursor:default">…</span>
        <button
          v-else type="button" class="btn sm" :class="it === page ? 'primary' : 'ghost'"
          :aria-current="it === page ? 'page' : 'false'" @click="emit('go', it)"
        >{{ it }}</button>
      </template>
      <button type="button" class="btn sm ghost" :disabled="page >= pages" @click="emit('go', page + 1)">下一页</button>
    </div>
  </div>
</template>
