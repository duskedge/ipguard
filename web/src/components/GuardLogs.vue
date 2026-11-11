<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ChevronDown, ShieldBan } from 'lucide-vue-next'
import { api } from '@/api'
import type { LogRange } from '@/api/types'
import IpCell from '@/components/IpCell.vue'
import PagerBar from '@/components/PagerBar.vue'
import RefreshButton from '@/components/RefreshButton.vue'
import SelectBox from '@/components/SelectBox.vue'
import { fmtFull } from '@/utils/format'
import { useDebounced, useLoader } from '@/utils/poll'

const route = useRoute()
const router = useRouter()
const query = (key: string) => typeof route.query[key] === 'string' ? route.query[key] as string : ''
const f = reactive({ range: (query('range') || '24h') as LogRange, ip: query('ip'), host: query('host'), rule: query('rule'), path: '', page: 1, size: 20 })
const ip = useDebounced(() => f.ip)
const host = useDebounced(() => f.host)
const path = useDebounced(() => f.path)
const opened = ref<number | null>(null)
const ruleHosts = computed(() => f.rule.split(/[,;，；]+/).map((s) => s.trim()).filter(Boolean))
watch(() => [route.query.ip, route.query.host, route.query.rule, route.query.range], () => {
  f.ip = query('ip'); f.host = query('host'); f.rule = query('rule'); f.range = (query('range') || '24h') as LogRange
})
watch(() => [f.range, ip.value, host.value, path.value, f.rule, f.size], () => { f.page = 1; opened.value = null })
const { data, loading, error } = useLoader(() => api.guardLogs({ range: f.range, ip: ip.value.trim(), host: host.value.trim(), rule: f.rule, path: path.value.trim(), page: f.page, size: f.size }),
  [() => [f.range, ip.value, host.value, f.rule, path.value, f.page, f.size]])
const ranges = [{value:'1h',label:'最近 1 小时'},{value:'24h',label:'最近 24 小时'},{value:'today',label:'今天'},{value:'7d',label:'最近 7 天'}]
const hasFilter = computed(() => f.ip || f.host || f.rule || f.path)
function reset() { f.ip = ''; f.host = ''; f.rule = ''; f.path = ''; router.replace({name: 'logs', query: {tab:'guard'}}) }
</script>

<template>
  <p class="lead">记录域名访问控制实际拒绝的请求，保留当时的规则与原因。同一分钟的相同拦截合并计数，保留最近 7 天、最多 2 万条样本；记录跨部署保留；设置页为持久化累计次数，与采样明细条数不同，历史 403 无法补录。</p>
  <div class="filters">
    <SelectBox v-model="f.range" :options="ranges" aria-label="拦截时间范围" min-width="136px" />
    <input v-model="f.ip" class="field" aria-label="拦截客户端 IP" placeholder="客户端 IP（精确匹配）">
    <input v-model="f.host" class="field" aria-label="拦截域名" placeholder="访问域名（精确匹配）">
    <input v-model="f.path" class="field" aria-label="拦截路径" placeholder="路径关键字">
    <span v-if="ruleHosts.length === 1" class="tag">规则：{{ ruleHosts[0] }}</span>
    <div v-else-if="ruleHosts.length > 1" class="rule-host-pills" role="group" aria-label="按规则内域名筛选">
      <span class="rule-host-label">规则域名：</span>
      <button type="button" class="rule-host-pill" :class="{ active: !f.host }" @click="f.host = ''">全部 ({{ ruleHosts.length }})</button>
      <button
        v-for="rh in ruleHosts"
        :key="rh"
        type="button"
        class="rule-host-pill mono"
        :class="{ active: f.host === rh }"
        @click="f.host = f.host === rh ? '' : rh"
      >
        {{ rh }}
      </button>
    </div>
    <button v-if="hasFilter" type="button" class="btn sm ghost" @click="reset">清除筛选</button>
    <RefreshButton class="push-right" />
  </div>
  <p v-if="error" class="banner bad" role="alert">{{ error }}</p>
  <p v-if="data?.omitted" class="banner" role="status">本次运行已省略 {{ data.omitted.toLocaleString() }} 次拦截记录（采样限流、队列满或写入失败），拦截仍正常执行，列表次数仅统计已记录的请求。</p>
  <section class="card guard-log-card" :class="{ loading }">
    <header class="guard-log-heading"><span><ShieldBan :size="16" />域名规则拦截</span><span class="meta">{{ data ? `共 ${data.total.toLocaleString()} 条采样记录；` : '' }}一条可合并多次请求，点击查看详情</span></header>
    <div class="guard-table-wrap">
      <table class="data guard-table">
        <thead><tr><th>时间</th><th>客户端 IP</th><th>域名 / 路径</th><th>拦截原因</th><th>结果 / 次数</th></tr></thead>
        <tbody>
          <template v-for="row in data?.rows ?? []" :key="row.id">
            <tr>
              <td class="mono nowrap" :title="`首次：${fmtFull(row.ts)}；最近：${fmtFull(row.last_ts)}`">{{ fmtFull(row.last_ts) }}</td>
              <td><IpCell :ip="row.ip" :tag="row.tag" :geo="row.geo" :cc="row.cc" /></td>
              <td class="guard-target"><span class="mono">{{ row.host }}</span><span class="guard-path mono" :title="row.path">{{ row.method || '—' }} {{ row.path || '/' }}</span></td>
              <td><button class="guard-reason" type="button" :aria-expanded="opened === row.id" :aria-controls="`guard-detail-${row.id}`" @click="opened = opened === row.id ? null : row.id"><span><strong>{{ row.mode === 'domestic' ? '仅限国内访问' : '仅限指定来源' }}</strong><span>{{ row.reason }}</span></span><ChevronDown :size="14" :class="{ rotated: opened === row.id }" /></button></td>
              <td><span class="tag warn">403</span><span class="guard-hit-count">{{ row.hits.toLocaleString() }} 次</span></td>
            </tr>
            <tr v-if="opened === row.id" :id="`guard-detail-${row.id}`" class="guard-detail"><td colspan="5">
              <dl><div><dt>拦截来源</dt><dd>ipguard 域名访问控制</dd></div><div><dt>命中规则</dt><dd class="mono">{{ row.rule_host }}</dd></div><div><dt>当时的来源</dt><dd>{{ row.geo }}{{ row.tag ? `（标识：${row.tag}）` : '（无 IP 标识）' }}</dd></div><div><dt>允许的标识</dt><dd>{{ row.allow_tags || '未配置' }}</dd></div><div><dt>允许的 IP / 网段</dt><dd class="mono">{{ row.allow_ips || '未配置' }}</dd></div><div><dt>User-Agent</dt><dd>{{ row.ua || '未记录' }}</dd></div></dl>
              <p>判定结果记录于请求发生时。此请求被域名规则拒绝，不代表该 IP 已被 CrowdSec 封禁。</p>
            </td></tr>
          </template>
          <tr v-if="data && !data.rows.length"><td colspan="5"><div class="empty">没有符合条件的域名拦截记录。此处从功能启用后开始记录，其他来源的 403 不会出现在这里。</div></td></tr>
          <tr v-else-if="!data && loading"><td colspan="5"><div class="empty">正在加载拦截记录…</div></td></tr>
        </tbody>
      </table>
    </div>
    <PagerBar v-if="data" v-model:size="f.size" :page="data.page" :pages="data.pages" :total="data.total" @go="f.page = $event" />
  </section>
</template>

<style scoped>
.rule-host-pills { display: inline-flex; align-items: center; flex-wrap: wrap; gap: 5px; padding: 3px 6px; border: 1px solid var(--line); border-radius: 7px; background: var(--sunken); }
.rule-host-label { font-size: 11.5px; color: var(--dim); padding: 0 2px; }
.rule-host-pill { border: 1px solid transparent; background: transparent; color: var(--dim); font-size: 11.5px; padding: 3px 8px; border-radius: 5px; cursor: pointer; line-height: 1.4; }
.rule-host-pill:hover { color: var(--text); background: var(--hover); }
.rule-host-pill.active { background: var(--elev); color: var(--accent-text); border-color: color-mix(in srgb, var(--accent) 30%, var(--line)); font-weight: 560; }
.guard-hit-count { display: block; margin-top: 5px; color: var(--dim); font-size: 12px; white-space: nowrap; }
.guard-log-heading { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 15px 18px; border-bottom: 1px solid var(--line); }
.guard-log-heading > span:first-child { display: flex; align-items: center; gap: 8px; font-weight: 600; }
.guard-log-heading .meta { color: var(--dim); font-size: 12px; }
.guard-table-wrap { overflow-x: auto; }
.guard-table { min-width: 900px; }
.guard-table td { padding: 12px 14px; }
.guard-target { max-width: 280px; overflow-wrap: anywhere; }
.guard-path { display: block; color: var(--dim); font-size: 11px; margin-top: 5px; max-width: 280px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.guard-reason { display: flex; align-items: center; gap: 16px; width: 100%; text-align: left; color: var(--text); background: transparent; border: 0; padding: 4px 0; font: inherit; }
.guard-reason strong { display: block; font-size: 12px; font-weight: 550; margin-bottom: 4px; }
.guard-reason span span { color: var(--dim); font-size: 12px; }
.guard-reason svg { flex-shrink: 0; color: var(--faint); }
.guard-reason .rotated { transform: rotate(180deg); }
.guard-detail td { background: var(--sunken); }
.guard-detail dl { display: grid; grid-template-columns: 1fr 1fr; gap: 14px 28px; margin: 4px 0; }
.guard-detail dl > div { display: flex; gap: 16px; min-width: 0; }
.guard-detail dt { min-width: 105px; color: var(--dim); }
.guard-detail dd { margin: 0; overflow-wrap: anywhere; }
.guard-detail p { color: var(--dim); font-size: 12px; margin: 18px 0 0; }
@media (max-width: 760px) { .guard-hit-count { display: block; margin-top: 5px; color: var(--dim); font-size: 12px; white-space: nowrap; }
.guard-log-heading { flex-wrap: wrap; } .guard-detail dl { grid-template-columns: 1fr; } }
</style>
