<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'

import { api } from '@/api'
import BanButton from '@/components/BanButton.vue'
import PagerBar from '@/components/PagerBar.vue'
import RefreshButton from '@/components/RefreshButton.vue'
import SelectBox from '@/components/SelectBox.vue'
import SegControl from '@/components/SegControl.vue'
import SortTh from '@/components/SortTh.vue'
import SourceTag from '@/components/SourceTag.vue'
import { useUi } from '@/stores/ui'
import { flag, fmtFull, fmtNum, fmtRemain } from '@/utils/format'
import { useDebounced, useLoader } from '@/utils/poll'

const ui = useUi()
const f = reactive({ source: 'all', q: '', country: '', sort: 'ts', dir: 'desc' as 'asc' | 'desc', page: 1, size: 10 })
const q = useDebounced(() => f.q)
watch(() => [f.source, q.value, f.country, f.size], () => { f.page = 1 })

const { data, error, loading } = useLoader(
  () => api.bans({ source: f.source, q: q.value.trim(), country: f.country, sort: f.sort, dir: f.dir, page: f.page, size: f.size }),
  [() => [f.source, q.value, f.country, f.sort, f.dir, f.page, f.size]],
)
function onSort(k: string, d: 'asc' | 'desc') { f.sort = k; f.dir = d; f.page = 1 }

const countryOpts = computed(() => [{ value: '', label: '全部国家' }, { value: '!中国', label: '中国以外' }, ...(data.value?.countries ?? []).map((c) => ({ value: c, label: c }))])

const auditOpen = ref(false)
const auditPage = ref(1)
const auditSize = ref(10)
const { data: audit } = useLoader(
  () => (auditOpen.value ? api.audit({ page: auditPage.value, size: auditSize.value }) : Promise.resolve(null)),
  [auditOpen, auditPage, auditSize],
)
const ACTION: Record<string, string> = { ban: '封禁', unban: '解封' }
</script>

<template>
  <p class="lead">
    当前 CrowdSec 生效决策 {{ fmtNum(data?.counts.total) }} 条，其中社区黑名单 {{ fmtNum(data?.counts.community) }} 条。
    封禁/解封会同步到 MikroTik。
  </p>
  <div class="filters">
    <SegControl v-model="f.source" :items="[['all', '全部'], ['manual', '手动'], ['auto', '自动'], ['community', '社区']]" />
    <button type="button" class="btn sm" :class="{ pressed: f.source === 'manual' }" @click="f.source = f.source === 'manual' ? 'all' : 'manual'">只看手动</button>
    <input v-model="f.q" class="field search" aria-label="搜索 IP" placeholder="搜索 IP 或网段">
    <SelectBox v-model="f.country" :options="countryOpts" aria-label="国家" min-width="124px" />
    <RefreshButton class="push-right" />
  </div>
  <p v-if="error || data?.error" class="banner bad">CrowdSec：{{ error || data?.error }}</p>
  <section class="card" :class="{ loading }">
    <div class="table-wrap">
      <table class="data">
        <thead>
          <tr>
            <SortTh k="target" label="IP / 网段" :sort="f.sort" :dir="f.dir" @sort="onSort" />
            <th>国家 · 城市</th>
            <SortTh k="source" label="来源" :sort="f.sort" :dir="f.dir" @sort="onSort" />
            <th>场景 / 原因</th>
            <SortTh k="remain" label="剩余时长" :sort="f.sort" :dir="f.dir" @sort="onSort" />
            <SortTh k="ts" label="创建时间" class="hide-sm" :sort="f.sort" :dir="f.dir" @sort="onSort" />
            <th />
          </tr>
        </thead>
        <tbody>
          <tr v-for="b in data?.rows ?? []" :key="b.id">
            <td class="mono">{{ b.target }}</td>
            <td>{{ flag(b.cc) }} {{ b.geo }}</td>
            <td><SourceTag :source="b.source" /></td>
            <td><span class="clip mono" :title="`${b.scenario} (${b.origin})`">{{ b.scenario }}</span></td>
            <td>{{ fmtRemain(b.remain_min, b.permanent) }}</td>
            <td class="mono hide-sm">{{ fmtFull(b.created_ts) }}</td>
            <td><button type="button" class="btn sm ghost-danger" @click="ui.openUnban(b)">解封</button></td>
          </tr>
          <tr v-if="data && !data.rows.length"><td colspan="7"><div class="empty">没有符合条件的封禁</div></td></tr>
        </tbody>
      </table>
    </div>
    <PagerBar v-if="data" v-model:size="f.size" :page="data.page" :pages="data.pages" :total="data.total" @go="(p) => (f.page = p)" />
  </section>

  <section class="card" style="margin-top:14px">
    <div class="card-h">
      <h2>操作记录</h2>
      <button type="button" class="btn sm ghost" @click="auditOpen = !auditOpen">{{ auditOpen ? '收起' : '展开' }}</button>
    </div>
    <template v-if="auditOpen">
      <div class="table-wrap">
        <table class="data compact">
          <thead><tr><th>时间</th><th>操作</th><th>目标</th><th class="hide-sm">时长</th><th class="hide-sm">原因 / 场景</th><th class="hide-sm">操作人</th><th>结果</th><th /></tr></thead>
          <tbody>
            <tr v-for="a in audit?.rows ?? []" :key="a.id" class="audit-row">
              <td class="mono">{{ fmtFull(Date.parse(a.created_at)) }}</td>
              <td>{{ ACTION[a.action] ?? a.action }}</td>
              <td class="mono">{{ a.target }}</td>
              <td class="hide-sm">{{ a.duration === 'forever' ? '永久' : a.duration || '—' }}</td>
              <td class="hide-sm"><span class="clip mono" :title="a.reason">{{ a.reason || '—' }}</span></td>
              <td class="hide-sm">{{ a.operator }} <span class="place">{{ a.client_ip }}</span></td>
              <td><span v-if="a.result === 'ok'" class="tag ok">成功</span><span v-else class="tag danger" :title="a.error">失败</span></td>
              <td><BanButton v-if="a.action === 'unban' && a.result === 'ok' && !a.target.includes('/')" :ip="a.target" /></td>
            </tr>
            <tr v-if="audit && !audit.rows.length"><td colspan="8"><div class="empty">还没有操作记录</div></td></tr>
          </tbody>
        </table>
      </div>
      <PagerBar v-if="audit" v-model:size="auditSize" :page="auditPage" :pages="Math.max(1, Math.ceil(audit.total / auditSize))" :total="audit.total" @go="(p) => (auditPage = p)" />
    </template>
  </section>
</template>
