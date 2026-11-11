<script setup lang="ts">
import { computed, ref } from 'vue'
import { X } from 'lucide-vue-next'
import { isIpOrCidr } from '@/utils/format'

const props = defineProps<{ modelValue: string; id: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const input = ref<HTMLInputElement | null>(null)
const draft = ref('')
const error = ref('')
const feedback = ref('')
const split = (value: string) => value.split(/[\s,;，；]+/).filter(Boolean)
const values = computed(() => split(props.modelValue))

function commit(): boolean {
  const incoming = split(draft.value)
  if (!incoming.length) { draft.value = ''; error.value = ''; return true }
  const invalid = incoming.filter((value) => !isIpOrCidr(value))
  if (invalid.length) {
    error.value = `请填写有效的 IP 或 CIDR：${invalid[0]}`
    return false
  }
  const next = [...values.value]
  for (const value of incoming) {
    if (!next.some((item) => item.toLowerCase() === value.toLowerCase())) next.push(value)
  }
  feedback.value = next.length === values.value.length ? '地址已存在' : `已添加 ${next.length - values.value.length} 个地址`
  emit('update:modelValue', next.join('\n'))
  draft.value = ''
  error.value = ''
  return true
}
function remove(index: number) {
  const next = [...values.value]
  next.splice(index, 1)
  emit('update:modelValue', next.join('\n'))
  input.value?.focus()
}
function keydown(event: KeyboardEvent) {
  if (event.isComposing || event.keyCode === 229) return
  if (event.key === 'Enter') { event.preventDefault(); commit() }
}
function paste(event: ClipboardEvent) {
  const text = event.clipboardData?.getData('text') ?? ''
  if (split(text).length < 2) return
  event.preventDefault()
  const start = input.value?.selectionStart ?? draft.value.length
  const end = input.value?.selectionEnd ?? start
  draft.value = draft.value.slice(0, start) + text + draft.value.slice(end)
  commit()
}
function focus() { input.value?.focus() }
defineExpose({ commit, focus })
</script>

<template>
  <div class="address-editor" :class="{ invalid: error }" @click.self="focus">
    <span v-for="(value, index) in values" :key="`${index}-${value}`" class="address-token">
      <span class="address-value" :title="value">{{ value }}</span>
      <button type="button" :aria-label="`移除地址 ${value}`" @click="remove(index)"><X :size="12" /></button>
    </span>
    <input :id="id" ref="input" v-model="draft" type="text" autocomplete="off" spellcheck="false" aria-label="放行 IP 或网段" :aria-invalid="!!error" :aria-describedby="`${id}-help ${id}-error`" :placeholder="values.length ? '继续添加，回车确认' : '输入 IP / 网段，回车添加'" @keydown="keydown" @paste="paste" @blur="commit" @input="error = ''; feedback = ''">
  </div>
  <p v-if="error" :id="`${id}-error`" class="address-error" role="alert">{{ error }}</p>
  <span class="address-feedback" role="status">{{ feedback }}</span>
</template>

<style scoped>
.address-editor { display: flex; align-items: center; flex-wrap: wrap; gap: 5px; padding: 5px 6px; min-height: 36px; border: 1px solid var(--line); border-radius: 6px; background: var(--elev); cursor: text; }
.address-editor:focus-within { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }
.address-editor.invalid { border-color: var(--danger); }
.address-token { display: inline-flex; align-items: center; gap: 5px; max-width: 100%; padding: 2px 5px 2px 7px; min-height: 23px; border-radius: 4px; background: var(--sunken); border: 1px solid var(--line); color: var(--text); }
.address-value { font-family: var(--mono); font-size: 11.5px; line-height: 1.5; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.address-token button { display: grid; place-items: center; width: 16px; height: 18px; flex-shrink: 0; padding: 0; background: transparent; border: 0; border-radius: 3px; color: var(--dim); }
.address-token button:hover { color: var(--danger); background: var(--danger-soft); }
.address-editor input { flex: 1 1 108px; min-width: 0; width: 108px; max-width: 100%; height: 24px; padding: 0 3px; border: 0; background: transparent; color: var(--text); font: inherit; font-size: 11.5px; outline: none; }
.address-error { margin: 5px 0 0; color: var(--danger); font-size: 11px; line-height: 1.5; overflow-wrap: anywhere; }
.address-feedback { position: fixed; top: 0; left: 0; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); }
</style>
