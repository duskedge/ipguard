package stats

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"

	"github.com/duskedge/ipguard/internal/geoip"
)

// HostQuery 是 /api/hosts 的查询参数。
type HostQuery struct {
	Range   string // 1h | today | 24h | 7d
	Q       string // 域名或后端关键字
	Host    string // 选定某个域名时，下方路径榜仅统计该域名
	HideLAN bool
	Sort    string // count | ip | bytes | err | sens | last | host
	Dir     string
	Page    int
	Size    int
}

// HostStatRow 是按域名聚合的一行。
type HostStatRow struct {
	Host     string   `json:"host"`
	Backend  string   `json:"backend"`
	Count    int      `json:"count"`
	Share    float64  `json:"share"`
	UniqueIP int      `json:"unique_ip"`
	Bytes    int64    `json:"bytes"`
	ErrCount int      `json:"err_count"`
	ErrRatio float64  `json:"err_ratio"`
	SensHits int      `json:"sens_hits"`
	LastTS   int64    `json:"last_ts"`
	TopPaths []string `json:"top_paths"`
}

// PathStatRow 是按路径聚合的一行（热门路径 / 异常路径排行）。
type PathStatRow struct {
	Host      string  `json:"host"`
	Path      string  `json:"path"`
	Count     int     `json:"count"`
	UniqueIP  int     `json:"unique_ip"`
	Bytes     int64   `json:"bytes"`
	ErrCount  int     `json:"err_count"`
	ErrRatio  float64 `json:"err_ratio"`
	TopStatus int     `json:"top_status"`
	Sensitive bool    `json:"sensitive"`
}

// HostsResult 是 /api/hosts 的完整响应。
type HostsResult struct {
	Page[HostStatRow]
	WindowTotal  int           `json:"window_total"`
	TotalBytes   int64         `json:"total_bytes"`
	TotalHosts   int           `json:"total_hosts"`
	TotalErr     int           `json:"total_err"`
	TotalSens    int           `json:"total_sens"`
	SelectedHost string        `json:"selected_host,omitempty"`
	TopPaths     []PathStatRow `json:"top_paths"`
	ErrPaths     []PathStatRow `json:"err_paths"`
}

type hostAcc struct {
	host     string
	count    int
	bytes    int64
	errs     int
	sensHits int
	last     int64
	ips      map[netip.Addr]bool
	backends map[string]int
	paths    map[string]int
}

type pathAcc struct {
	host     string
	path     string
	count    int
	bytes    int64
	errs     int
	ips      map[netip.Addr]bool
	statuses map[int]int
	errCodes map[int]int
}

const maxTrackedPaths = 2048

type hostsSnapshot struct {
	rows       []HostStatRow
	totalReq   int
	totalBytes int64
	totalErr   int
	totalSens  int
	topPaths   []PathStatRow
	errPaths   []PathStatRow
}

func (e *Engine) buildHostsSnapshot(rng string, hideLAN bool, selectedHost string) hostsSnapshot {
	key := fmt.Sprintf("hosts:%s:%v:%s", rng, hideLAN, selectedHost)
	return e.cached(key, func() any {
		from, to := e.Window(rng)
		hMap := map[string]*hostAcc{}
		pMap := map[string]*pathAcc{}
		var snap hostsSnapshot

		e.store.View(from, to, func(recs []Record) {
			for i := range recs {
				r := &recs[i]
				if hideLAN && geoip.IsPrivate(r.IP) {
					continue
				}
				snap.totalReq++
				snap.totalBytes += r.Size
				isErr := r.Status >= 400
				if isErr {
					snap.totalErr++
				}
				cleanPath := trimQuery(r.Path)
				if cleanPath == "" {
					cleanPath = "/"
				}
				isSens := isErr && e.IsSensitive(r.Path)
				if isSens {
					snap.totalSens++
				}

				hName := r.Host
				if hName == "" {
					hName = "(默认站点)"
				}
				ha := hMap[hName]
				if ha == nil {
					ha = &hostAcc{
						host:     hName,
						ips:      map[netip.Addr]bool{},
						backends: map[string]int{},
						paths:    map[string]int{},
					}
					hMap[hName] = ha
				}
				ha.count++
				ha.bytes += r.Size
				if isErr {
					ha.errs++
				}
				if isSens {
					ha.sensHits++
				}
				if r.TS > ha.last {
					ha.last = r.TS
				}
				ha.ips[r.IP] = true
				if r.Backend != "" {
					ha.backends[r.Backend]++
				}
				if _, ok := ha.paths[cleanPath]; ok || len(ha.paths) < maxPathsPerIP {
					ha.paths[cleanPath]++
				}

				// 下方路径统计支持按选定域名过滤
				if selectedHost != "" && hName != selectedHost {
					continue
				}
				pKey := cleanPath
				pHost := selectedHost
				if selectedHost == "" {
					pKey = hName + "\x00" + cleanPath
					pHost = hName
				}
				pa := pMap[pKey]
				if pa == nil {
					if len(pMap) >= maxTrackedPaths {
						continue
					}
					pa = &pathAcc{
						host:     pHost,
						path:     cleanPath,
						ips:      map[netip.Addr]bool{},
						statuses: map[int]int{},
						errCodes: map[int]int{},
					}
					pMap[pKey] = pa
				}
				pa.count++
				pa.bytes += r.Size
				pa.ips[r.IP] = true
				pa.statuses[r.Status]++
				if isErr {
					pa.errs++
					pa.errCodes[r.Status]++
				}
			}
		})

		snap.rows = make([]HostStatRow, 0, len(hMap))
		for _, ha := range hMap {
			rawBackend := ""
			if top := topKeys(ha.backends, 1); len(top) > 0 {
				rawBackend = top[0]
			}
			share := 0.0
			if snap.totalReq > 0 {
				share = float64(ha.count) / float64(snap.totalReq)
			}
			errRatio := 0.0
			if ha.count > 0 {
				errRatio = float64(ha.errs) / float64(ha.count)
			}
			topP := topKeys(ha.paths, 3)
			if topP == nil {
				topP = []string{}
			}
			snap.rows = append(snap.rows, HostStatRow{
				Host:     ha.host,
				Backend:  e.backendOf(ha.host, rawBackend),
				Count:    ha.count,
				Share:    round3(share),
				UniqueIP: len(ha.ips),
				Bytes:    ha.bytes,
				ErrCount: ha.errs,
				ErrRatio: round3(errRatio),
				SensHits: ha.sensHits,
				LastTS:   ha.last,
				TopPaths: topP,
			})
		}

		allPaths := make([]PathStatRow, 0, len(pMap))
		errPaths := make([]PathStatRow, 0, 32)
		for _, pa := range pMap {
			errRatio := 0.0
			if pa.count > 0 {
				errRatio = float64(pa.errs) / float64(pa.count)
			}
			sens := e.IsSensitive(pa.path)
			row := PathStatRow{
				Host:      pa.host,
				Path:      pa.path,
				Count:     pa.count,
				UniqueIP:  len(pa.ips),
				Bytes:     pa.bytes,
				ErrCount:  pa.errs,
				ErrRatio:  round3(errRatio),
				TopStatus: topIntKey(pa.statuses),
				Sensitive: sens,
			}
			allPaths = append(allPaths, row)
			if pa.errs > 0 {
				errRow := row
				errRow.TopStatus = topIntKey(pa.errCodes)
				errPaths = append(errPaths, errRow)
			}
		}

		sort.Slice(allPaths, func(i, j int) bool {
			if allPaths[i].Count != allPaths[j].Count {
				return allPaths[i].Count > allPaths[j].Count
			}
			if allPaths[i].Host != allPaths[j].Host {
				return allPaths[i].Host < allPaths[j].Host
			}
			return allPaths[i].Path < allPaths[j].Path
		})
		if len(allPaths) > 15 {
			allPaths = allPaths[:15]
		}
		snap.topPaths = allPaths

		sort.Slice(errPaths, func(i, j int) bool {
			if errPaths[i].ErrCount != errPaths[j].ErrCount {
				return errPaths[i].ErrCount > errPaths[j].ErrCount
			}
			if errPaths[i].Sensitive != errPaths[j].Sensitive {
				return errPaths[i].Sensitive
			}
			return errPaths[i].Path < errPaths[j].Path
		})
		if len(errPaths) > 15 {
			errPaths = errPaths[:15]
		}
		snap.errPaths = errPaths
		return snap
	}).(hostsSnapshot)
}

func topIntKey(m map[int]int) int {
	bestK, bestV := 0, -1
	for k, v := range m {
		if v > bestV || (v == bestV && k < bestK) {
			bestK, bestV = k, v
		}
	}
	return bestK
}

// HostsStats 返回按域名与路径聚合的统计结果。
func (e *Engine) HostsStats(q HostQuery) HostsResult {
	rng := NormRange(q.Range, true)
	selHost := strings.TrimSpace(q.Host)
	snap := e.buildHostsSnapshot(rng, q.HideLAN, selHost)

	kw := strings.ToLower(strings.TrimSpace(q.Q))
	rows := make([]HostStatRow, 0, len(snap.rows))
	for _, r := range snap.rows {
		if kw != "" && !strings.Contains(strings.ToLower(r.Host), kw) && !strings.Contains(strings.ToLower(r.Backend), kw) {
			continue
		}
		rows = append(rows, r)
	}

	asc := q.Dir == "asc"
	less := func(a, b HostStatRow) bool { return a.Count < b.Count }
	switch q.Sort {
	case "ip":
		less = func(a, b HostStatRow) bool { return a.UniqueIP < b.UniqueIP }
	case "bytes":
		less = func(a, b HostStatRow) bool { return a.Bytes < b.Bytes }
	case "err":
		less = func(a, b HostStatRow) bool {
			if a.ErrRatio != b.ErrRatio {
				return a.ErrRatio < b.ErrRatio
			}
			return a.ErrCount < b.ErrCount
		}
	case "sens":
		less = func(a, b HostStatRow) bool { return a.SensHits < b.SensHits }
	case "last":
		less = func(a, b HostStatRow) bool { return a.LastTS < b.LastTS }
	case "host":
		less = func(a, b HostStatRow) bool { return a.Host < b.Host }
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if asc {
			if less(rows[i], rows[j]) != less(rows[j], rows[i]) {
				return less(rows[i], rows[j])
			}
		} else if less(rows[j], rows[i]) != less(rows[i], rows[j]) {
			return less(rows[j], rows[i])
		}
		return rows[i].Host < rows[j].Host
	})

	return HostsResult{
		Page:         paginate(rows, q.Page, q.Size),
		WindowTotal:  snap.totalReq,
		TotalBytes:   snap.totalBytes,
		TotalHosts:   len(snap.rows),
		TotalErr:     snap.totalErr,
		TotalSens:    snap.totalSens,
		SelectedHost: selHost,
		TopPaths:     snap.topPaths,
		ErrPaths:     snap.errPaths,
	}
}
