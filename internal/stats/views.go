package stats

import (
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/duskedge/ipguard/internal/geoip"
	"github.com/duskedge/ipguard/internal/iptag"
)

// ── 访问日志 ───────────────────────────────────────────────────────────────

// LogQuery 是 /api/logs 的参数。
type LogQuery struct {
	Range   string
	Q       string // IP 或标识子串（支持 !排除）
	Tag     string // 标识筛选："" | "*" | "!*" | "公司" | "!公司"
	Host    string
	Status  string // 2xx | 3xx | 4xx | 5xx
	Path    string
	Country string
	HideLAN bool   // 隐藏内网/保留地址（CI runner 之类的内网轮询会刷屏）
	Sort    string // ts | status | size | ip
	Dir     string
	Page    int
	Size    int
}

// LogRow 是日志列表的一行。
type LogRow struct {
	ID      string `json:"id"`
	TS      int64  `json:"ts"`
	IP      string `json:"ip"`
	Tag     string `json:"tag,omitempty"`
	Geo     string `json:"geo"`
	CC      string `json:"cc"`
	Method  string `json:"method"`
	Host    string `json:"host"`
	Path    string `json:"path"`
	Status  int    `json:"status"`
	Size    int64  `json:"size"`
	UA      string `json:"ua"`
	Backend string `json:"backend"`
	Level   string `json:"level"`
	Banned  bool   `json:"banned"`
}

func statusGroup(s int) string {
	switch {
	case s >= 500:
		return "5xx"
	case s >= 400:
		return "4xx"
	case s >= 300:
		return "3xx"
	default:
		return "2xx"
	}
}

func (e *Engine) logRow(r *Record, levels map[netip.Addr]string) LogRow {
	p := e.place(r.IP)
	return LogRow{
		ID: strconv.FormatUint(r.Seq, 36), TS: r.TS, IP: r.IP.String(), Tag: e.tagOf(r.IP), Geo: p.Label(), CC: p.CC,
		Method: r.Method, Host: r.Host, Path: r.Path, Status: r.Status, Size: r.Size, UA: r.UA,
		Backend: e.backendOf(r.Host, r.Backend), Level: levels[r.IP], Banned: e.banned(r.IP),
	}
}

// levelMap 返回 24h 窗口内各 IP 的威胁等级，用于在日志行上着色。
func (e *Engine) levelMap() map[netip.Addr]string {
	return e.cached("levels", func() any {
		rows, _ := e.ipRows("24h")
		m := make(map[netip.Addr]string, len(rows))
		for _, r := range rows {
			if r.Level != "normal" {
				if ip, err := netip.ParseAddr(r.IP); err == nil {
					m[ip] = r.Level
				}
			}
		}
		return m
	}).(map[netip.Addr]string)
}

// Logs 查询访问日志。
func (e *Engine) Logs(q LogQuery) Page[LogRow] {
	from, to := e.Window(NormRange(q.Range, true))
	qip := strings.TrimSpace(q.Q)
	qtag := strings.TrimSpace(q.Tag)
	qpath := strings.ToLower(strings.TrimSpace(q.Path))
	matched := make([]Record, 0, 256)
	e.store.View(from, to, func(recs []Record) {
		for i := len(recs) - 1; i >= 0; i-- { // 倒序遍历，默认排序就不用再排
			r := &recs[i]
			if q.HideLAN && geoip.IsPrivate(r.IP) {
				continue
			}
			if q.Host != "" && r.Host != q.Host {
				continue
			}
			if q.Status != "" && statusGroup(r.Status) != q.Status {
				continue
			}
			if qtag != "" || qip != "" {
				tag := e.tagOf(r.IP)
				if !iptag.MatchFilter(tag, qtag) {
					continue
				}
				if !iptag.MatchQuery(r.IP.String(), tag, qip) {
					continue
				}
			}
			if qpath != "" && !strings.Contains(strings.ToLower(r.Path), qpath) {
				continue
			}
			if !geoip.MatchCountry(countryLabel(e.place(r.IP)), q.Country) {
				continue
			}
			matched = append(matched, *r)
		}
	})
	asc := q.Dir == "asc"
	var key func(r *Record) int64
	switch q.Sort {
	case "status":
		key = func(r *Record) int64 { return int64(r.Status) }
	case "size":
		key = func(r *Record) int64 { return r.Size }
	case "ip":
		sort.SliceStable(matched, func(i, j int) bool {
			if asc {
				return matched[i].IP.Less(matched[j].IP)
			}
			return matched[j].IP.Less(matched[i].IP)
		})
	default:
		if asc {
			for i, j := 0, len(matched)-1; i < j; i, j = i+1, j-1 {
				matched[i], matched[j] = matched[j], matched[i]
			}
		}
	}
	if key != nil {
		sort.SliceStable(matched, func(i, j int) bool {
			if asc {
				return key(&matched[i]) < key(&matched[j])
			}
			return key(&matched[i]) > key(&matched[j])
		})
	}
	pg := paginate(matched, q.Page, q.Size)
	levels := e.levelMap()
	rows := make([]LogRow, len(pg.Rows))
	for i := range pg.Rows {
		rows[i] = e.logRow(&pg.Rows[i], levels)
	}
	return Page[LogRow]{Rows: rows, Page: pg.Page, Pages: pg.Pages, Total: pg.Total}
}

// Hosts 返回筛选下拉用的域名列表（合并日志中出现过的域名与 npm-guard 已配置域名）。
func (e *Engine) Hosts() []string {
	seen := map[string]bool{}
	var out []string
	for _, h := range e.store.Hosts() {
		if h != "" && !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	if e.configuredHosts != nil {
		for _, h := range e.configuredHosts() {
			if h != "" && !seen[h] {
				seen[h] = true
				out = append(out, h)
			}
		}
	}
	sort.Strings(out)
	if out == nil {
		return []string{}
	}
	return out
}

// Countries 返回保留期内出现过的国家/地区名（筛选下拉用）。
func (e *Engine) Countries() []string {
	return e.cached("countries", func() any {
		from, to := e.Window("7d")
		set := map[string]bool{}
		e.store.View(from, to, func(recs []Record) {
			seen := map[netip.Addr]bool{}
			for i := range recs {
				if seen[recs[i].IP] {
					continue
				}
				seen[recs[i].IP] = true
				set[countryLabel(e.place(recs[i].IP))] = true
			}
		})
		out := make([]string, 0, len(set))
		for k := range set {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}).([]string)
}

// ── IP 明细 ────────────────────────────────────────────────────────────────

// IPDetail 是抽屉里展示的单 IP 明细。
type IPDetail struct {
	IP       string             `json:"ip"`
	Tag      string             `json:"tag,omitempty"`
	Geo      string             `json:"geo"`
	CC       string             `json:"cc"`
	Province string             `json:"province"`
	Counts   map[string]int     `json:"counts"`
	Errs     map[string]float64 `json:"err_ratio"`
	LastTS   int64              `json:"last_ts"`
	Level    string             `json:"level"`
	Host     string             `json:"host"`
	Hosts    []string           `json:"hosts"`
	Paths    []string           `json:"paths"`
	Banned   bool               `json:"banned"`
	Recent   []LogRow           `json:"recent"`
}

// IPDetail 返回单个 IP 的明细。
func (e *Engine) IPDetail(ip netip.Addr) IPDetail {
	p := e.place(ip)
	d := IPDetail{
		IP: ip.String(), Tag: e.tagOf(ip), Geo: p.Label(), CC: p.CC, Province: p.Province,
		Counts: map[string]int{}, Errs: map[string]float64{}, Banned: e.banned(ip), Level: "normal",
		Hosts: []string{}, Paths: []string{}, Recent: []LogRow{},
	}
	var sensPaths []string
	for _, rng := range []string{"1h", "24h", "7d"} {
		from, to := e.Window(rng)
		var mine []Record
		e.store.View(from, to, func(recs []Record) {
			for i := range recs {
				if recs[i].IP == ip {
					mine = append(mine, recs[i])
				}
			}
		})
		agg := e.aggregate(mine)[ip]
		if agg == nil {
			d.Counts[rng], d.Errs[rng] = 0, 0
			continue
		}
		d.Counts[rng], d.Errs[rng] = agg.count, round3(agg.errRatio())
		if rng == "7d" {
			d.LastTS = agg.last
			d.Hosts = topKeys(agg.hosts, 5)
			if len(d.Hosts) > 0 {
				d.Host = d.Hosts[0]
			}
			d.Paths = topKeys(agg.paths, 8)
			levels := map[netip.Addr]string{}
			for i := len(mine) - 1; i >= 0 && len(d.Recent) < 100; i-- {
				d.Recent = append(d.Recent, e.logRow(&mine[i], levels))
			}
		}
		if rng == "24h" {
			d.Level = e.level(agg)
			if d.Level != "normal" && len(agg.sensPaths) > 0 {
				sensPaths = topKeys(agg.sensPaths, 8)
			}
		}
	}
	if sensPaths != nil {
		// 可疑地址优先展示敏感路径
		d.Paths = sensPaths
	}
	return d
}

// ── 归属地 ─────────────────────────────────────────────────────────────────

// CountryRow 是国家排行的一行。
type CountryRow struct {
	Name     string  `json:"name"`
	CC       string  `json:"cc"`
	Req      int     `json:"req"`
	IP       int     `json:"ip"`
	Domestic bool    `json:"domestic"`
	Lat      float64 `json:"lat"`
	Lng      float64 `json:"lng"`
}

// ProvinceRow 是省/市排行的一行。
type ProvinceRow struct {
	Province string  `json:"province"`
	Cities   string  `json:"cities"`
	Req      int     `json:"req"`
	IP       int     `json:"ip"`
	Lat      float64 `json:"lat"`
	Lng      float64 `json:"lng"`
	City     string  `json:"city"` // 地图上标注用的主要城市
}

// GeoResult 是 /api/geo 的响应。
type GeoResult struct {
	Total       int           `json:"total"`
	UniqueIP    int           `json:"unique_ip"`
	DomesticReq int           `json:"domestic_req"`
	OverseasReq int           `json:"overseas_req"`
	LanReq      int           `json:"lan_req"`
	UnknownReq  int           `json:"unknown_req"`
	DomesticIP  int           `json:"domestic_ip"`
	Countries   []CountryRow  `json:"countries"`
	Provinces   []ProvinceRow `json:"provinces"`
}

type geoAcc struct {
	req      int
	ips      map[netip.Addr]bool
	latSum   float64
	lngSum   float64
	locN     int
	cities   map[string]int
	cityLocs map[string][2]float64
}

var municipality = map[string]bool{"北京": true, "上海": true, "天津": true, "重庆": true}

func newAcc() *geoAcc {
	return &geoAcc{ips: map[netip.Addr]bool{}, cities: map[string]int{}, cityLocs: map[string][2]float64{}}
}

func (a *geoAcc) add(ip netip.Addr, p geoip.Place) {
	a.req++
	a.ips[ip] = true
	if p.Lat != 0 || p.Lng != 0 {
		a.latSum += p.Lat
		a.lngSum += p.Lng
		a.locN++
	}
	// 城市名退回成省名的（库里只有区县名）不算“主要城市”；直辖市除外
	if p.City != "" && (p.City != p.Province || municipality[p.City]) {
		a.cities[p.City]++
		if _, ok := a.cityLocs[p.City]; !ok && (p.Lat != 0 || p.Lng != 0) {
			a.cityLocs[p.City] = [2]float64{p.Lat, p.Lng}
		}
	}
}

func (a *geoAcc) center() (float64, float64) {
	if a.locN == 0 {
		return 0, 0
	}
	return round3(a.latSum / float64(a.locN)), round3(a.lngSum / float64(a.locN))
}

// Geo 计算窗口内的归属地分布。
func (e *Engine) Geo(rng string) GeoResult {
	rng = NormRange(rng, true)
	return e.cached("geo:"+rng, func() any {
		from, to := e.Window(rng)
		return e.geoOf(from, to)
	}).(GeoResult)
}

func (e *Engine) geoOf(from, to int64) GeoResult {
	var res GeoResult
	countries := map[string]*geoAcc{}
	ccOf := map[string]string{}
	provinces := map[string]*geoAcc{}
	allIPs := map[netip.Addr]bool{}
	e.store.View(from, to, func(recs []Record) {
		res.Total = len(recs)
		for i := range recs {
			ip := recs[i].IP
			p := e.place(ip)
			allIPs[ip] = true
			switch {
			case p.CC == "LAN":
				res.LanReq++
				continue
			case p.CC == "":
				res.UnknownReq++
			case p.Domestic():
				res.DomesticReq++
			default:
				res.OverseasReq++
			}
			name := countryLabel(p)
			acc := countries[name]
			if acc == nil {
				acc = newAcc()
				countries[name] = acc
				ccOf[name] = p.CC
			}
			acc.add(ip, p)
			if p.Domestic() {
				prov := p.Province
				if prov == "" {
					prov = "其他"
				}
				pa := provinces[prov]
				if pa == nil {
					pa = newAcc()
					provinces[prov] = pa
				}
				pa.add(ip, p)
			}
		}
	})
	res.UniqueIP = len(allIPs)
	for name, a := range countries {
		lat, lng := a.center()
		cc := ccOf[name]
		res.Countries = append(res.Countries, CountryRow{Name: name, CC: cc, Req: a.req, IP: len(a.ips), Domestic: cc == "CN", Lat: lat, Lng: lng})
		if cc == "CN" {
			res.DomesticIP = len(a.ips)
		}
	}
	sort.Slice(res.Countries, func(i, j int) bool {
		if res.Countries[i].Req != res.Countries[j].Req {
			return res.Countries[i].Req > res.Countries[j].Req
		}
		return res.Countries[i].Name < res.Countries[j].Name
	})
	for prov, a := range provinces {
		top := topKeys(a.cities, 2)
		row := ProvinceRow{Province: prov, Cities: strings.Join(top, "、"), Req: a.req, IP: len(a.ips)}
		if row.Cities == "" {
			row.Cities = "—"
		}
		if len(top) > 0 {
			row.City = top[0]
			if loc, ok := a.cityLocs[top[0]]; ok {
				row.Lat, row.Lng = loc[0], loc[1]
			}
		}
		if row.Lat == 0 && row.Lng == 0 {
			row.Lat, row.Lng = a.center()
		}
		res.Provinces = append(res.Provinces, row)
	}
	sort.Slice(res.Provinces, func(i, j int) bool {
		// “其他”永远垫底
		if (res.Provinces[i].Province == "其他") != (res.Provinces[j].Province == "其他") {
			return res.Provinces[j].Province == "其他"
		}
		if res.Provinces[i].Req != res.Provinces[j].Req {
			return res.Provinces[i].Req > res.Provinces[j].Req
		}
		return res.Provinces[i].Province < res.Provinces[j].Province
	})
	if res.Countries == nil {
		res.Countries = []CountryRow{}
	}
	if res.Provinces == nil {
		res.Provinces = []ProvinceRow{}
	}
	return res
}

// ── 概览 ───────────────────────────────────────────────────────────────────

// Trend 是趋势图数据。
type Trend struct {
	Range  string   `json:"range"`
	Labels []string `json:"labels"`
	Values []int    `json:"values"`
	Hint   string   `json:"hint"`
	Sum    int      `json:"sum"`
	Peak   int      `json:"peak"`
	PeakAt string   `json:"peak_at"`
}

// Overview 是概览页数据。
type Overview struct {
	Now                  int64     `json:"now"`
	TodayRequests        int       `json:"today_requests"`
	YesterdayRequests    int       `json:"yesterday_requests"`
	ReqDeltaPct          *int      `json:"req_delta_pct"`
	UniqueIP             int       `json:"unique_ip"`
	YesterdayIP          int       `json:"yesterday_ip"`
	IPDeltaPct           *int      `json:"ip_delta_pct"`
	OverseasPct          float64   `json:"overseas_pct"`
	YesterdayOverseasPct float64   `json:"yesterday_overseas_pct"`
	Trend                Trend     `json:"trend"`
	Geo                  GeoResult `json:"geo"`
	TopIPs               []IPRow   `json:"top_ips"`
	Anomalies            []IPRow   `json:"anomalies"`
	Hosts                int       `json:"hosts"`
}

func deltaPct(cur, prev int) *int {
	if prev == 0 {
		return nil
	}
	v := int(float64(cur-prev)/float64(prev)*100 + 0.5*sign(cur-prev))
	return &v
}

func sign(x int) float64 {
	if x < 0 {
		return -1
	}
	return 1
}

func overseasPct(g GeoResult) float64 {
	if g.Total == 0 {
		return 0
	}
	return float64(int(float64(g.OverseasReq)/float64(g.Total)*1000+0.5)) / 10
}

// Overview 计算概览。昨日对比用“昨日同期”（昨天 0 点到昨天此刻），避免拿半天比整天。
func (e *Engine) Overview(trendRange string) Overview {
	trendRange = NormRange(trendRange, false)
	return e.cached("overview:"+trendRange, func() any {
		now := e.now()
		today := e.dayStart(now)
		yStart := today.AddDate(0, 0, -1)
		yNow := now.AddDate(0, 0, -1)

		tg := e.geoOf(today.UnixMilli(), now.UnixMilli()+1)
		yg := e.geoOf(yStart.UnixMilli(), yNow.UnixMilli()+1)

		ov := Overview{
			Now:                  now.UnixMilli(),
			TodayRequests:        tg.Total,
			YesterdayRequests:    yg.Total,
			ReqDeltaPct:          deltaPct(tg.Total, yg.Total),
			UniqueIP:             tg.UniqueIP,
			YesterdayIP:          yg.UniqueIP,
			IPDeltaPct:           deltaPct(tg.UniqueIP, yg.UniqueIP),
			OverseasPct:          overseasPct(tg),
			YesterdayOverseasPct: overseasPct(yg),
			Trend:                e.trend(trendRange),
			Geo:                  tg,
			Hosts:                len(e.store.Hosts()),
		}
		rows, _ := e.ipRows("24h")
		sorted := append([]IPRow(nil), rows...)
		sort.SliceStable(sorted, func(i, j int) bool {
			if sorted[i].Count != sorted[j].Count {
				return sorted[i].Count > sorted[j].Count
			}
			return sorted[i].IP < sorted[j].IP
		})
		ov.TopIPs = []IPRow{}
		ov.Anomalies = []IPRow{}
		for _, r := range sorted {
			// Top IP 只看外部来源；内网请求量在归属地的“内网”一栏里体现
			if len(ov.TopIPs) < 6 && r.CC != "LAN" {
				ov.TopIPs = append(ov.TopIPs, r)
			}
			if r.Level != "normal" && !r.Banned && len(ov.Anomalies) < 8 {
				ov.Anomalies = append(ov.Anomalies, r)
			}
		}
		return ov
	}).(Overview)
}

func (e *Engine) trend(rng string) Trend {
	now := e.now().In(e.loc)
	t := Trend{Range: rng}
	var starts []time.Time
	switch rng {
	case "1h":
		// 12 个 5 分钟桶，最后一个桶包含此刻
		end := now.Truncate(5 * time.Minute)
		for i := 11; i >= 0; i-- {
			starts = append(starts, end.Add(-time.Duration(i)*5*time.Minute))
		}
		t.Hint = "每 5 分钟"
		for _, s := range starts {
			t.Labels = append(t.Labels, s.Format("15:04"))
		}
	case "7d":
		day := e.dayStart(now)
		for i := 6; i >= 0; i-- {
			starts = append(starts, day.AddDate(0, 0, -i))
		}
		t.Hint = "按天"
		for _, s := range starts {
			t.Labels = append(t.Labels, s.Format("01-02"))
		}
	default:
		day := e.dayStart(now)
		for h := 0; h <= now.Hour(); h++ {
			starts = append(starts, day.Add(time.Duration(h)*time.Hour))
		}
		t.Hint = "今天 0 点至今，按小时"
		for _, s := range starts {
			t.Labels = append(t.Labels, fmt.Sprintf("%02d:00", s.Hour()))
		}
	}
	t.Values = make([]int, len(starts))
	bounds := make([]int64, len(starts)+1)
	for i, s := range starts {
		bounds[i] = s.UnixMilli()
	}
	bounds[len(starts)] = now.UnixMilli() + 1
	e.store.View(bounds[0], bounds[len(starts)], func(recs []Record) {
		b := 0
		for i := range recs {
			for b < len(starts)-1 && recs[i].TS >= bounds[b+1] {
				b++
			}
			t.Values[b]++
		}
	})
	for i, v := range t.Values {
		t.Sum += v
		if v > t.Peak {
			t.Peak, t.PeakAt = v, t.Labels[i]
		}
	}
	return t
}
