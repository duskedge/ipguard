<script setup lang="ts">
// 统一样式的下拉框，替代原生 <select>（原生弹层跟随系统样式，深浅主题下都不协调）。
// 选项多时自带搜索；支持 ↑↓ / Enter / Esc / 输入字母过滤。
import { Check, ChevronDown, Search } from 'lucide-vue-next'
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'

export interface SelectOption { value: string; label: string }

const props = withDefaults(defineProps<{
  modelValue: string
  options: SelectOption[]
  ariaLabel?: string
  /** 选项数超过这个值时显示搜索框 */
  searchFrom?: number
  minWidth?: string
  block?: boolean
  /** 表格滚动容器内使用视口定位，避免弹层被裁切。 */
  fixedPopup?: boolean
}>(), { searchFrom: 10, minWidth: '', block: false, fixedPopup: false })
const emit = defineEmits<{ 'update:modelValue': [v: string] }>()

const root = ref<HTMLElement | null>(null)
const listEl = ref<HTMLElement | null>(null)
const searchEl = ref<HTMLInputElement | null>(null)
const open = ref(false)
const popupStyle = ref<Record<string, string>>({})
const q = ref('')
const active = ref(0)
const dropUp = ref(false)
const alignRight = ref(false)

const current = computed(() => props.options.find((o) => o.value === props.modelValue))
const searchable = computed(() => props.options.length > props.searchFrom)
const filtered = computed(() => {
  const k = q.value.trim().toLowerCase()
  return k ? props.options.filter((o) => o.label.toLowerCase().includes(k)) : props.options
})

function toggle() { open.value ? close() : show() }
async function show() {
  q.value = ''
  open.value = true
  active.value = Math.max(0, props.options.findIndex((o) => o.value === props.modelValue))
  // 下方空间不够就往上弹；靠右时右对齐避免手机屏幕右侧溢出
  const r = root.value?.getBoundingClientRect()
  dropUp.value = !!r && window.innerHeight - r.bottom < 300 && r.top > window.innerHeight - r.bottom
  alignRight.value = !!r && r.left > window.innerWidth / 2
  if (props.fixedPopup && r) popupStyle.value = {
    position: 'fixed', left: `${Math.max(8, Math.min(r.left, window.innerWidth - Math.min(r.width, window.innerWidth - 16) - 8))}px`, right: 'auto',
    width: `${Math.min(r.width, window.innerWidth - 16)}px`, minWidth: '0',
    top: dropUp.value ? 'auto' : `${r.bottom + 4}px`,
    bottom: dropUp.value ? `${window.innerHeight - r.top + 4}px` : 'auto',
  }
  await nextTick()
  if (searchable.value && window.matchMedia?.('(pointer: fine)').matches) searchEl.value?.focus({ preventScroll: true })
  scrollToActive()
}
function close() { open.value = false }
function pick(o: SelectOption) {
  emit('update:modelValue', o.value)
  close()
  root.value?.querySelector<HTMLButtonElement>('.sel-btn')?.focus()
}

function scrollToActive() {
  nextTick(() => listEl.value?.querySelector<HTMLElement>(`[data-i="${active.value}"]`)?.scrollIntoView({ block: 'nearest' }))
}
function onKey(e: KeyboardEvent) {
  if (!open.value) {
    if (['ArrowDown', 'ArrowUp', 'Enter', ' '].includes(e.key)) { e.preventDefault(); show() }
    return
  }
  if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); close(); return }
  if (e.key === 'ArrowDown') { e.preventDefault(); active.value = Math.min(filtered.value.length - 1, active.value + 1); scrollToActive() }
  if (e.key === 'ArrowUp') { e.preventDefault(); active.value = Math.max(0, active.value - 1); scrollToActive() }
  if (e.key === 'Enter') { e.preventDefault(); const o = filtered.value[active.value]; if (o) pick(o) }
  if (e.key === 'Tab') close()
}
watch(q, () => { active.value = 0 })

function onDocDown(e: Event) {
  if (open.value && root.value && !root.value.contains(e.target as Node)) close()
}
function onViewportChange(e: Event) {
  if (listEl.value?.contains(e.target as Node)) return
  close()
}
watch(open, (v) => {
  if (props.fixedPopup) {
    if (v) { document.addEventListener('scroll', onViewportChange, true); window.addEventListener('resize', close) }
    else { document.removeEventListener('scroll', onViewportChange, true); window.removeEventListener('resize', close) }
  }
  if (v) document.addEventListener('pointerdown', onDocDown)
  else document.removeEventListener('pointerdown', onDocDown)
})
onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onDocDown)
  document.removeEventListener('scroll', onViewportChange, true)
  window.removeEventListener('resize', close)
})
</script>

<template>
  <div ref="root" class="sel" :class="{ open, block }" :style="minWidth ? { minWidth } : undefined" @keydown="onKey">
    <button
      type="button" class="field sel-btn" :aria-label="ariaLabel" aria-haspopup="listbox" :aria-expanded="open"
      @click="toggle"
    >
      <span class="sel-text">{{ current?.label ?? '请选择' }}</span>
      <ChevronDown class="sel-caret" :size="14" />
    </button>
    <div v-if="open" class="sel-pop" :class="{ up: dropUp, right: alignRight }" :style="fixedPopup ? popupStyle : undefined">
      <div v-if="searchable" class="sel-search">
        <Search :size="14" />
        <input ref="searchEl" v-model="q" placeholder="搜索" aria-label="搜索选项">
      </div>
      <ul ref="listEl" class="sel-list" role="listbox" :aria-label="ariaLabel">
        <li
          v-for="(o, i) in filtered" :key="o.value" :data-i="i" role="option" :aria-selected="o.value === modelValue"
          class="sel-opt" :class="{ active: i === active, selected: o.value === modelValue }"
          @mouseenter="active = i" @mousedown.prevent @click="pick(o)"
        >
          <span class="sel-label" :title="o.label">{{ o.label }}</span>
          <Check v-if="o.value === modelValue" :size="14" />
        </li>
        <li v-if="!filtered.length" class="sel-empty">没有匹配项</li>
      </ul>
    </div>
  </div>
</template>

<style scoped>
.sel { position: relative; display: inline-block; }
.sel.block { display: block; width: 100%; }
.sel-btn {
  display: flex; align-items: center; gap: 8px; width: 100%;
  text-align: left; cursor: pointer;
}
.filters .sel-btn { background: var(--elev); }
.sel-btn:hover { border-color: var(--line-strong); }
.sel.open .sel-btn { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }
.sel-text { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.sel-caret { flex: none; color: var(--faint); transition: transform .15s; }
.sel.open .sel-caret { transform: rotate(180deg); }
.sel-pop {
  position: absolute; left: 0; top: calc(100% + 4px); z-index: 50;
  min-width: 100%; max-width: min(360px, 90vw); width: max-content;
  background: var(--elev); border: 1px solid var(--line); border-radius: 8px;
  box-shadow: var(--pop-shadow); padding: 4px;
}
.sel-pop.up { top: auto; bottom: calc(100% + 4px); }
.sel-pop.right { left: auto; right: 0; }
.sel-search {
  display: flex; align-items: center; gap: 6px; margin: 2px 2px 4px;
  padding: 0 8px; height: 30px; border-radius: 6px;
  background: var(--sunken); border: 1px solid var(--line); color: var(--faint);
}
.sel-search input { flex: 1; min-width: 0; border: 0; outline: none; background: transparent; font-size: 12.5px; color: var(--text); }
.sel-list { list-style: none; margin: 0; padding: 0; max-height: 280px; overflow: auto; }
.sel-opt {
  display: flex; align-items: center; gap: 8px; justify-content: space-between;
  height: 30px; padding: 0 8px; border-radius: 6px; cursor: pointer;
  font-size: 13px; color: var(--text); white-space: nowrap;
}
.sel-opt.active { background: var(--hover); }
.sel-opt.selected { color: var(--accent-text); font-weight: 560; }
.sel-opt.selected.active { background: var(--accent-soft); }
.sel-label { overflow: hidden; text-overflow: ellipsis; }
.sel-empty { padding: 8px; color: var(--faint); font-size: 12.5px; }
</style>
