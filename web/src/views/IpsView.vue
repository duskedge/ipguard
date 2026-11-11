<script setup lang="ts">
import { reactive, watch } from 'vue'

import { api } from '@/api'
import type { Range } from '@/api/types'
import BanButton from '@/components/BanButton.vue'
import IpCell from '@/components/IpCell.vue'
import PagerBar from '@/components/PagerBar.vue'
import RatioCell from '@/components/RatioCell.vue'
import RefreshButton from '@/components/RefreshButton.vue'
import SegControl from '@/components/SegControl.vue'
import TagFilter from '@/components/TagFilter.vue'
import SortTh from '@/components/SortTh.vue'
import StatusTag from '@/components/StatusTag.vue'
import { useUi } from '@/stores/ui'
import { fmtNum, fmtShort, pct } from '@/utils/format'
import { useDebounced, useLoader } from '@/utils/poll'

const ui = useUi()
const f = reactive({ range: '24h' as Range, level: 'all', tag: '', q: '', sort: 'count', dir: 'desc' as 'asc' | 'desc', page: 1, size: 10 })
const q = useDebounced(() => f.q)
watch(() => [f.range, f.level, f.tag, q.value, ui.hideLan, f.size], () => { f.page = 1 })

const { data, error, loading } = useLoader(
  () => api.ips({ range: f.range, level: f.level, tag: f.tag, q: q.value.trim(), sort: f.sort, dir: f.dir, page: f.page, size: f.size, hide_lan: ui.hideLan ? 1 : 0 }),
  [() => [f.range, f.level, f.tag, q.value, f.sort, f.dir, f.page, f.size, ui.hideLan]],
)

function onSort(k: string, d: 'asc' | 'desc') { f.sort = k; f.dir = d; f.page = 1 }
function tone(level: string, banned: boolean) {
  return banned ? 'banned' : level === 'scan' ? 'scan' : level === 'warn' ? 'warn' : ''
}
</script>

<template>
  <p class="lead">次数是窗口内的汇总。点 IP 查看访问明细。</p>
  <div class="page-tools">
    <div class="filters" style="margin:0">
      <SegControl v-model="f.range" :items="[['1h', '1 小时'], ['24h', '24 小时'], ['7d', '7 天']]" />
      <SegControl v-model="f.level" :items="[['all', '全部'], ['suspicious', '只看可疑']]" />
      <TagFilter v-model="f.tag" :tags="data?.tags ?? []" />
      <input v-model="f.q" class="field search" aria-label="搜索 IP 或标识" placeholder="搜索 IP / 标识（!排除）">
      <button type="button" class="btn sm" :class="{ pressed: ui.hideLan }" :aria-pressed="ui.hideLan" title="隐藏 192.168.* 等内网地址" @click="ui.hideLan = !ui.hideLan">隐藏内网</button>
    </div>
    <div class="filters" style="margin:0">
      <span class="meta" style="color:var(--faint)">窗口请求 {{ fmtNum(data?.window_total) }}</span>
      <RefreshButton />
    </div>
  </div>
  <p v-if="error" class="banner bad">{{ error }}</p>
  <section class="card" :class="{ loading }">
    <div class="table-wrap">
      <table class="data">
        <thead>
          <tr>
            <th class="num">#</th>
            <SortTh k="ip" label="IP" :sort="f.sort" :dir="f.dir" @sort="onSort" />
            <SortTh k="count" label="请求数" class="num" :sort="f.sort" :dir="f.dir" @sort="onSort" />
            <th class="num hide-sm">占比</th>
            <SortTh k="err" label="4xx/5xx" class="num" :sort="f.sort" :dir="f.dir" @sort="onSort" />
            <SortTh k="last" label="最近访问" class="hide-sm" :sort="f.sort" :dir="f.dir" @sort="onSort" />
            <th class="hide-sm">主要域名 / 路径</th>
            <th>状态</th><th />
          </tr>
        </thead>
        <tbody>
          <tr v-for="(ip, i) in data?.rows ?? []" :key="ip.ip" :class="tone(ip.level, ip.banned)">
            <td class="num">{{ (data!.page - 1) * f.size + i + 1 }}</td>
            <td><IpCell :ip="ip.ip" :tag="ip.tag" :geo="ip.geo" :cc="ip.cc" :banned="ip.banned" /></td>
            <td class="num">{{ fmtNum(ip.count) }}</td>
            <td class="num hide-sm">{{ pct(ip.share) }}</td>
            <td class="num"><RatioCell :v="ip.err_ratio" /></td>
            <td class="mono hide-sm">{{ fmtShort(ip.last_ts) }}</td>
            <td class="hide-sm"><span class="clip" :title="`${ip.host} ${ip.paths.join(' ')}`">{{ ip.host || '—' }} · {{ ip.paths[0] ?? '' }}</span></td>
            <td><StatusTag :level="ip.level" :banned="ip.banned" /></td>
            <td><BanButton :ip="ip.ip" :banned="ip.banned" /></td>
          </tr>
          <tr v-if="data && !data.rows.length"><td colspan="9"><div class="empty">这个窗口里没有访问</div></td></tr>
        </tbody>
      </table>
    </div>
    <PagerBar v-if="data" v-model:size="f.size" :page="data.page" :pages="data.pages" :total="data.total" @go="(p) => (f.page = p)" />
  </section>
</template>
