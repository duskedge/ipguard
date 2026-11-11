<script setup lang="ts">
import { computed, ref } from 'vue'

import { api } from '@/api'
import type { Range } from '@/api/types'
import BanButton from '@/components/BanButton.vue'
import CountryBars from '@/components/CountryBars.vue'
import GeoDonut from '@/components/GeoDonut.vue'
import IpCell from '@/components/IpCell.vue'
import LineChart from '@/components/LineChart.vue'
import RefreshButton from '@/components/RefreshButton.vue'
import SegControl from '@/components/SegControl.vue'
import { useUi } from '@/stores/ui'
import { flag, fmtClock, fmtNum, fmtShort, pct, signed } from '@/utils/format'
import { useLoader } from '@/utils/poll'

const ui = useUi()
const trend = ref<Range>('24h')
const { data, error } = useLoader(() => api.overview(trend.value), [trend])

const ov = computed(() => data.value?.overview)
const bans = computed(() => data.value?.bans)
const topCountries = computed(() => (ov.value?.geo.countries ?? []).slice(0, 6))
function tone(level: string, banned: boolean) {
  return banned ? 'banned' : level === 'scan' ? 'scan' : level === 'warn' ? 'warn' : ''
}
</script>

<template>
  <p v-if="error" class="banner bad">数据加载失败：{{ error }}</p>
  <p v-if="ui.meta?.crowdsec && !ui.meta.crowdsec.connected" class="banner">
    CrowdSec {{ ui.meta.crowdsec.configured ? '连接失败' : '未配置' }}，封禁数量与封禁操作不可用。{{ ui.meta.crowdsec.error }}
  </p>
  <p v-if="ui.meta && !ui.meta.geoip.loaded" class="banner">GeoIP 库未加载，归属地显示为“未知”。</p>
  <template v-if="ov">
    <div class="page-tools" style="margin-bottom:12px">
      <p class="lead" style="margin:0">npm-guard 上 {{ ov.hosts }} 个站点，截至今天 {{ fmtClock(ov.now) }}。国内按中国大陆统计，中国香港单列。</p>
      <RefreshButton />
    </div>
    <section class="stat-strip" aria-label="今日概况">
      <div class="stat-cell">
        <div class="stat-label">今日请求</div>
        <div class="stat-num">{{ fmtNum(ov.today_requests) }}</div>
        <div class="stat-sub">昨日同期 {{ fmtNum(ov.yesterday_requests) }} · {{ signed(ov.req_delta_pct) }}</div>
      </div>
      <div class="stat-cell">
        <div class="stat-label">独立 IP</div>
        <div class="stat-num">{{ fmtNum(ov.unique_ip) }}</div>
        <div class="stat-sub">昨日同期 {{ fmtNum(ov.yesterday_ip) }} · {{ signed(ov.ip_delta_pct) }}</div>
      </div>
      <div class="stat-cell">
        <div class="stat-label">国外占比</div>
        <div class="stat-num" :class="{ 'warn-text': ov.overseas_pct > ov.yesterday_overseas_pct + 5 }">{{ ov.overseas_pct }}%</div>
        <div class="stat-sub">昨日同期 {{ ov.yesterday_overseas_pct }}%</div>
      </div>
      <div class="stat-cell">
        <div class="stat-label">当前封禁</div>
        <div class="stat-num">{{ bans ? fmtNum(bans.total) : '—' }}</div>
        <div class="split">
          <span class="k-manual">手动 <b>{{ fmtNum(bans?.manual) }}</b></span>
          <span class="k-auto">自动 <b>{{ fmtNum(bans?.auto) }}</b></span>
          <span class="k-comm">社区 <b>{{ fmtNum(bans?.community) }}</b></span>
        </div>
      </div>
    </section>

    <div class="grid-2">
      <section class="card">
        <div class="card-h">
          <h2>访问趋势</h2>
          <SegControl v-model="trend" :items="[['1h', '1 小时'], ['24h', '24 小时'], ['7d', '7 天']]" />
        </div>
        <div class="card-b">
          <LineChart :values="ov.trend.values" :labels="ov.trend.labels" />
          <div class="chart-foot">
            {{ ov.trend.hint }} · 合计 {{ fmtNum(ov.trend.sum) }}<template v-if="ov.trend.peak"> · 峰值 {{ fmtNum(ov.trend.peak) }}（{{ ov.trend.peak_at }}）</template>
          </div>
        </div>
      </section>
      <section class="card">
        <div class="card-h"><h2>归属地</h2><RouterLink to="/geo">全部</RouterLink></div>
        <div class="card-b">
          <GeoDonut :geo="ov.geo" />
          <CountryBars :items="topCountries" />
        </div>
      </section>
    </div>

    <div class="grid-2">
      <section class="card">
        <div class="card-h"><h2>Top 访问 IP</h2><span class="meta">最近 24 小时 · 不含内网 · <RouterLink to="/ips">全部</RouterLink></span></div>
        <div class="table-wrap">
          <table class="data compact">
            <thead><tr><th>IP</th><th class="num">次数</th><th class="hide-sm">最近</th><th /></tr></thead>
            <tbody>
              <tr v-for="ip in ov.top_ips" :key="ip.ip" :class="tone(ip.level, ip.banned)">
                <td><IpCell :ip="ip.ip" :tag="ip.tag" :geo="ip.geo" :cc="ip.cc" :banned="ip.banned" /></td>
                <td class="num">{{ fmtNum(ip.count) }}</td>
                <td class="hide-sm mono">{{ fmtShort(ip.last_ts) }}</td>
                <td><BanButton :ip="ip.ip" :banned="ip.banned" /></td>
              </tr>
              <tr v-if="!ov.top_ips.length"><td colspan="4"><div class="empty">最近 24 小时没有访问</div></td></tr>
            </tbody>
          </table>
        </div>
      </section>
      <section class="card">
        <div class="card-h"><h2>最近异常</h2><span class="meta">{{ ov.anomalies.length }} 个地址（未封禁）</span></div>
        <div class="anom">
          <div v-for="ip in ov.anomalies" :key="ip.ip" class="anom-row" :class="{ warn: ip.level === 'warn' }">
            <div>
              <div class="ip">
                <button type="button" class="link" @click="ui.openIp(ip.ip)">{{ flag(ip.cc) }} {{ ip.ip }}</button>
                <span v-if="ip.tag" class="tag ip-tag" style="margin-left:6px">{{ ip.tag }}</span>
              </div>
              <div class="place">{{ ip.geo }}</div>
            </div>
            <div><BanButton :ip="ip.ip" :banned="ip.banned" /></div>
            <div class="why">
              24 小时 {{ fmtNum(ip.count) }} 次，{{ ip.level === 'scan' ? '敏感路径' : '路径' }}
              <template v-for="p in ip.paths" :key="p"><code>{{ p }}</code> </template>，4xx/5xx 占 {{ pct(ip.err_ratio) }}
            </div>
          </div>
          <div v-if="!ov.anomalies.length" class="empty">没有发现可疑地址</div>
        </div>
      </section>
    </div>
  </template>
  <p v-else-if="!error" class="lead">加载中…</p>
</template>
