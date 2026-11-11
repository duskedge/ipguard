<script setup lang="ts">
import { X } from 'lucide-vue-next'
import { computed, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'

import { api } from '@/api'
import type { LogRange } from '@/api/types'
import LogPathCell from '@/components/LogPathCell.vue'
import PagerBar from '@/components/PagerBar.vue'
import RatioCell from '@/components/RatioCell.vue'
import RefreshButton from '@/components/RefreshButton.vue'
import SegControl from '@/components/SegControl.vue'
import SortTh from '@/components/SortTh.vue'
import { useUi } from '@/stores/ui'
import { codeCls, fmtNum, fmtShort, fmtSize, pct, statusTitle } from '@/utils/format'
import { useDebounced, useLoader } from '@/utils/poll'

const ui = useUi()
const router = useRouter()
const pathTab = ref<'top' | 'err'>('top')
const f = reactive({
  range: '24h' as LogRange,
  q: '',
  host: '',
  sort: 'count',
  dir: 'desc' as 'asc' | 'desc',
  page: 1,
  size: 10,
})
const q = useDebounced(() => f.q)

watch(() => [f.range, q.value, ui.hideLan, f.size], () => { f.page = 1 })

const { data, error, loading } = useLoader(
  () => api.hosts({
    range: f.range,
    q: q.value.trim(),
    host: f.host,
    hide_lan: ui.hideLan ? 1 : 0,
    sort: f.sort,
    dir: f.dir,
    page: f.page,
    size: f.size,
  }),
  [() => [f.range, q.value, f.host, f.sort, f.dir, f.page, f.size, ui.hideLan]],
)

const activePaths = computed(() => (pathTab.value === 'err' ? data.value?.err_paths : data.value?.top_paths) ?? [])
const maxPathMetric = computed(() => {
  const list = activePaths.value
  if (!list.length) return 1
  return Math.max(1, ...list.map((p) => (pathTab.value === 'err' ? p.err_count : p.count)))
})

function onSort(k: string, d: 'asc' | 'desc') {
  f.sort = k
  f.dir = d
  f.page = 1
}

function toggleHost(host: string) {
  f.host = f.host === host ? '' : host
}

function goLogs(query: Record<string, string>) {
  const next: Record<string, string> = { range: f.range, ...query }
  if (next.host === '(默认站点)') delete next.host
  router.push({ name: 'logs', query: next })
}
</script>

<template>
  <p class="lead">按反代域名（Host）汇总请求量、流量、独立访客与异常路径。点域名可下钻筛选下方路径排行，点路径可直达访问日志。</p>

  <div class="page-tools">
    <div class="filters" style="margin:0">
      <SegControl v-model="f.range" :items="[['1h', '1 小时'], ['today', '今天'], ['24h', '24 小时'], ['7d', '7 天']]" />
      <input v-model="f.q" class="field search" aria-label="搜索域名或后端" placeholder="搜索域名 / 后端">
      <button type="button" class="btn sm" :class="{ pressed: ui.hideLan }" :aria-pressed="ui.hideLan" title="隐藏 192.168.* 等内网地址的请求" @click="ui.hideLan = !ui.hideLan">隐藏内网</button>
    </div>
    <div class="filters" style="margin:0">
      <RefreshButton />
    </div>
  </div>

  <p v-if="error" class="banner bad">{{ error }}</p>

  <section class="stat-strip" :class="{ loading }" style="margin-bottom: 14px">
    <div class="stat-cell">
      <div class="stat-label">活跃域名</div>
      <div class="stat-num">{{ fmtNum(data?.total_hosts) }}</div>
      <div class="stat-sub">窗口内有访问记录的服务</div>
    </div>
    <div class="stat-cell">
      <div class="stat-label">窗口总请求</div>
      <div class="stat-num">{{ fmtNum(data?.window_total) }}</div>
      <div class="stat-sub">其中 4xx/5xx {{ fmtNum(data?.total_err) }} 次</div>
    </div>
    <div class="stat-cell">
      <div class="stat-label">响应总流量</div>
      <div class="stat-num">{{ fmtSize(data?.total_bytes ?? 0) }}</div>
      <div class="stat-sub">按 Nginx 响应字节汇总</div>
    </div>
    <div class="stat-cell">
      <div class="stat-label">敏感路径探测</div>
      <div class="stat-num" :class="{ 'warn-text': (data?.total_sens ?? 0) > 0 }">{{ fmtNum(data?.total_sens) }}</div>
      <div class="stat-sub">命中 /.env、/.git 等且返回 4xx/5xx</div>
    </div>
  </section>

  <section class="card" :class="{ loading }">
    <div class="card-h">
      <h2>各域名服务统计</h2>
      <span class="meta">点击域名可过滤下方路径榜</span>
    </div>
    <div class="table-wrap">
      <table class="data fit">
        <colgroup>
          <col style="width:46px">
          <col style="width:210px">
          <col style="width:88px">
          <col class="hide-sm" style="width:76px">
          <col style="width:84px">
          <col style="width:98px">
          <col style="width:124px">
          <col style="width:92px">
          <col class="hide-sm" style="width:136px">
          <col class="hide-sm">
          <col style="width:76px">
        </colgroup>
        <thead>
          <tr>
            <th class="num">#</th>
            <SortTh k="host" label="域名 / 主要后端" :sort="f.sort" :dir="f.dir" @sort="onSort" />
            <SortTh k="count" label="请求数" class="num" :sort="f.sort" :dir="f.dir" @sort="onSort" />
            <th class="num hide-sm">占比</th>
            <SortTh k="ip" label="独立 IP" class="num" :sort="f.sort" :dir="f.dir" @sort="onSort" />
            <SortTh k="bytes" label="响应流量" class="num" :sort="f.sort" :dir="f.dir" @sort="onSort" />
            <SortTh k="err" label="4xx/5xx" class="num" :sort="f.sort" :dir="f.dir" @sort="onSort" />
            <SortTh k="sens" label="敏感扫描" class="num" :sort="f.sort" :dir="f.dir" @sort="onSort" />
            <SortTh k="last" label="最近访问" class="hide-sm pad-l" :sort="f.sort" :dir="f.dir" @sort="onSort" />
            <th class="hide-sm">主要路径</th>
            <th class="num">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="(h, i) in data?.rows ?? []"
            :key="h.host"
            :class="{ 'host-selected': f.host === h.host, scan: h.sens_hits >= 10, warn: h.sens_hits > 0 && h.sens_hits < 10 }"
          >
            <td class="num">{{ (data!.page - 1) * f.size + i + 1 }}</td>
            <td>
              <div class="host-cell">
                <button type="button" class="link mono host-btn clip" :title="f.host === h.host ? '点击取消路径筛选' : '点击仅看该域名的路径排行'" @click="toggleHost(h.host)">
                  {{ h.host }}
                </button>
                <span v-if="f.host === h.host" class="tag manual">已选</span>
              </div>
              <div class="place mono">{{ h.backend || '—' }}</div>
            </td>
            <td class="num">{{ fmtNum(h.count) }}</td>
            <td class="num hide-sm">{{ pct(h.share) }}</td>
            <td class="num">{{ fmtNum(h.unique_ip) }}</td>
            <td class="num mono">{{ fmtSize(h.bytes) }}</td>
            <td class="num"><RatioCell :v="h.err_ratio" /></td>
            <td class="num">
              <span v-if="h.sens_hits > 0" class="tag danger">{{ fmtNum(h.sens_hits) }}</span>
              <span v-else class="faint-text">0</span>
            </td>
            <td class="mono hide-sm pad-l">{{ fmtShort(h.last_ts) }}</td>
            <td class="hide-sm">
              <div v-if="h.top_paths.length" class="path-chips" :title="h.top_paths.join(' · ')">
                <code v-for="tp in h.top_paths" :key="tp">{{ tp }}</code>
              </div>
              <span v-else class="faint-text">—</span>
            </td>
            <td class="num">
              <button type="button" class="btn sm ghost" @click="goLogs({ host: h.host })">查日志</button>
            </td>
          </tr>
          <tr v-if="data && !data.rows.length">
            <td colspan="11"><div class="empty">当前时间窗口内没有匹配的域名访问记录</div></td>
          </tr>
        </tbody>
      </table>
    </div>
    <PagerBar v-if="data" v-model:size="f.size" :page="data.page" :pages="data.pages" :total="data.total" @go="(p) => (f.page = p)" />
  </section>

  <section class="card" :class="{ loading }" style="margin-top: 14px">
    <div class="card-h">
      <div class="path-hd-left">
        <h2>路径排行 Top 15</h2>
        <span v-if="f.host" class="tag manual host-filter-tag">
          {{ f.host }}
          <button type="button" class="tag-x" title="清除域名筛选" @click="f.host = ''"><X :size="12" /></button>
        </span>
      </div>
      <div class="path-hd-right">
        <SegControl
          v-model="pathTab"
          :items="[
            ['top', `热门请求路径 (${data?.top_paths.length ?? 0})`],
            ['err', `异常 / 扫描路径 (${data?.err_paths.length ?? 0})`],
          ]"
        />
      </div>
    </div>
    <div class="table-wrap">
      <table class="data fit">
        <colgroup>
          <col style="width:46px">
          <col style="width:190px">
          <col>
          <col style="width:84px">
          <col style="width:96px">
          <col style="width:88px">
          <col class="hide-sm" style="width:104px">
          <col class="hide-sm" style="width:150px">
          <col style="width:76px">
        </colgroup>
        <thead>
          <tr>
            <th class="num">#</th>
            <th>所属域名</th>
            <th>请求路径</th>
            <th class="num">主要状态</th>
            <th class="num">{{ pathTab === 'err' ? '错误次数' : '请求数' }}</th>
            <th class="num">{{ pathTab === 'err' ? '触发 IP' : '独立 IP' }}</th>
            <th class="num hide-sm">{{ pathTab === 'err' ? '总请求' : '响应流量' }}</th>
            <th class="hide-sm">相对量</th>
            <th class="num">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="(p, i) in activePaths"
            :key="`${pathTab}-${p.host}-${p.path}`"
            :class="{ scan: p.sensitive && p.err_count > 0 }"
          >
            <td class="num">{{ i + 1 }}</td>
            <td>
              <button type="button" class="link mono clip" :title="`筛选域名：${p.host}`" @click="toggleHost(p.host)">
                {{ p.host }}
              </button>
            </td>
            <td>
              <div class="path-cell">
                <LogPathCell
                  :row="{ host: p.host, path: p.path, status: p.top_status }"
                  clickable
                  @select="goLogs(pathTab === 'err' ? { host: p.host, path: p.path, status: p.top_status >= 500 ? '5xx' : '4xx' } : { host: p.host, path: p.path })"
                />
                <span v-if="p.sensitive && p.err_count > 0" class="tag danger">扫描特征</span>
              </div>
            </td>
            <td class="num">
              <span class="code" :class="codeCls(p.top_status)" :title="statusTitle(p.top_status)">{{ p.top_status }}</span>
            </td>
            <td class="num" :class="{ hot: pathTab === 'err' }">
              {{ fmtNum(pathTab === 'err' ? p.err_count : p.count) }}
            </td>
            <td class="num">{{ fmtNum(p.unique_ip) }}</td>
            <td class="num mono hide-sm">
              {{ pathTab === 'err' ? fmtNum(p.count) : fmtSize(p.bytes) }}
            </td>
            <td class="hide-sm">
              <span class="track" style="display:block;width:100%">
                <span
                  class="fill"
                  :class="{ cn: pathTab === 'top' && p.top_status < 400 }"
                  :style="{ width: (((pathTab === 'err' ? p.err_count : p.count) / maxPathMetric) * 100).toFixed(1) + '%' }"
                />
              </span>
            </td>
            <td class="num">
              <button
                type="button"
                class="btn sm ghost"
                @click="goLogs(pathTab === 'err' ? { host: p.host, path: p.path, status: p.top_status >= 500 ? '5xx' : '4xx' } : { host: p.host, path: p.path })"
              >
                查日志
              </button>
            </td>
          </tr>
          <tr v-if="data && !activePaths.length">
            <td colspan="9">
              <div class="empty">{{ pathTab === 'err' ? '当前时间窗口内没有 4xx/5xx 异常路径' : '当前时间窗口内暂无路径记录' }}</div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>

<style scoped>
.host-cell { display: flex; align-items: center; gap: 6px; min-width: 0; }
.host-btn { font-weight: 560; }
.faint-text { color: var(--faint); }
.pad-l { padding-left: 18px !important; }
tr.host-selected td { background: var(--accent-soft); }
tr.host-selected td:first-child { box-shadow: inset 2px 0 0 var(--accent); }

.path-chips {
  display: flex; align-items: center; gap: 6px;
  overflow: hidden; white-space: nowrap;
}
.path-chips code {
  font-family: var(--mono); font-size: 11.5px;
  background: var(--sunken); border: 1px solid var(--line);
  padding: 1px 6px; border-radius: 4px;
  max-width: 180px; overflow: hidden; text-overflow: ellipsis;
}

.path-hd-left { display: flex; align-items: center; gap: 10px; min-width: 0; flex-wrap: wrap; }
.path-hd-right { display: flex; align-items: center; gap: 8px; max-width: 100%; overflow-x: auto; }

.host-filter-tag { display: inline-flex; align-items: center; gap: 4px; font-family: var(--mono); }
.tag-x {
  display: inline-grid; place-items: center;
  border: 0; background: transparent; color: inherit;
  padding: 0; width: 14px; height: 14px; border-radius: 3px;
}
.tag-x:hover { background: rgba(255, 255, 255, .12); }

.path-cell { display: flex; align-items: center; gap: 8px; min-width: 0; }
.path-btn { max-width: 100%; }
</style>
