<script setup lang="ts">
import { Layers, Tag, X } from 'lucide-vue-next'
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'

import { api, errText } from '@/api'
import type { AddressSet, IPDetail } from '@/api/types'
import BanButton from '@/components/BanButton.vue'
import SelectBox from '@/components/SelectBox.vue'
import PagerBar from '@/components/PagerBar.vue'
import StatusTag from '@/components/StatusTag.vue'
import { useUi } from '@/stores/ui'
import { codeCls, flag, fmtNum, fmtShort, methodCls, pct, statusTitle } from '@/utils/format'

const props = defineProps<{ ip: string }>()
const ui = useUi()
const router = useRouter()
const d = ref<IPDetail | null>(null)
const err = ref('')
const editingTag = ref(false)
const tagInput = ref('')
const savingTag = ref(false)
const editingSet = ref(false)
const addressSets = ref<AddressSet[]>([])
const selectedSet = ref('')
const setsLoading = ref(false)
const savingSet = ref(false)
const setError = ref('')
const setOptions = computed(() => addressSets.value.map((a) => ({ value: a.id, label: `${a.name}${a.tag ? ` · ${a.tag}` : ''}` })))
async function showAddressSets() {
  editingSet.value = !editingSet.value; editingTag.value = false
  if (!editingSet.value) return
  const ip = props.ip
  setsLoading.value = true; setError.value = ''; selectedSet.value = ''
  try { const res = await api.addressSetSettings(); if (ip === props.ip) addressSets.value = res.sets }
  catch (e) { if (ip === props.ip) setError.value = errText(e) }
  finally { if (ip === props.ip) setsLoading.value = false }
}
async function addToAddressSet() {
  const ip = props.ip, id = selectedSet.value
  if (!id || savingSet.value) return
  const name = addressSets.value.find((a) => a.id === id)?.name ?? '地址集'
  savingSet.value = true; setError.value = ''
  try {
    const res = await api.addAddressSetIP(id, ip)
    ui.bump()
    if (ip === props.ip) editingSet.value = false
    ui.toast(res.added ? `${ip} 已加入「${name}」` : `${ip} 已在「${name}」或其网段内，无需重复添加`)
  } catch (e) { if (ip === props.ip) setError.value = errText(e) }
  finally { savingSet.value = false }
}
const page = ref(1)
const size = ref(10)

async function load() {
  try {
    const ip = props.ip
    const detail = await api.ip(ip)
    if (ip !== props.ip) return
    d.value = detail
    tagInput.value = d.value.tag ?? ''
    err.value = ''
  } catch (e) {
    err.value = errText(e)
  }
}
watch(() => props.ip, () => { page.value = 1; editingTag.value = false; editingSet.value = false; selectedSet.value = ''; setError.value = ''; setsLoading.value = false })
watch(() => [props.ip, ui.dataVersion], load, { immediate: true })

const recentRows = computed(() => d.value?.recent ?? [])
const totalPages = computed(() => Math.max(1, Math.ceil(recentRows.value.length / size.value)))
const pagedRecent = computed(() => {
  const start = (page.value - 1) * size.value
  return recentRows.value.slice(start, start + size.value)
})

function viewAllInLogs() {
  const ip = props.ip
  ui.closeDrawer()
  router.push({ name: 'logs', query: { q: ip } })
}

async function saveQuickTag(nextTag: string) {
  savingTag.value = true
  try {
    const res = await api.ipTagSettings()
    const items = res.items.filter((x) => x.target !== props.ip)
    const trimmed = nextTag.trim()
    if (trimmed) {
      const prev = res.items.find((x) => x.target === props.ip)
      items.push({ target: props.ip, tag: trimmed, note: prev?.note ?? '' })
    }
    await api.saveIPTags(items)
    editingTag.value = false
    ui.bump()
    ui.toast(trimmed ? `已标记为「${trimmed}」` : '已移除单独标识；如地址集设有标识，将继续继承')
  } catch (e) {
    ui.toast(errText(e), true)
  } finally {
    savingTag.value = false
  }
}
</script>

<template>
  <div class="drawer-overlay" @click.self="ui.closeDrawer()">
    <aside class="drawer" role="dialog" :aria-label="ip">
      <div class="hd">
        <div>
          <h3>访问明细</h3>
          <div class="ip-lg">{{ ip }}</div>
          <div v-if="d" class="place" style="display:flex;align-items:center;gap:6px;flex-wrap:wrap;margin-top:4px">
            <span>{{ flag(d.cc) }} {{ d.geo }}</span>
            <span v-if="d.tag" class="tag ip-tag">{{ d.tag }}</span>
            <StatusTag :level="d.level" :banned="d.banned" />
            <button type="button" class="btn sm ghost" style="height:22px;padding:0 6px;font-size:11.5px" @click="editingTag = !editingTag; editingSet = false">
              <Tag :size="12" />{{ d.tag ? '改标识' : '打标识' }}
            </button>
            <button type="button" class="btn sm ghost drawer-set-action" @click="showAddressSets"><Layers :size="12" />加入地址集</button>
          </div>
          <form v-if="editingSet" class="drawer-set-form" @submit.prevent="addToAddressSet">
            <label class="drawer-set-label">将当前 IP 加入放行地址集</label>
            <p v-if="setsLoading" class="note" role="status">正在加载地址集…</p>
            <div v-else-if="addressSets.length" class="drawer-set-controls"><SelectBox v-model="selectedSet" :options="setOptions" :search-from="0" ariaLabel="目标地址集" block fixed-popup /><button type="submit" class="btn sm primary" :disabled="!selectedSet || savingSet">{{ savingSet ? '加入中…' : '确认加入' }}</button></div>
            <p v-else-if="!setError" class="note">暂无地址集，<RouterLink :to="{ name: 'settings', query: { section: 'address-sets' } }" @click="ui.closeDrawer()">前往创建</RouterLink></p>
            <p v-if="setError" class="drawer-set-error" role="alert">{{ setError }} <button type="button" class="btn sm" @click="editingSet = false; showAddressSets()">重新加载</button></p>
            <p class="drawer-set-help">加入后，引用此集合的域名会放行该 IP。标识继承集合设置，单独标识优先。</p>
          </form>
          <form v-if="editingTag" style="display:flex;gap:6px;align-items:center;margin-top:8px" @submit.prevent="saveQuickTag(tagInput)">
            <input v-model="tagInput" class="field" style="height:28px;width:140px;font-size:12px" maxlength="24" placeholder="如：公司、家里">
            <button type="submit" class="btn sm primary" :disabled="savingTag">保存</button>
            <button v-if="d?.tag" type="button" class="btn sm ghost-danger" :disabled="savingTag" @click="saveQuickTag('')">清除</button>
          </form>
        </div>
        <button type="button" class="x" aria-label="关闭" @click="ui.closeDrawer()"><X class="i" :size="16" /></button>
      </div>
      <div class="bd">
        <p v-if="err" class="note fail-text">{{ err }}</p>
        <template v-if="d">
          <div class="mini-stats">
            <div><em>1 小时</em><b>{{ fmtNum(d.counts['1h']) }}</b></div>
            <div><em>24 小时</em><b>{{ fmtNum(d.counts['24h']) }}</b></div>
            <div><em>7 天</em><b>{{ fmtNum(d.counts['7d']) }}</b></div>
          </div>
          <div class="place">24 小时 4xx/5xx {{ pct(d.err_ratio['24h']) }} · 主要站点 {{ d.host || '—' }}</div>
          <div class="chips"><code v-for="p in d.paths" :key="p">{{ p }}</code></div>
          <table class="data compact fit">
            <colgroup>
              <col style="width:96px">
              <col style="width:48px">
              <col>
              <col style="width:48px">
            </colgroup>
            <thead><tr><th>时间</th><th>方法</th><th>路径</th><th class="num">状态</th></tr></thead>
            <tbody>
              <tr v-for="l in pagedRecent" :key="l.id">
                <td class="mono">{{ fmtShort(l.ts) }}</td>
                <td :class="methodCls(l.method)">{{ l.method }}</td>
                <td><span class="clip mono" :title="l.host + l.path">{{ l.path }}</span></td>
                <td class="num"><span class="code" :class="codeCls(l.status)" :title="statusTitle(l.status)">{{ l.status }}</span></td>
              </tr>
              <tr v-if="!recentRows.length"><td colspan="4"><div class="empty">最近 7 天没有这个地址的访问</div></td></tr>
            </tbody>
          </table>
          <PagerBar
            v-if="recentRows.length"
            v-model:size="size"
            :page="page"
            :pages="totalPages"
            :total="recentRows.length"
            :sizes="[10, 20, 50]"
            @go="(p) => (page = p)"
          />
        </template>
      </div>
      <div class="ft">
        <button type="button" class="btn sm ghost" @click="viewAllInLogs">在访问日志中查看全部 →</button>
        <BanButton :ip="ip" :banned="d?.banned" />
      </div>
    </aside>
  </div>
</template>

<style scoped>
.drawer-set-action { height: 22px; padding: 0 6px; font-size: 11.5px; }
.drawer-set-form { margin-top: 10px; padding: 12px; border: 1px solid var(--line); border-radius: 6px; background: var(--sunken); }
.drawer-set-label { display: block; margin-bottom: 8px; font-size: 12px; font-weight: 600; }
.drawer-set-controls { display: flex; align-items: center; gap: 8px; }
.drawer-set-controls > :first-child { flex: 1; min-width: 0; }
.drawer-set-controls > button { flex: none; }
.drawer-set-help { margin: 8px 0 0; font-size: 11px; line-height: 1.6; color: var(--dim); }
.drawer-set-error { font-size: 12px; color: var(--danger); }
</style>
