<script setup lang="ts">
import { computed, ref, watch } from 'vue'

import { api } from '@/api'
import type { LogRange } from '@/api/types'
import CountryBars from '@/components/CountryBars.vue'
import GeoDonut from '@/components/GeoDonut.vue'
import GeoMap, { type MapPoint } from '@/components/GeoMap.vue'
import PagerBar from '@/components/PagerBar.vue'
import RefreshButton from '@/components/RefreshButton.vue'
import SegControl from '@/components/SegControl.vue'
import { flag, fmtNum, share } from '@/utils/format'
import { useLoader } from '@/utils/poll'

const scope = ref<'world' | 'china'>('world')
const range = ref<LogRange>('today')
const { data: g, error } = useLoader(() => api.geo(range.value), [range], 60_000)

const countries = computed(() => g.value?.countries ?? [])
const provinces = computed(() => g.value?.provinces ?? [])
const cPage = ref(1)
const cSize = ref(10)
const pPage = ref(1)
const pSize = ref(10)
watch(range, () => { cPage.value = 1; pPage.value = 1 })

const cPages = computed(() => Math.max(1, Math.ceil(countries.value.length / cSize.value)))
const pagedCountries = computed(() => countries.value.slice((cPage.value - 1) * cSize.value, cPage.value * cSize.value))
const pPages = computed(() => Math.max(1, Math.ceil(provinces.value.length / pSize.value)))
const pagedProvinces = computed(() => provinces.value.slice((pPage.value - 1) * pSize.value, pPage.value * pSize.value))
const worldPoints = computed<MapPoint[]>(() =>
  countries.value.filter((c) => c.cc && (c.lat || c.lng)).map((c) => ({ name: c.name, lat: c.lat, lng: c.lng, req: c.req, ip: c.ip, domestic: c.domestic })),
)
const chinaPoints = computed<MapPoint[]>(() =>
  provinces.value.filter((p) => p.lat || p.lng).map((p) => ({ name: p.city || p.province, sub: p.province, lat: p.lat, lng: p.lng, req: p.req, ip: p.ip, domestic: true })),
)
const maxCountry = computed(() => Math.max(1, ...countries.value.map((c) => c.req)))
const maxProvince = computed(() => Math.max(1, ...provinces.value.map((p) => p.req)))
const provinceBars = computed(() => provinces.value.filter((p) => p.province !== '其他').slice(0, 6).map((p) => ({ name: p.province, req: p.req, domestic: true, cc: 'CN' })))
const rangeName = computed(() => ({ today: '今日', '1h': '最近 1 小时', '24h': '最近 24 小时', '7d': '最近 7 天' })[range.value])
</script>

<template>
  <div class="page-tools">
    <div class="filters" style="margin:0">
      <SegControl v-model="scope" :items="[['world', '世界'], ['china', '中国大陆']]" />
      <SegControl v-model="range" :items="[['today', '今天'], ['24h', '24 小时'], ['7d', '7 天']]" />
    </div>
    <div class="filters" style="margin:0">
      <span style="color:var(--faint);font-size:12px">按请求数 · 内网 {{ fmtNum(g?.lan_req) }} 次不计入国外</span>
      <RefreshButton />
    </div>
  </div>
  <p v-if="error" class="banner bad">{{ error }}</p>
  <template v-if="g">
    <template v-if="scope === 'world'">
      <div class="grid-geo">
        <section class="card">
          <div class="card-h"><h2>国内 / 国外</h2></div>
          <div class="card-b">
            <GeoDonut :geo="g" />
            <p class="note">独立 IP {{ fmtNum(g.unique_ip) }}，其中国内 {{ fmtNum(g.domestic_ip) }}。</p>
          </div>
        </section>
        <section class="card">
          <div class="card-h"><h2>来源地图</h2></div>
          <div class="card-b">
            <GeoMap key="world" kind="world" :points="worldPoints" />
            <p class="map-cap">放大后可看到城市和道路。靛蓝是中国大陆，琥珀是境外；点位为该国访问 IP 的平均位置。</p>
          </div>
        </section>
      </div>
      <section class="card" style="margin-top:14px">
        <div class="card-h"><h2>国家排行</h2><span class="meta">{{ rangeName }} · 条均 ≥ 80 用琥珀色标出</span></div>
        <div class="table-wrap">
          <table class="data">
            <thead><tr><th>国家 / 地区</th><th class="num">请求数</th><th class="num">独立 IP</th><th class="num">条均</th><th class="num">占比</th><th class="hide-sm">相对量</th></tr></thead>
            <tbody>
              <tr v-for="c in pagedCountries" :key="c.name">
                <td>{{ flag(c.cc) }} {{ c.name }}</td>
                <td class="num">{{ fmtNum(c.req) }}</td>
                <td class="num">{{ fmtNum(c.ip) }}</td>
                <td class="num" :class="{ hot: c.req / c.ip >= 80 }">{{ fmtNum(Math.round(c.req / Math.max(1, c.ip))) }}</td>
                <td class="num">{{ share(c.req, g.total) }}</td>
                <td class="hide-sm"><span class="track" style="display:block;width:100%"><span class="fill" :class="{ cn: c.domestic }" :style="{ width: ((c.req / maxCountry) * 100).toFixed(1) + '%' }" /></span></td>
              </tr>
              <tr v-if="!countries.length"><td colspan="6"><div class="empty">没有外部访问</div></td></tr>
            </tbody>
          </table>
        </div>
        <PagerBar v-if="countries.length" v-model:size="cSize" :page="cPage" :pages="cPages" :total="countries.length" @go="(p) => (cPage = p)" />
      </section>
    </template>
    <template v-else>
      <div class="grid-geo">
        <section class="card">
          <div class="card-h"><h2>大陆请求</h2></div>
          <div class="card-b">
            <div class="stat-num">{{ fmtNum(g.domestic_req) }}</div>
            <p class="note">{{ fmtNum(g.domestic_ip) }} 个独立 IP，占{{ rangeName }} {{ share(g.domestic_req, g.total) }}。</p>
            <CountryBars :items="provinceBars" />
          </div>
        </section>
        <section class="card">
          <div class="card-h"><h2>国内来源</h2></div>
          <div class="card-b">
            <GeoMap key="china" kind="china" :points="chinaPoints" />
            <p class="map-cap">底图为腾讯地图，可放大到街道。点位标在省会。</p>
          </div>
        </section>
      </div>
      <section class="card" style="margin-top:14px">
        <div class="card-h"><h2>省 / 市排行</h2><span class="meta">占大陆请求的比例</span></div>
        <div class="table-wrap">
          <table class="data">
            <thead><tr><th>省</th><th>主要城市</th><th class="num">请求数</th><th class="num">独立 IP</th><th class="num">占国内</th><th>相对量</th></tr></thead>
            <tbody>
              <tr v-for="p in pagedProvinces" :key="p.province">
                <td>{{ p.province }}</td><td>{{ p.cities }}</td>
                <td class="num">{{ fmtNum(p.req) }}</td><td class="num">{{ fmtNum(p.ip) }}</td>
                <td class="num">{{ share(p.req, g.domestic_req) }}</td>
                <td><span class="track" style="display:block"><span class="fill cn" :style="{ width: ((p.req / maxProvince) * 100).toFixed(1) + '%' }" /></span></td>
              </tr>
              <tr v-if="!provinces.length"><td colspan="6"><div class="empty">没有国内访问</div></td></tr>
            </tbody>
          </table>
        </div>
        <PagerBar v-if="provinces.length" v-model:size="pSize" :page="pPage" :pages="pPages" :total="provinces.length" @go="(p) => (pPage = p)" />
      </section>
    </template>
  </template>
</template>
