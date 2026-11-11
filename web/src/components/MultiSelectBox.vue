<script setup lang="ts">
import { ChevronDown, Search } from 'lucide-vue-next'
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'

interface Option { value: string; label: string; description?: string }
const props = defineProps<{ modelValue: string[]; options: Option[]; ariaLabel: string; placeholder?: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: string[]] }>()
const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)
const search = ref<HTMLInputElement | null>(null)
const popup = ref<HTMLElement | null>(null)
const open = ref(false)
const query = ref('')
const popupStyle = ref<Record<string, string>>({})
const selected = computed(() => props.modelValue.map((value) => props.options.find((o) => o.value === value) ?? { value, label: value }))
const filtered = computed(() => props.options.filter((o) => `${o.label} ${o.description ?? ''}`.toLowerCase().includes(query.value.trim().toLowerCase())))
function updatePosition() {
  const r = trigger.value?.getBoundingClientRect()
  if (!r) return
  const vw = window.innerWidth
  const vv = window.visualViewport
  const vvTop = vv?.offsetTop ?? 0
  const vh = vv?.height ?? window.innerHeight
  const vvBottom = vvTop + vh
  if (document.activeElement !== search.value && (r.bottom < vvTop || r.top > vvBottom)) { close(); return }
  const width = Math.min(320, vw - 16)
  const left = Math.max(8, Math.min(r.left, vw - width - 8))
  const spaceBelow = vvBottom - r.bottom - 12
  const spaceAbove = r.top - vvTop - 12
  if (spaceBelow >= 200 || (spaceBelow >= spaceAbove && spaceBelow >= 150)) {
    popupStyle.value = { width: `${width}px`, left: `${left}px`, top: `${r.bottom + 4}px`, bottom: 'auto', maxHeight: `${Math.min(360, spaceBelow)}px` }
  } else if (spaceAbove >= 180 && document.activeElement !== search.value) {
    popupStyle.value = { width: `${width}px`, left: `${left}px`, top: 'auto', bottom: `${window.innerHeight - r.top + 4}px`, maxHeight: `${Math.min(360, spaceAbove)}px` }
  } else {
    const maxH = Math.max(150, Math.min(320, vh - 16))
    const top = Math.max(vvTop + 8, Math.min(r.bottom + 4, vvBottom - maxH - 8))
    popupStyle.value = { width: `${width}px`, left: `${left}px`, top: `${top}px`, bottom: 'auto', maxHeight: `${maxH}px` }
  }
}
async function show() {
  if (!trigger.value) return
  query.value = ''; open.value = true
  updatePosition()
  await nextTick()
  if (window.matchMedia?.('(pointer: fine)').matches) search.value?.focus({ preventScroll: true })
}
function close() { open.value = false }
function escape(e: KeyboardEvent) {
  if (e.key === 'Escape' && open.value) { e.preventDefault(); e.stopPropagation(); close(); trigger.value?.focus() }
}
function toggle(value: string) {
  emit('update:modelValue', props.modelValue.includes(value) ? props.modelValue.filter((v) => v !== value) : [...props.modelValue, value])
}
function outside(e: Event) { if (!root.value?.contains(e.target as Node)) close() }
function scroll(e: Event) { if (!popup.value?.contains(e.target as Node)) updatePosition() }
function focusOutside(e: FocusEvent) { if (e.relatedTarget && !root.value?.contains(e.relatedTarget as Node)) close() }
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
  <div ref="root" class="multi-select" @keydown="escape" @focusout="focusOutside">
    <button ref="trigger" type="button" class="field multi-trigger" :class="{ open }" :aria-label="`${ariaLabel}，已选 ${modelValue.length} 项`" aria-haspopup="dialog" :aria-expanded="open" :title="selected.map((o) => o.label).join('、')" @click="open ? close() : show()">
      <span v-if="!selected.length" class="multi-placeholder">{{ placeholder ?? '请选择' }}</span>
      <span v-else class="multi-summary"><span v-for="o in selected.slice(0, 1)" :key="o.value" class="multi-token">{{ o.label }}</span><span v-if="selected.length > 1" class="multi-more">+{{ selected.length - 1 }}</span></span>
      <ChevronDown :size="13" class="multi-caret" />
    </button>
    <div v-if="open" ref="popup" class="multi-popup" :style="popupStyle" role="dialog" :aria-label="ariaLabel">
      <div class="multi-search"><Search :size="14" /><input ref="search" v-model="query" :aria-label="`搜索${ariaLabel}`" placeholder="搜索名称" @keydown.enter.prevent @keydown.down.prevent="popup?.querySelector<HTMLInputElement>('input[type=checkbox]')?.focus()"></div>
      <div class="multi-count">已选 {{ modelValue.length }} 项，可多选</div>
      <div class="multi-options">
        <label v-for="o in filtered" :key="o.value" class="multi-option" :class="{ selected: modelValue.includes(o.value) }">
          <input type="checkbox" :checked="modelValue.includes(o.value)" @change="toggle(o.value)">
          <span><span class="multi-label">{{ o.label }}</span><span v-if="o.description" class="multi-description">{{ o.description }}</span></span>
        </label>
        <p v-if="!filtered.length" class="multi-empty">没有匹配项</p>
      </div>
    </div>
  </div>
</template>

<style scoped>
.multi-select { width: 100%; min-width: 0; }
.multi-trigger { display: flex; align-items: center; gap: 5px; width: 100%; height: 34px; padding: 4px 6px; text-align: left; background: var(--elev); cursor: pointer; }
.multi-trigger.open { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }
.multi-placeholder { flex: 1; min-width: 0; color: var(--faint); font-size: 11.5px; }
.multi-summary { display: flex; align-items: center; flex: 1; min-width: 0; gap: 4px; overflow: hidden; }
.multi-token { min-width: 0; max-width: 100%; flex: 0 1 auto; padding: 2px 5px; background: var(--accent-soft); border-radius: 3px; color: var(--accent); font-size: 11px; line-height: 18px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.multi-more { flex: none; color: var(--dim); font-size: 11px; }
.multi-caret { flex: none; color: var(--faint); }
.multi-popup { position: fixed; z-index: 100; display: flex; flex-direction: column; overflow: hidden; background: var(--elev); border: 1px solid var(--line); border-radius: 8px; box-shadow: var(--pop-shadow); padding: 6px; }
.multi-search { display: flex; flex: none; align-items: center; gap: 7px; padding: 0 8px; height: 32px; border: 1px solid var(--line); border-radius: 5px; color: var(--dim); }
.multi-search input { min-width: 0; width: 100%; border: 0; background: transparent; outline: none; color: var(--text); font: inherit; font-size: 12px; }
.multi-search:focus-within { border-color: var(--accent); }
.multi-count { padding: 8px; font-size: 11px; color: var(--dim); }
.multi-options { overflow-y: auto; min-height: 0; }
.multi-option { display: flex; align-items: flex-start; gap: 8px; padding: 8px; border-radius: 4px; cursor: pointer; }
.multi-option:hover { background: var(--hover); }
.multi-option.selected { background: var(--accent-soft); }
.multi-option input { flex: none; margin: 2px 0; accent-color: var(--accent); }
.multi-option > span { min-width: 0; }
.multi-label { display: block; font-size: 12px; color: var(--text); overflow-wrap: anywhere; }
.multi-description { display: block; margin-top: 3px; font-size: 11px; line-height: 1.5; color: var(--dim); overflow-wrap: anywhere; }
.multi-empty { padding: 12px; color: var(--dim); font-size: 12px; }
</style>
