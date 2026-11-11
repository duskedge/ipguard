<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useRoute } from 'vue-router'

import { api } from '@/api'
import type { LogRange } from '@/api/types'
import LogPathCell from '@/components/LogPathCell.vue'
import GuardLogs from '@/components/GuardLogs.vue'
import SegControl from '@/components/SegControl.vue'
import BanButton from '@/components/BanButton.vue'
import IpCell from '@/components/IpCell.vue'
import PagerBar from '@/components/PagerBar.vue'
import RefreshButton from '@/components/RefreshButton.vue'
import SelectBox from '@/components/SelectBox.vue'
import TagFilter from '@/components/TagFilter.vue'
import SortTh from '@/components/SortTh.vue'
import { useUi } from '@/stores/ui'
import { codeCls, fmtShort, fmtSize, methodCls, statusTitle } from '@/utils/format'
import { useDebounced, useLoader } from '@/utils/poll'

const route = useRoute()
const ui = useUi()
const logTab = computed(() => route.query.tab === 'guard' ? 'guard' : 'access')
const f = reactive({
  range: (typeof route.query.range === 'string' ? route.query.range : '24h') as LogRange,
  q: typeof route.query.q === 'string' ? route.query.q : '',
  tag: '',
  host: typeof route.query.host === 'string' ? route.query.host : '',
  status: typeof route.query.status === 'string' ? route.query.status : '',
  path: typeof route.query.path === 'string' ? route.query.path : '',
  country: '',
  sort: 'ts', dir: 'desc' as 'asc' | 'desc', page: 1, size: 20,
})
const q = useDebounced(() => f.q)
const path = useDebounced(() => f.path)
const uaOpen = ref<string | null>(null)

watch(() => route.query.q, (v) => {
  if (typeof v === 'string') f.q = v
})
watch(() => [route.query.host, route.query.path, route.query.status, route.query.range], ([h, p, s, r]) => {
  if (typeof h === 'string') f.host = h
  if (typeof p === 'string') f.path = p
  if (typeof s === 'string') f.status = s
  if (typeof r === 'string') f.range = r as LogRange
})

// 改筛选条件回到第一页
watch(() => [f.range, q.value, f.tag, f.host, f.status, path.value, f.country, ui.hideLan, f.size], () => { f.page = 1 })

const { data, error, loading } = useLoader(
  () => api.logs({
    range: f.range, q: q.value.trim(), tag: f.tag, host: f.host, status: f.status, path: path.value.trim(),
    country: f.country, sort: f.sort, dir: f.dir, page: f.page, size: f.size, hide_lan: ui.hideLan ? 1 : 0,
  }),
  [() => [f.range, q.value, f.tag, f.host, f.status, path.value, f.country, f.sort, f.dir, f.page, f.size, ui.hideLan]],
)
const { data: filters } = useLoader(() => api.logFilters(), [], 120_000)

const RANGES = [
  { value: '1h', label: '最近 1 小时' }, { value: '24h', label: '最近 24 小时' },
  { value: 'today', label: '今天' }, { value: '7d', label: '最近 7 天' },
]
const STATUSES = [{ value: '', label: '全部状态' }, ...['2xx', '3xx', '4xx', '5xx'].map((s) => ({ value: s, label: s }))]
const hostOpts = computed(() => [{ value: '', label: '全部域名' }, ...(filters.value?.hosts ?? []).map((h) => ({ value: h, label: h }))])
const countryOpts = computed(() => [{ value: '', label: '全部国家' }, { value: '!中国', label: '中国以外' }, ...(filters.value?.countries ?? []).map((c) => ({ value: c, label: c }))])


function onSort(k: string, d: 'asc' | 'desc') { f.sort = k; f.dir = d; f.page = 1 }
function tone(level: string, banned: boolean) {
  return banned ? 'banned' : level === 'scan' ? 'scan' : level === 'warn' ? 'warn' : ''
}
</script>

<template>
  <div class="log-tabs"><SegControl :model-value="logTab" :items="[['access', '访问记录'], ['guard', '域名拦截']]" @update:model-value="$router.push({ name: 'logs', query: $event === 'guard' ? { tab: 'guard' } : {} })" /></div>
  <GuardLogs v-if="logTab === 'guard'" />
  <template v-else>
  <p class="lead">npm-guard 访问日志（proxy-host 与默认站点），默认最近 24 小时，时间倒序。{{ ui.hideLan ? '已隐藏内网地址。' : '' }}</p>
  <div class="filters">
    <SelectBox v-model="f.range" :options="RANGES" aria-label="时间范围" min-width="136px" />
    <input v-model="f.q" class="field search" aria-label="搜索 IP 或标识" placeholder="搜索 IP / 标识（!排除）">
    <TagFilter v-model="f.tag" :tags="filters?.tags ?? []" />
    <SelectBox v-model="f.host" :options="hostOpts" aria-label="域名" min-width="200px" />
    <SelectBox v-model="f.status" :options="STATUSES" aria-label="状态码" min-width="112px" />
    <input v-model="f.path" class="field path" aria-label="路径关键字" placeholder="路径关键字">
    <SelectBox v-model="f.country" :options="countryOpts" aria-label="国家" min-width="124px" />
    <button type="button" class="btn sm" :class="{ pressed: ui.hideLan }" :aria-pressed="ui.hideLan" title="隐藏 192.168.* 等内网地址的请求（如 CI runner 轮询）" @click="ui.hideLan = !ui.hideLan">隐藏内网</button>
    <RefreshButton class="push-right" />
  </div>
  <p v-if="error" class="banner bad">{{ error }}</p>
  <section class="card" :class="{ loading }">
    <div class="table-wrap">
      <table class="data fit">
        <colgroup>
          <col style="width:118px"><col style="width:172px"><col class="hide-sm" style="width:56px"><col style="width:196px"><col>
          <col style="width:82px"><col class="hide-sm" style="width:68px"><col class="hide-sm" style="width:148px"><col class="hide-sm" style="width:116px"><col style="width:64px">
        </colgroup>
        <thead>
          <tr>
            <SortTh k="ts" label="时间" :sort="f.sort" :dir="f.dir" @sort="onSort" />
            <SortTh k="ip" label="客户端 IP" :sort="f.sort" :dir="f.dir" @sort="onSort" />
            <th class="hide-sm">方法</th><th>域名</th><th>路径</th>
            <SortTh k="status" label="状态" class="num" :sort="f.sort" :dir="f.dir" @sort="onSort" />
            <SortTh k="size" label="大小" class="num hide-sm" :sort="f.sort" :dir="f.dir" @sort="onSort" />
            <th class="hide-sm">User-Agent</th><th class="hide-sm">后端</th><th />
          </tr>
        </thead>
        <tbody>
          <tr v-for="l in data?.rows ?? []" :key="l.id" :class="[tone(l.level, l.banned), { 'ua-open': uaOpen === l.id }]">
            <td class="mono">{{ fmtShort(l.ts) }}</td>
            <td><IpCell :ip="l.ip" :tag="l.tag" :geo="l.geo" :cc="l.cc" :banned="l.banned" /></td>
            <td class="hide-sm" :class="methodCls(l.method)">{{ l.method }}</td>
            <td><span class="clip mono" :title="l.host">{{ l.host || '—' }}</span></td>
            <td><LogPathCell :row="l" /></td>
            <td class="num"><span class="code" :class="codeCls(l.status)" :title="statusTitle(l.status)">{{ l.status }}</span><RouterLink v-if="l.status === 403" class="guard-lookup" :to="{ name: 'logs', query: { tab: 'guard', ip: l.ip, host: l.host, range: f.range } }" title="查找同一 IP 和域名的拦截记录；无记录时无法确认此 403 的来源">查拦截</RouterLink></td>
            <td class="num hide-sm">{{ fmtSize(l.size) }}</td>
            <td class="hide-sm ua" title="点击展开" @click="uaOpen = uaOpen === l.id ? null : l.id"><span class="clip">{{ l.ua || '—' }}</span></td>
            <td class="hide-sm nowrap"><span class="clip mono" :title="l.backend">{{ l.backend || '—' }}</span></td>
            <td><BanButton :ip="l.ip" :banned="l.banned" /></td>
          </tr>
          <tr v-if="data && !data.rows.length"><td colspan="10"><div class="empty">没有符合条件的记录</div></td></tr>
        </tbody>
      </table>
    </div>
    <PagerBar v-if="data" v-model:size="f.size" :page="data.page" :pages="data.pages" :total="data.total" @go="(p) => (f.page = p)" />
  </section>
  </template>
</template>
<style scoped>
.log-tabs { margin-bottom: 14px; }
.guard-lookup { display: block; margin-top: 3px; font-size: 11px; white-space: nowrap; }
</style>
