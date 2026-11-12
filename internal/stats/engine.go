package stats

import (
	"net/netip"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/duskedge/ipguard/internal/geoip"
	"github.com/duskedge/ipguard/internal/iptag"
)

// BanChecker 判断某 IP 当前是否被封禁（由封禁服务实现）。
type BanChecker interface {
	Banned(ip netip.Addr) bool
	Version() uint64
}

// Detect 是异常识别阈值。
type Detect struct {
	SensitivePaths []string
	ScanHits       int
	ScanErrRatio   float64
	WarnMinReq     int
	WarnErrRatio   float64
}

// Engine 基于 Store 做聚合。并发安全。
type Engine struct {
	store  *Store
	geo    *geoip.Resolver
	bans   BanChecker
	detect Detect
	loc    *time.Location
	now    func() time.Time

	mu    sync.Mutex
	cache map[string]cacheEntry

	// sensitive 是当前生效的敏感路径（小写），可在设置页热更新；sensVer 参与缓存版本
	sensitive atomic.Pointer[[]string]
	sensVer   atomic.Uint64

	// backendLabel 把日志里的后端（只有地址）换成“地址:端口”或别名，nil 时原样展示
	backendLabel    func(host, raw string) string
	configuredHosts func() []string
	tags            *iptag.Store
}

// SetBackendLabeler 设置后端展示转换（见 npmconf）。
func (e *Engine) SetBackendLabeler(fn func(host, raw string) string) { e.backendLabel = fn }

// SetConfiguredHosts 设置 npm-guard 已配置的全部域名列表来源。
func (e *Engine) SetConfiguredHosts(fn func() []string) { e.configuredHosts = fn }

// SetIPTags 设置自定义 IP 标识存储。
func (e *Engine) SetIPTags(tags *iptag.Store) { e.tags = tags }

func (e *Engine) tagOf(ip netip.Addr) string {
	if e.tags == nil {
		return ""
	}
	return e.tags.Lookup(ip)
}

// Tags 返回当前已配置的去重标识名称列表。
func (e *Engine) Tags() []string {
	if e.tags == nil {
		return []string{}
	}
	return e.tags.Tags()
}

func (e *Engine) backendOf(host, raw string) string {
	if e.backendLabel == nil {
		return raw
	}
	return e.backendLabel(host, raw)
}

type cacheEntry struct {
	ver uint64
	at  time.Time
	val any
}

// NewEngine 创建聚合引擎。bans 可以为 nil（未接 CrowdSec 时）。
func NewEngine(store *Store, geo *geoip.Resolver, bans BanChecker, d Detect, loc *time.Location) *Engine {
	e := &Engine{store: store, geo: geo, bans: bans, detect: d, loc: loc, now: time.Now, cache: map[string]cacheEntry{}}
	e.SetSensitivePaths(d.SensitivePaths)
	return e
}

// SetSensitivePaths 替换生效的敏感路径（小写子串匹配），之后的统计立即按新列表计算。
func (e *Engine) SetSensitivePaths(paths []string) {
	list := make([]string, 0, len(paths))
	for _, p := range paths {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
			list = append(list, p)
		}
	}
	e.sensitive.Store(&list)
	e.sensVer.Add(1)
}

// SensitivePaths 返回当前生效的敏感路径副本。
func (e *Engine) SensitivePaths() []string {
	return append([]string(nil), *e.sensitive.Load()...)
}

// cached 在数据版本不变、且距上次计算不足 10 秒时复用结果。
// 时间窗是滑动的，所以即使没有新日志也要定期重算。
func (e *Engine) cached(key string, fn func() any) any {
	ver := e.store.Version()
	if e.bans != nil {
		ver = ver*1_000_003 + e.bans.Version()
	}
	if e.tags != nil {
		ver = ver*1_000_003 + e.tags.Version()
	}
	if e.geo != nil {
		ver = ver*1_000_003 + e.geo.Version()
	}
	ver = ver*1_000_003 + e.sensVer.Load()
	now := e.now()
	e.mu.Lock()
	c, ok := e.cache[key]
	e.mu.Unlock()
	if ok && c.ver == ver && now.Sub(c.at) < 10*time.Second {
		return c.val
	}
	v := fn()
	e.mu.Lock()
	if len(e.cache) > 256 {
		e.cache = map[string]cacheEntry{}
	}
	e.cache[key] = cacheEntry{ver: ver, at: now, val: v}
	e.mu.Unlock()
	return v
}

// Window 把 range 参数换算成 [from, to) 毫秒时间窗。
func (e *Engine) Window(rng string) (int64, int64) {
	now := e.now()
	to := now.UnixMilli() + 1
	switch rng {
	case "1h":
		return now.Add(-time.Hour).UnixMilli(), to
	case "7d":
		return now.Add(-7 * 24 * time.Hour).UnixMilli(), to
	case "today":
		return e.dayStart(now).UnixMilli(), to
	default: // 24h
		return now.Add(-24 * time.Hour).UnixMilli(), to
	}
}

func (e *Engine) dayStart(t time.Time) time.Time {
	t = t.In(e.loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, e.loc)
}

// NormRange 规范化 range 参数。
func NormRange(r string, allowToday bool) string {
	switch r {
	case "1h", "24h", "7d":
		return r
	case "today":
		if allowToday {
			return r
		}
	}
	return "24h"
}

// ── 地点辅助 ───────────────────────────────────────────────────────────────

func (e *Engine) place(ip netip.Addr) geoip.Place { return e.geo.Lookup(ip) }

func countryLabel(p geoip.Place) string {
	if p.CC == "LAN" {
		return "内网"
	}
	if p.Country == "" {
		return "未知"
	}
	return p.Country
}

func (e *Engine) banned(ip netip.Addr) bool {
	return e.bans != nil && e.bans.Banned(ip)
}

// IsSensitive 报告路径是否命中扫描特征。
func (e *Engine) IsSensitive(path string) bool {
	lp := strings.ToLower(path)
	for _, s := range *e.sensitive.Load() {
		if strings.Contains(lp, s) {
			return true
		}
	}
	return false
}

// ── IP 聚合 ────────────────────────────────────────────────────────────────

type ipAgg struct {
	ip        netip.Addr
	count     int
	errs      int
	sensitive int
	last      int64
	hosts     map[string]int
	paths     map[string]int
	sensPaths map[string]int
}

const maxPathsPerIP = 64

func (e *Engine) aggregate(recs []Record) map[netip.Addr]*ipAgg {
	m := map[netip.Addr]*ipAgg{}
	for i := range recs {
		r := &recs[i]
		a := m[r.IP]
		if a == nil {
			a = &ipAgg{ip: r.IP, hosts: map[string]int{}, paths: map[string]int{}, sensPaths: map[string]int{}}
			m[r.IP] = a
		}
		a.count++
		if isErrorStatus(r.Status) {
			a.errs++
		}
		if r.TS > a.last {
			a.last = r.TS
		}
		if r.Host != "" {
			a.hosts[r.Host]++
		}
		p := trimQuery(r.Path)
		// 只有失败的敏感路径访问才算扫描特征：扫描器探 /.env 基本拿到 404，
		// 自己用 WebDAV/后台正常打开这类文件是 200/207，不应被标可疑
		if isErrorStatus(r.Status) && e.IsSensitive(r.Path) {
			a.sensitive++
			if _, ok := a.sensPaths[p]; ok || len(a.sensPaths) < maxPathsPerIP {
				a.sensPaths[p]++
			}
		}
		if _, ok := a.paths[p]; ok || len(a.paths) < maxPathsPerIP {
			a.paths[p]++
		}
	}
	return m
}

func trimQuery(p string) string {
	if i := strings.IndexByte(p, '?'); i >= 0 {
		return p[:i]
	}
	return p
}

func (a *ipAgg) errRatio() float64 {
	if a.count == 0 {
		return 0
	}
	return float64(a.errs) / float64(a.count)
}

// isErrorStatus 判断请求是否计入错误率。499 是 nginx 的"客户端主动断开"(浏览器取消未加载完的图片、
// 拖动视频进度等),不是服务端错误也不是攻击特征,计入会把正常浏览媒体库的访客误判为可疑。
func isErrorStatus(s int) bool { return s >= 400 && s != 499 }

// level：scan（扫描）> warn（可疑）> normal。内网地址不打标。
func (e *Engine) level(a *ipAgg) string {
	if geoip.IsPrivate(a.ip) {
		return "normal"
	}
	d := e.detect
	er := a.errRatio()
	if a.sensitive >= d.ScanHits && (er >= d.ScanErrRatio || a.sensitive >= d.ScanHits*3) {
		return "scan"
	}
	// 单次命中敏感路径可能是误判（正常页面恰好含关键字），要么多次命中、要么伴随较高错误率
	if a.sensitive >= 2 || (a.sensitive > 0 && er >= d.WarnErrRatio) || (a.count >= d.WarnMinReq && er >= d.WarnErrRatio) {
		return "warn"
	}
	return "normal"
}

func topKeys(m map[string]int, n int) []string {
	type kv struct {
		k string
		v int
	}
	arr := make([]kv, 0, len(m))
	for k, v := range m {
		arr = append(arr, kv{k, v})
	}
	sort.Slice(arr, func(i, j int) bool {
		if arr[i].v != arr[j].v {
			return arr[i].v > arr[j].v
		}
		return arr[i].k < arr[j].k
	})
	out := make([]string, 0, n)
	for i := 0; i < len(arr) && i < n; i++ {
		out = append(out, arr[i].k)
	}
	return out
}

// IPRow 是 IP 统计列表的一行。
type IPRow struct {
	IP       string   `json:"ip"`
	Tag      string   `json:"tag,omitempty"`
	Geo      string   `json:"geo"`
	CC       string   `json:"cc"`
	Count    int      `json:"count"`
	ErrRatio float64  `json:"err_ratio"`
	Share    float64  `json:"share"`
	LastTS   int64    `json:"last_ts"`
	Level    string   `json:"level"`
	Host     string   `json:"host"`
	Paths    []string `json:"paths"`
	Banned   bool     `json:"banned"`
}

func (e *Engine) rowOf(a *ipAgg, total int) IPRow {
	p := e.place(a.ip)
	lv := e.level(a)
	paths := topKeys(a.paths, 3)
	if lv != "normal" && len(a.sensPaths) > 0 {
		paths = topKeys(a.sensPaths, 3)
	}
	share := 0.0
	if total > 0 {
		share = float64(a.count) / float64(total)
	}
	host := ""
	if h := topKeys(a.hosts, 1); len(h) > 0 {
		host = h[0]
	}
	return IPRow{
		IP: a.ip.String(), Tag: e.tagOf(a.ip), Geo: p.Label(), CC: p.CC, Count: a.count, ErrRatio: round3(a.errRatio()),
		Share: round3(share), LastTS: a.last, Level: lv, Host: host, Paths: paths, Banned: e.banned(a.ip),
	}
}

func round3(f float64) float64 { return float64(int(f*1000+0.5)) / 1000 }

// ipRows 计算窗口内所有 IP 的行（未排序），并返回窗口总请求数。带缓存。
func (e *Engine) ipRows(rng string) ([]IPRow, int) {
	type res struct {
		rows  []IPRow
		total int
	}
	v := e.cached("ips:"+rng, func() any {
		from, to := e.Window(rng)
		var rows []IPRow
		total := 0
		e.store.View(from, to, func(recs []Record) {
			total = len(recs)
			for _, a := range e.aggregate(recs) {
				rows = append(rows, e.rowOf(a, total))
			}
		})
		return res{rows, total}
	}).(res)
	return v.rows, v.total
}

// IPQuery 是 /api/ips 的参数。
type IPQuery struct {
	Range   string
	Q       string
	Tag     string // 标识筛选："" | "*" | "!*" | "公司" | "!公司"
	Level   string // all | scan | warn | suspicious（scan+warn）
	HideLAN bool
	Sort    string // count | err | last | ip
	Dir     string
	Page    int
	Size    int
}

// Page 是通用分页结果。
type Page[T any] struct {
	Rows  []T `json:"rows"`
	Page  int `json:"page"`
	Pages int `json:"pages"`
	Total int `json:"total"`
}

func paginate[T any](rows []T, page, size int) Page[T] {
	if size <= 0 {
		size = 10
	}
	if size > 200 {
		size = 200
	}
	pages := (len(rows) + size - 1) / size
	if pages < 1 {
		pages = 1
	}
	if page < 1 {
		page = 1
	}
	if page > pages {
		page = pages
	}
	start := (page - 1) * size
	end := start + size
	if end > len(rows) {
		end = len(rows)
	}
	out := rows[start:end]
	if out == nil {
		out = []T{}
	}
	return Page[T]{Rows: out, Page: page, Pages: pages, Total: len(rows)}
}

// IPsResult 附带窗口总请求数（前端显示“窗口请求 N”）与已配置标识列表。
type IPsResult struct {
	Page[IPRow]
	WindowTotal int      `json:"window_total"`
	Tags        []string `json:"tags"`
}

// IPs 返回 IP 统计列表。
func (e *Engine) IPs(q IPQuery) IPsResult {
	all, total := e.ipRows(NormRange(q.Range, false))
	rows := make([]IPRow, 0, len(all))
	for _, r := range all {
		if !iptag.MatchFilter(r.Tag, q.Tag) {
			continue
		}
		if !iptag.MatchQuery(r.IP, r.Tag, q.Q) {
			continue
		}
		if q.HideLAN && r.CC == "LAN" {
			continue
		}
		switch q.Level {
		case "scan", "warn":
			if r.Level != q.Level {
				continue
			}
		case "suspicious":
			if r.Level == "normal" {
				continue
			}
		}
		rows = append(rows, r)
	}
	asc := q.Dir == "asc"
	less := func(a, b IPRow) bool { return a.Count < b.Count }
	switch q.Sort {
	case "err":
		less = func(a, b IPRow) bool { return a.ErrRatio < b.ErrRatio }
	case "last":
		less = func(a, b IPRow) bool { return a.LastTS < b.LastTS }
	case "ip":
		less = func(a, b IPRow) bool { return ipLess(a.IP, b.IP) }
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if asc {
			if less(rows[i], rows[j]) != less(rows[j], rows[i]) {
				return less(rows[i], rows[j])
			}
		} else if less(rows[j], rows[i]) != less(rows[i], rows[j]) {
			return less(rows[j], rows[i])
		}
		return rows[i].IP < rows[j].IP
	})
	return IPsResult{Page: paginate(rows, q.Page, q.Size), WindowTotal: total, Tags: e.Tags()}
}

func ipLess(a, b string) bool {
	x, e1 := netip.ParseAddr(a)
	y, e2 := netip.ParseAddr(b)
	if e1 != nil || e2 != nil {
		return a < b
	}
	return x.Less(y)
}
