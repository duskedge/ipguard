<script setup lang="ts">
import { X } from 'lucide-vue-next'
import { computed, onMounted, ref } from 'vue'

import { api, errText } from '@/api'
import type { Ban } from '@/api/types'
import { useUi } from '@/stores/ui'
import { flag, fmtRemain } from '@/utils/format'

// 从封禁列表进来带 ban；从 IP 行进来只有 ip，需要先查出覆盖它的那条决策（可能是网段）
const props = defineProps<{ ban: Ban | null; ip: string | null }>()
const ui = useUi()
const target = ref<Ban | null>(props.ban)
const loading = ref(!props.ban)
const busy = ref(false)
const missing = ref(false)

const SRC: Record<string, string> = { manual: '手动', auto: '自动', community: '社区' }
const coversRange = computed(() => !!props.ip && target.value?.scope === 'range')

onMounted(async () => {
  if (props.ban || !props.ip) return
  try {
    const r = await api.bans({ q: props.ip, source: 'all', size: 5 })
    target.value = r.rows.find((b) => b.target === props.ip) ?? r.rows[0] ?? null
    missing.value = !target.value
  } catch (e) {
    ui.toast(errText(e), true)
  } finally {
    loading.value = false
  }
})

async function confirm() {
  if (!target.value) return
  busy.value = true
  try {
    await api.unban({ id: target.value.id })
    ui.toast('已解封 ' + target.value.target)
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
    <div class="modal" role="dialog" aria-modal="true" aria-labelledby="unban-title">
      <div class="hd">
        <h3 id="unban-title">解封这个地址</h3>
        <button type="button" class="x" aria-label="关闭" @click="ui.closeModal()"><X class="i" :size="16" /></button>
      </div>
      <div class="bd">
        <p v-if="loading" class="note">正在查询封禁记录…</p>
        <p v-else-if="missing" class="note">没有找到 {{ ip }} 的封禁记录，可能已经过期。</p>
        <template v-else-if="target">
          <div class="ip-lg">{{ target.target }}</div>
          <div class="place">{{ flag(target.cc) }} {{ target.geo }} · {{ SRC[target.source] ?? target.source }}</div>
          <p class="note">{{ target.scenario }} · 剩余 {{ fmtRemain(target.remain_min, target.permanent) }}</p>
          <p v-if="coversRange" class="note fail-text">{{ ip }} 是被这个网段封禁的，解封会放行整个网段。</p>
          <p class="note">会删除 CrowdSec 上这条决策，路由器入口不再丢弃该地址。</p>
          <p v-if="target.source === 'community'" class="note">社区黑名单条目在下次同步时可能会被重新下发。</p>
        </template>
      </div>
      <div class="ft">
        <button type="button" class="btn" @click="ui.closeModal()">取消</button>
        <button type="button" class="btn danger" :disabled="busy || !target" @click="confirm">{{ busy ? '提交中…' : '确认解封' }}</button>
      </div>
    </div>
  </div>
</template>
