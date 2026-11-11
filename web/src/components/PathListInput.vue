<script setup lang="ts">
// 敏感路径片段编辑器：标签式增删，回车 / 逗号 / 粘贴多行批量添加。
import { ref } from 'vue'
import { X } from 'lucide-vue-next'

const props = defineProps<{ modelValue: string[]; id: string; defaults?: string[] }>()
const emit = defineEmits<{ 'update:modelValue': [value: string[]] }>()
const input = ref<HTMLInputElement | null>(null)
const draft = ref('')
const error = ref('')
const feedback = ref('')
const split = (value: string) => value.split(/[\s,;，；]+/).map((v) => v.trim().toLowerCase()).filter(Boolean)
const isDefault = (value: string) => !props.defaults || props.defaults.includes(value)

function commit(): boolean {
  const incoming = split(draft.value)
  if (!incoming.length) { draft.value = ''; error.value = ''; return true }
  const bad = incoming.find((v) => v.length < 2 || v.length > 128)
  if (bad) {
    error.value = `每条须在 2～128 个字符之间：${bad}`
    return false
  }
  const next = [...props.modelValue]
  for (const v of incoming) if (!next.includes(v)) next.push(v)
  feedback.value = next.length === props.modelValue.length ? '已存在' : `已添加 ${next.length - props.modelValue.length} 条`
  emit('update:modelValue', next)
  draft.value = ''
  error.value = ''
  return true
}
function remove(index: number) {
  const next = [...props.modelValue]
  next.splice(index, 1)
  emit('update:modelValue', next)
  input.value?.focus()
}
function keydown(event: KeyboardEvent) {
  if (event.isComposing || event.keyCode === 229) return
  if (event.key === 'Enter' || event.key === ',') { event.preventDefault(); commit() }
  else if (event.key === 'Backspace' && !draft.value && props.modelValue.length) remove(props.modelValue.length - 1)
}
function paste(event: ClipboardEvent) {
  const text = event.clipboardData?.getData('text') ?? ''
  if (split(text).length < 2) return
  event.preventDefault()
  draft.value = text
  commit()
}
function focus() { input.value?.focus() }
defineExpose({ commit, focus })
</script>

<template>
  <div class="path-editor" :class="{ invalid: error }" @click.self="focus">
    <span v-for="(value, index) in modelValue" :key="value" class="path-token" :class="{ added: !isDefault(value) }" :title="isDefault(value) ? value : `${value}（自定义）`">
      <span class="path-value">{{ value }}</span>
      <button type="button" :aria-label="`移除 ${value}`" @click="remove(index)"><X :size="12" /></button>
    </span>
    <input :id="id" ref="input" v-model="draft" type="text" autocomplete="off" spellcheck="false" aria-label="添加敏感路径片段" :aria-invalid="!!error" :placeholder="modelValue.length ? '继续添加，回车确认' : '输入路径片段，如 /.env，回车添加'" @keydown="keydown" @paste="paste" @blur="commit" @input="error = ''; feedback = ''">
  </div>
  <p v-if="error" class="path-error" role="alert">{{ error }}</p>
  <span class="path-feedback" role="status">{{ feedback }}</span>
</template>

<style scoped>
.path-editor { display: flex; align-items: center; flex-wrap: wrap; gap: 5px; padding: 6px; min-height: 36px; max-height: 260px; overflow-y: auto; border: 1px solid var(--line); border-radius: 6px; background: var(--elev); cursor: text; }
.path-editor:focus-within { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }
.path-editor.invalid { border-color: var(--danger); }
.path-token { display: inline-flex; align-items: center; gap: 4px; max-width: 100%; padding: 2px 4px 2px 7px; min-height: 23px; border-radius: 4px; background: var(--sunken); border: 1px solid var(--line); color: var(--text); }
.path-token.added { border-color: var(--accent); background: var(--accent-soft); }
.path-value { font-family: var(--mono); font-size: 11.5px; line-height: 1.5; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.path-token button { display: grid; place-items: center; width: 16px; height: 18px; flex-shrink: 0; padding: 0; background: transparent; border: 0; border-radius: 3px; color: var(--dim); }
.path-token button:hover { color: var(--danger); background: var(--danger-soft); }
.path-editor input { flex: 1 1 160px; min-width: 0; width: 160px; height: 24px; padding: 0 3px; border: 0; background: transparent; color: var(--text); font: inherit; font-family: var(--mono); font-size: 11.5px; outline: none; }
.path-error { margin: 5px 0 0; color: var(--danger); font-size: 11px; line-height: 1.5; overflow-wrap: anywhere; }
.path-feedback { position: fixed; top: 0; left: 0; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); }
</style>
