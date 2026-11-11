<script setup lang="ts">
import { X } from 'lucide-vue-next'
import { computed, onMounted, ref, watch } from 'vue'

import { api, errText } from '@/api'
import SelectBox from '@/components/SelectBox.vue'
import { useUi } from '@/stores/ui'
import { durationLabel, durationValue, flag, isIpOrCidr } from '@/utils/format'

// target 为空 = 手动封禁（可编辑地址，支持 CIDR）
const props = defineProps<{ target: string | null }>()
const ui = useUi()

const editable = computed(() => !props.target)
const addr = ref(props.target ?? '')
const dur = ref('1440')
const customN = ref(12)
const unit = ref<'hour' | 'day'>('hour')
const reason = ref('manual-ipguard')
const busy = ref(false)
const geo = ref<{ geo: string; cc: string; banned: boolean } | null>(null)
const paths = ref<string[]>([])
const durEl = ref<HTMLElement | null>(null)
const addrEl = ref<HTMLInputElement | null>(null)

const DURATIONS = [
  { value: '60', label: '1 小时' }, { value: '1440', label: '24 小时' }, { value: '10080', label: '7 天' },
  { value: 'forever', label: '永久' }, { value: 'custom', label: '自定义' },
]
const UNITS = [{ value: 'hour', label: '小时' }, { value: 'day', label: '天' }]
const valid = computed(() => isIpOrCidr(addr.value))
const hint = computed(() => {
  const v = addr.value.trim()
  if (!v) return { text: '', bad: false }
  if (!valid.value) return { text: '格式应为 IPv4 / IPv6 或 CIDR', bad: true }
  if (!geo.value) return { text: '', bad: false }
  if (geo.value.banned) return { text: `${flag(geo.value.cc)} ${geo.value.geo} · 已在封禁中`, bad: true }
  return { text: geo.value.geo === '未知' ? '归属地未知，仍可封禁' : `${flag(geo.value.cc)} ${geo.value.geo}`, bad: false }
})

let lookupSeq = 0
async function lookup() {
  const v = addr.value.trim()
  geo.value = null
  if (!valid.value) return
  const my = ++lookupSeq
  try {
    const g = await api.lookup(v.split('/')[0])
    if (my === lookupSeq) geo.value = g
  } catch { /* 归属地只是提示 */ }
}
watch(addr, lookup)

onMounted(async () => {
  if (editable.value) addrEl.value?.focus()
  else durEl.value?.querySelector<HTMLButtonElement>('button')?.focus()
  await lookup()
  if (props.target) {
    try { paths.value = (await api.ip(props.target)).paths.slice(0, 4) } catch { /* ignore */ }
  }
})

async function submit() {
  const target = addr.value.trim()
  if (!isIpOrCidr(target)) return
  const d = durationValue(dur.value, customN.value, unit.value)
  if (!d) { ui.toast('请填写大于 0 的时长', true); return }
  busy.value = true
  try {
    const r = await api.ban(target, d, reason.value.trim() || 'manual-ipguard')
    ui.toast(`已封禁 ${r.target}，${durationLabel(dur.value, customN.value, unit.value)}`)
    ui.closeModal()
    ui.closeDrawer()
    ui.bump()
  } catch (e) {
    ui.toast(errText(e), true)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="overlay" @click.self="ui.closeModal()">
    <form class="modal" role="dialog" aria-modal="true" aria-labelledby="ban-title" @submit.prevent="submit">
      <div class="hd">
        <h3 id="ban-title">{{ editable ? '手动封禁' : '封禁这个地址' }}</h3>
        <button type="button" class="x" aria-label="关闭" @click="ui.closeModal()"><X class="i" :size="16" /></button>
      </div>
      <div class="bd">
        <template v-if="editable">
          <label class="lbl" for="ban-target">IP 或网段</label>
          <input id="ban-target" ref="addrEl" v-model="addr" class="field" placeholder="例如 192.0.2.214 或 203.0.113.0/24" autocomplete="off">
          <p class="hint" :class="{ bad: hint.bad }">{{ hint.text }}</p>
        </template>
        <template v-else>
          <div class="ip-lg">{{ addr }}</div>
          <div class="place">{{ geo ? `${flag(geo.cc)} ${geo.geo}` : '…' }}</div>
          <p v-if="paths.length" class="note">最近路径：{{ paths.join('、') }}</p>
          <p class="note">写入 CrowdSec 决策后，bouncer 会同步到 MikroTik 的 address-list，WAN 入口丢弃。</p>
        </template>
        <label class="lbl">时长</label>
        <div ref="durEl"><SelectBox v-model="dur" :options="DURATIONS" aria-label="时长" block /></div>
        <div v-if="dur === 'custom'" class="custom-row">
          <input v-model.number="customN" class="field" type="number" min="1" aria-label="自定义时长">
          <SelectBox v-model="unit" :options="UNITS" aria-label="单位" min-width="100px" />
        </div>
        <label class="lbl" for="ban-reason">原因</label>
        <input id="ban-reason" v-model="reason" class="field" maxlength="200">
        <p v-if="editable" class="note">写入 CrowdSec 决策后，bouncer 会同步到 MikroTik 的 address-list。</p>
      </div>
      <div class="ft">
        <button type="button" class="btn" @click="ui.closeModal()">取消</button>
        <button type="submit" class="btn danger" :disabled="busy || !valid">{{ busy ? '提交中…' : '确认封禁' }}</button>
      </div>
    </form>
  </div>
</template>
