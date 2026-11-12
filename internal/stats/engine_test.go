package stats

import (
	"net/netip"
	"testing"
	"time"

	"github.com/duskedge/ipguard/internal/config"
	"github.com/duskedge/ipguard/internal/geoip"
	"github.com/duskedge/ipguard/internal/logparser"
)

type noBans struct{}

func (noBans) Banned(netip.Addr) bool { return false }
func (noBans) Version() uint64        { return 0 }

func newTestEngine(t *testing.T, now time.Time, entries []logparser.Entry) *Engine {
	t.Helper()
	st := NewStore(7*24*time.Hour, 0)
	st.now = func() time.Time { return now }
	st.Add(entries)
	loc, _ := time.LoadLocation("Asia/Shanghai")
	e := NewEngine(st, geoip.Open(""), noBans{}, Detect{
		SensitivePaths: append([]string(nil), config.DefaultSensitivePaths...),
		ScanHits:       3, ScanErrRatio: 0.6, WarnMinReq: 10, WarnErrRatio: 0.3,
	}, loc)
	e.now = func() time.Time { return now }
	return e
}

func ent(ts time.Time, ip, path string, status int) logparser.Entry {
	return logparser.Entry{TS: ts.UnixMilli(), IP: netip.MustParseAddr(ip), Method: "GET", Host: "a.example.com", Path: path, Status: status, Size: 10}
}

func TestLevelsAndLogs(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Date(2026, 11, 3, 21, 36, 0, 0, loc)
	var es []logparser.Entry
	for i, p := range []string{"/.env", "/.git/config", "/wp-login.php", "/admin"} {
		es = append(es, ent(now.Add(-time.Duration(i+1)*time.Minute), "192.0.2.214", p, 404))
	}
	for i := 0; i < 5; i++ {
		es = append(es, ent(now.Add(-time.Duration(i)*time.Hour), "198.51.100.36", "/", 200))
	}
	es = append(es, ent(now.Add(-3*time.Minute), "192.168.1.8", "/.env", 404))
	// 自己通过 WebDAV 正常打开 .env（207/200）不算扫描
	for i := 0; i < 4; i++ {
		es = append(es, ent(now.Add(-time.Duration(10+i)*time.Minute), "198.51.100.189", "/docker/ipguard/.env", 207))
	}
	// 乱序写入
	es = append(es, ent(now.Add(-30*time.Hour), "1.1.1.1", "/old", 200))
	e := newTestEngine(t, now, es)

	res := e.IPs(IPQuery{Range: "24h", Sort: "count", Dir: "desc", Page: 1, Size: 10})
	if res.Total != 4 || res.WindowTotal != 14 {
		t.Fatalf("24h IP 数/请求数不对: %d %d", res.Total, res.WindowTotal)
	}
	if res.Rows[0].IP != "198.51.100.36" || res.Rows[0].Level != "normal" {
		t.Fatalf("排序/等级不对: %+v", res.Rows[0])
	}
	lv := map[string]string{}
	for _, r := range res.Rows {
		lv[r.IP] = r.Level
	}
	if lv["192.0.2.214"] != "scan" || lv["192.168.1.8"] != "normal" || lv["198.51.100.189"] != "normal" {
		t.Fatalf("等级判定不对: %v", lv)
	}

	logs := e.Logs(LogQuery{Range: "24h", Status: "4xx", Page: 1, Size: 3})
	if logs.Total != 5 || logs.Pages != 2 || len(logs.Rows) != 3 {
		t.Fatalf("4xx 过滤/分页不对: %+v", logs)
	}
	if logs.Rows[0].TS < logs.Rows[1].TS {
		t.Fatal("默认应时间倒序")
	}
	if logs.Rows[0].Level != "scan" && logs.Rows[0].IP == "192.0.2.214" {
		t.Fatal("日志行应带等级")
	}
	if got := e.Logs(LogQuery{Range: "24h", HideLAN: true}).Total; got != 13 {
		t.Fatalf("隐藏内网后应有 13 条，得到 %d", got)
	}
	if got := e.IPs(IPQuery{Range: "24h", HideLAN: true}).Total; got != 3 {
		t.Fatalf("隐藏内网后应有 3 个 IP，得到 %d", got)
	}
	if got := e.Logs(LogQuery{Range: "7d"}).Total; got != 15 {
		t.Fatalf("7d 应有 15 条，得到 %d", got)
	}

	ov := e.Overview("24h")
	if ov.TodayRequests != 14 || len(ov.Trend.Values) != 22 || ov.Trend.Sum != 14 {
		t.Fatalf("概览不对: today=%d buckets=%d sum=%d", ov.TodayRequests, len(ov.Trend.Values), ov.Trend.Sum)
	}
	if len(ov.Anomalies) != 1 || ov.Anomalies[0].IP != "192.0.2.214" {
		t.Fatalf("异常列表不对: %+v", ov.Anomalies)
	}
	if ov.Geo.LanReq != 1 || ov.Geo.UnknownReq != 13 {
		t.Fatalf("内网/未知计数不对: %+v", ov.Geo)
	}

	d := e.IPDetail(netip.MustParseAddr("192.0.2.214"))
	if d.Counts["1h"] != 4 || d.Level != "scan" || len(d.Recent) != 4 {
		t.Fatalf("IP 明细不对: %+v", d)
	}
	if hs := e.HostsStats(HostQuery{Range: "24h", HideLAN: true, Sort: "count", Dir: "desc", Page: 1, Size: 10}); hs.TotalHosts != 1 || hs.WindowTotal != 13 || hs.TotalErr != 4 || hs.TotalSens != 3 || len(hs.Rows) != 1 || hs.Rows[0].UniqueIP != 3 || hs.Rows[0].SensHits != 3 || len(hs.ErrPaths) == 0 || !hs.ErrPaths[0].Sensitive {
		t.Fatalf("域名统计不对: %+v", hs)
	}
}

// 浏览媒体库时浏览器会取消未加载完的图片,nginx 记为 499;这不应计入错误率,否则正常访客被标"可疑"。
func TestClientClosed499NotCountedAsError(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Date(2026, 11, 12, 9, 0, 0, 0, loc)
	var es []logparser.Entry
	for i := 0; i < 20; i++ {
		status := 200
		if i%2 == 0 {
			status = 499
		}
		es = append(es, ent(now.Add(-time.Duration(i+1)*time.Minute), "203.0.113.50", "/gateway/preview/x", status))
	}
	// 对照组:同样比例但是真实的 404,应判为可疑
	for i := 0; i < 20; i++ {
		status := 200
		if i%2 == 0 {
			status = 404
		}
		es = append(es, ent(now.Add(-time.Duration(i+1)*time.Minute), "203.0.113.51", "/gateway/preview/x", status))
	}
	e := newTestEngine(t, now, es)
	lv := map[string]string{}
	for _, r := range e.IPs(IPQuery{Range: "24h", Sort: "count", Dir: "desc", Page: 1, Size: 10}).Rows {
		lv[r.IP] = r.Level
	}
	if lv["203.0.113.50"] != "normal" || lv["203.0.113.51"] != "warn" {
		t.Fatalf("499 不应计入错误率: %v", lv)
	}
}
