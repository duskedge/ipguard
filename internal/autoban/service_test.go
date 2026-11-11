package autoban

import (
	"context"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/duskedge/ipguard/internal/service"
	"github.com/duskedge/ipguard/internal/stats"
)

type fakeBanner struct {
	mu     sync.Mutex
	banned map[netip.Addr]bool
	calls  []service.BanInput
}

func newFakeBanner() *fakeBanner {
	return &fakeBanner{banned: map[netip.Addr]bool{}}
}

func (f *fakeBanner) AutoBan(_ context.Context, in service.BanInput) (service.BanResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ip, _ := netip.ParseAddr(in.Target)
	if f.banned[ip] {
		return service.BanResult{}, service.ErrAlreadyBanned
	}
	f.banned[ip] = true
	f.calls = append(f.calls, in)
	return service.BanResult{Target: in.Target, Scope: "ip", Duration: in.Duration}, nil
}

func (f *fakeBanner) Banned(ip netip.Addr) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.banned[ip.Unmap()]
}

type fakeTag map[netip.Addr]string

func (t fakeTag) Lookup(ip netip.Addr) string { return t[ip.Unmap()] }

type fakeACL map[netip.Addr]bool

func (a fakeACL) InAnyAddressSet(ip netip.Addr) bool { return a[ip.Unmap()] }

func TestGuardEscalationAndExemptions(t *testing.T) {
	banner := newFakeBanner()
	taggedIP := netip.MustParseAddr("198.51.100.10")
	allowSetIP := netip.MustParseAddr("198.51.100.11")
	lanIP := netip.MustParseAddr("192.168.1.10")
	attackerIP := netip.MustParseAddr("203.0.113.55")

	svc, err := New(t.TempDir(), banner, fakeTag{taggedIP: "公司"}, fakeACL{allowSetIP: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Save(Config{
		GuardEnabled:   true,
		GuardWindowMin: 5,
		GuardThreshold: 3,
		GuardDuration:  "24h",
		ScanEnabled:    false,
		ScanMinLevel:   "scan",
		ScanMinCount:   10,
		ScanDuration:   "7d",
		NotifyEnabled:  false,
	}); err != nil {
		t.Fatal(err)
	}

	// 豁免 IP 无论触发多少次都不会封禁
	for i := 0; i < 5; i++ {
		svc.RecordGuardBlock(lanIP, "nas.example.com", "/", "内网")
		svc.RecordGuardBlock(taggedIP, "nas.example.com", "/", "中国")
		svc.RecordGuardBlock(allowSetIP, "nas.example.com", "/", "美国")
	}

	// 攻击者触发 3 次达到阈值
	svc.RecordGuardBlock(attackerIP, "nas.example.com", "/admin", "美国")
	svc.RecordGuardBlock(attackerIP, "nas.example.com", "/admin", "美国")
	svc.RecordGuardBlock(attackerIP, "nas.example.com", "/admin", "美国")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if banner.Banned(attackerIP) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !banner.Banned(attackerIP) {
		t.Fatalf("expected attacker %s to be auto-banned", attackerIP)
	}
	if banner.Banned(taggedIP) || banner.Banned(allowSetIP) || banner.Banned(lanIP) {
		t.Fatalf("exempt IPs must never be auto-banned")
	}
	st := svc.View().Status
	if st.TotalBanned != 1 || st.GuardBanned != 1 || st.LastBanIP != attackerIP.String() {
		t.Fatalf("unexpected status: %+v", st)
	}
}

func TestScanAutoBan(t *testing.T) {
	banner := newFakeBanner()
	svc, err := New("", banner, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	svc.now = func() time.Time { return now }
	if _, err := svc.Save(Config{
		GuardEnabled:   false,
		GuardWindowMin: 5,
		GuardThreshold: 20,
		GuardDuration:  "24h",
		ScanEnabled:    true,
		ScanMinLevel:   "scan",
		ScanMinCount:   10,
		ScanDuration:   "7d",
		NotifyEnabled:  false,
	}); err != nil {
		t.Fatal(err)
	}

	svc.checkScanRows(context.Background(), []stats.IPRow{
		// 开启前的旧记录不应触发
		{IP: "203.0.113.1", Level: "scan", Count: 50, LastTS: now.UnixMilli() - 1000, Paths: []string{"/.env"}},
		// 达到阈值的新扫描记录应触发
		{IP: "203.0.113.2", Level: "scan", Count: 18, LastTS: now.UnixMilli() + 1000, Paths: []string{"/.git/config"}},
		// 有标识的 IP 不触发
		{IP: "203.0.113.3", Tag: "VPS", Level: "scan", Count: 30, LastTS: now.UnixMilli() + 1000, Paths: []string{"/.env"}},
	})

	if banner.Banned(netip.MustParseAddr("203.0.113.1")) {
		t.Fatalf("historical scan before enabling must not be banned")
	}
	if !banner.Banned(netip.MustParseAddr("203.0.113.2")) {
		t.Fatalf("expected active scanner 203.0.113.2 to be banned")
	}
	if banner.Banned(netip.MustParseAddr("203.0.113.3")) {
		t.Fatalf("tagged IP must not be banned")
	}
}

func TestSensitivePathsConfig(t *testing.T) {
	s, err := New(t.TempDir(), nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var applied []string
	s.BindSensitivePaths([]string{"/.env", "/.git"}, func(p []string) { applied = p })
	if len(applied) != 2 || s.View().SensitiveCustom {
		t.Fatalf("初始应沿用默认：%v", applied)
	}

	in := s.View().Config
	in.SensitivePaths = []string{" /.ENV ", "/admin", "/admin", ""}
	v, err := s.Save(in)
	if err != nil {
		t.Fatal(err)
	}
	if !v.SensitiveCustom || len(v.SensitivePaths) != 2 || v.SensitivePaths[1] != "/admin" || len(applied) != 2 || applied[1] != "/admin" {
		t.Fatalf("自定义列表未生效：view=%v applied=%v", v.SensitivePaths, applied)
	}

	// 与默认一致（顺序不同）时视为未自定义
	in.SensitivePaths = []string{"/.git", "/.env"}
	if v, err = s.Save(in); err != nil || v.SensitiveCustom {
		t.Fatalf("应恢复为默认：%v %v", v.SensitiveCustom, err)
	}

	in.SensitivePaths = []string{"a b"}
	if _, err = s.Save(in); err == nil {
		t.Fatal("含空白的路径应被拒绝")
	}
}
