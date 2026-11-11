<script setup lang="ts">
import { Check, ChevronDown, Search } from 'lucide-vue-next'
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'

const props = defineProps<{ modelValue: string; tags: string[] }>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)
const popup = ref<HTMLElement | null>(null)
const search = ref<HTMLInputElement | null>(null)
const open = ref(false)
const mode = ref<'include' | 'exclude'>('include')
const query = ref('')
const style = ref<Record<string, string>>({})
const quick = [{ value: '', label: '全部' }, { value: '*', label: '已标识' }, { value: '!*', label: '未标识' }]
const currentTag = computed(() => ['', '*', '!*'].includes(props.modelValue) ? '' : props.modelValue.replace(/^!/, ''))
const label = computed(() => {
  if (!props.modelValue) return '全部标识'
  if (props.modelValue === '*') return '仅看已标识'
  if (props.modelValue === '!*') return '仅看未标识'
  return `${props.modelValue.startsWith('!') ? '排除' : '仅看'}：${currentTag.value}`
})
const filtered = computed(() => [...new Set([...props.tags, ...(currentTag.value ? [currentTag.value] : [])])].filter((tag) => tag.toLowerCase().includes(query.value.trim().toLowerCase())))
function updatePosition() {
  const r = trigger.value?.getBoundingClientRect(); if (!r) return
  const vw = window.innerWidth
  const vv = window.visualViewport
  const vvTop = vv?.offsetTop ?? 0
  const vh = vv?.height ?? window.innerHeight
  const vvBottom = vvTop + vh
  if (document.activeElement !== search.value && (r.bottom < vvTop || r.top > vvBottom)) { close(); return }
  const width = Math.min(300, vw - 16)
  const left = Math.max(8, Math.min(r.left, vw - width - 8))
  const spaceBelow = vvBottom - r.bottom - 12
  const spaceAbove = r.top - vvTop - 12
  if (spaceBelow >= 220 || (spaceBelow >= spaceAbove && spaceBelow >= 160)) {
    style.value = { left: `${left}px`, width: `${width}px`, top: `${r.bottom + 4}px`, bottom: 'auto', maxHeight: `${Math.min(420, spaceBelow)}px` }
  } else if (spaceAbove >= 180 && document.activeElement !== search.value) {
    style.value = { left: `${left}px`, width: `${width}px`, top: 'auto', bottom: `${window.innerHeight - r.top + 4}px`, maxHeight: `${Math.min(420, spaceAbove)}px` }
  } else {
    const maxH = Math.max(160, Math.min(360, vh - 16))
    const top = Math.max(vvTop + 8, Math.min(r.bottom + 4, vvBottom - maxH - 8))
    style.value = { left: `${left}px`, width: `${width}px`, top: `${top}px`, bottom: 'auto', maxHeight: `${maxH}px` }
  }
}
async function show() {
  if (!trigger.value) return
  query.value = ''; mode.value = props.modelValue.startsWith('!') ? 'exclude' : 'include'; open.value = true
  updatePosition()
  await nextTick()
  if (window.matchMedia?.('(pointer: fine)').matches) search.value?.focus({ preventScroll: true })
}
function close() { open.value = false }
function pick(value: string) { emit('update:modelValue', value); close(); trigger.value?.focus() }
function changeMode(value: 'include' | 'exclude') {
  mode.value = value
  if (currentTag.value) emit('update:modelValue', `${value === 'exclude' ? '!' : ''}${currentTag.value}`)
}
function escape(e: KeyboardEvent) { if (e.key === 'Escape' && open.value) { e.preventDefault(); e.stopPropagation(); close(); trigger.value?.focus() } }
function outside(e: Event) { if (!root.value?.contains(e.target as Node)) close() }
function scroll(e: Event) { if (!popup.value?.contains(e.target as Node)) updatePosition() }
function blur(e: FocusEvent) { if (e.relatedTarget && !root.value?.contains(e.relatedTarget as Node)) close() }
function cleanup() {
  document.removeEventListener('pointerdown', outside)
  document.removeEventListener('scroll', scroll, true)
  window.removeEventListener('resize', updatePosition)
  window.visualViewport?.removeEventListener('resize', updatePosition)
  window.visualViewport?.removeEventListener('scroll', updatePosition)
}
watch(open, (v) => {
  cleanup()
  if (v) {
    document.addEventListener('pointerdown', outside)
    document.addEventListener('scroll', scroll, true)
    window.addEventListener('resize', updatePosition)
    window.visualViewport?.addEventListener('resize', updatePosition)
    window.visualViewport?.addEventListener('scroll', updatePosition)
  }
})
onBeforeUnmount(cleanup)
</script>

<template>
  <div ref="root" class="tag-filter" @keydown="escape" @focusout="blur">
    <button ref="trigger" type="button" class="field tag-filter-trigger" :class="{ active: modelValue }" aria-label="筛选 IP 标识" aria-haspopup="dialog" :aria-expanded="open" :title="label" @click="open ? close() : show()"><span>{{ label }}</span><ChevronDown :size="14" /></button>
    <div v-if="open" ref="popup" class="tag-filter-popup" :style="style" role="dialog" aria-label="IP 标识筛选">
      <div class="tag-quick" aria-label="标识范围"><button v-for="o in quick" :key="o.value" type="button" :aria-pressed="modelValue === o.value" @click="pick(o.value)">{{ o.label }}</button></div>
      <div class="tag-filter-mode" aria-label="筛选方式"><button type="button" :aria-pressed="mode === 'include'" @click="changeMode('include')">仅看所选标识</button><button type="button" :aria-pressed="mode === 'exclude'" @click="changeMode('exclude')">排除所选标识</button></div>
      <div class="tag-filter-search"><Search :size="14" /><input ref="search" v-model="query" aria-label="搜索标识名称" placeholder="搜索标识名称" @keydown.enter.prevent="filtered[0] && pick(`${mode === 'exclude' ? '!' : ''}${filtered[0]}`)"></div>
      <div class="tag-filter-list"><button v-for="tag in filtered" :key="tag" type="button" :aria-pressed="tag === currentTag" @click="pick(`${mode === 'exclude' ? '!' : ''}${tag}`)"><span>{{ tag }}</span><Check v-if="tag === currentTag" :size="14" /></button><p v-if="!filtered.length">{{ tags.length ? '没有匹配的标识' : '尚未配置标识' }}</p></div>
    </div>
  </div>
</template>

<style scoped>
.tag-filter { min-width: 132px; max-width: 220px; }
.tag-filter .tag-filter-trigger { display: flex; align-items: center; justify-content: space-between; gap: 8px; width: 100%; min-width: 132px; cursor: pointer; }
.tag-filter-trigger > span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.tag-filter-trigger > svg { flex: none; color: var(--dim); }
.tag-filter-trigger.active { border-color: var(--accent); color: var(--accent); }
.tag-filter-popup { position: fixed; z-index: 100; display: flex; flex-direction: column; padding: 8px; border: 1px solid var(--line); border-radius: 8px; background: var(--elev); box-shadow: var(--pop-shadow); overflow: hidden; }
.tag-quick, .tag-filter-mode { display: flex; gap: 4px; flex: none; }
.tag-quick { padding-bottom: 9px; border-bottom: 1px solid var(--line); }
.tag-quick button, .tag-filter-mode button { flex: 1; padding: 6px 4px; border: 0; background: transparent; border-radius: 4px; color: var(--dim); font: inherit; font-size: 12px; cursor: pointer; }
.tag-quick button[aria-pressed=true], .tag-filter-mode button[aria-pressed=true] { color: var(--accent); background: var(--accent-soft); }
.tag-filter-mode { margin: 8px 0; padding: 3px; background: var(--sunken); border-radius: 5px; }
.tag-filter-search { display: flex; flex: none; align-items: center; gap: 6px; padding: 0 8px; height: 32px; border: 1px solid var(--line); border-radius: 5px; color: var(--dim); }
.tag-filter-search:focus-within { border-color: var(--accent); }
.tag-filter-search input { min-width: 0; width: 100%; font: inherit; font-size: 12px; background: transparent; border: 0; outline: none; color: var(--text); }
.tag-filter-list { margin-top: 6px; min-height: 0; overflow-y: auto; }
.tag-filter-list button { display: flex; align-items: center; justify-content: space-between; gap: 10px; width: 100%; padding: 8px; border: 0; border-radius: 4px; background: transparent; color: var(--text); text-align: left; font: inherit; font-size: 12px; cursor: pointer; }
.tag-filter-list button:hover { background: var(--hover); }
.tag-filter-list button[aria-pressed=true] { background: var(--accent-soft); color: var(--accent); }
.tag-filter-list button > span { overflow-wrap: anywhere; min-width: 0; }
.tag-filter-list svg { flex: none; }
.tag-filter-list p { text-align: center; color: var(--dim); padding: 10px; font-size: 12px; }
</style>
