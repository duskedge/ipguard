package hostacl

import (
	"net/netip"
	"testing"

	"github.com/duskedge/ipguard/internal/geoip"
	"github.com/duskedge/ipguard/internal/iptag"
)

func TestHostACLCheckAndPersist(t *testing.T) {
	dir := t.TempDir()
	tags, err := iptag.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tags.Save([]iptag.Item{{Target: "198.51.100.189", Tag: "公司"}}); err != nil {
		t.Fatal(err)
	}

	geo := geoip.Open()
	store, err := New(dir, geo, tags)
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.Save([]Rule{
		{
			Host:      "emby.example.com",
			Enabled:   true,
			Mode:      ModeAllowlist,
			AllowTags: []string{"公司"},
			AllowIPs:  []string{"1.2.3.4"},
			Note:      "仅公司和指定 IP",
		},
		{
			Host:     "*.internal.example.com",
			Enabled:  true,
			Mode:     ModeDomestic,
			AllowIPs: []string{"8.8.8.8"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// 内网始终放行
	if ok, _ := store.Check("emby.example.com", netip.MustParseAddr("192.168.1.10")); !ok {
		t.Fatal("LAN should be allowed")
	}
	// 命中“公司”标识放行
	if ok, _ := store.Check("emby.example.com", netip.MustParseAddr("198.51.100.189")); !ok {
		t.Fatal("tagged IP should be allowed")
	}
	// 命中额外白名单 IP 放行
	if ok, _ := store.Check("emby.example.com:443", netip.MustParseAddr("1.2.3.4")); !ok {
		t.Fatal("explicit allow_ip should be allowed")
	}
	// 陌生公网 IP 拦截
	if ok, _ := store.Check("emby.example.com", netip.MustParseAddr("9.9.9.9")); ok {
		t.Fatal("unknown IP should be denied")
	}
	// 未配置规则的域名默认放行
	if ok, _ := store.Check("public.example.com", netip.MustParseAddr("9.9.9.9")); !ok {
		t.Fatal("unconfigured host should be allowed")
	}
	// 通配符规则命中
	if ok, _ := store.Check("a.internal.example.com", netip.MustParseAddr("8.8.8.8")); !ok {
		t.Fatal("wildcard allow_ip should be allowed")
	}

	list := store.List()
	if len(list) != 2 || list[0].BlockedCount != 1 || list[0].LastBlockedIP != "9.9.9.9" {
		t.Fatalf("unexpected stats: %+v", list)
	}
}

func TestMultiHostRuleAndRouting(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir, geoip.Open(), nil)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := store.Save([]Rule{
		{
			Hosts:    []string{"emby.example.com", "jellyfin.example.com", "emby.example.com"},
			Enabled:  true,
			Mode:     ModeAllowlist,
			AllowIPs: []string{"1.2.3.4"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != 1 || len(saved[0].Hosts) != 2 || saved[0].Host != "emby.example.com, jellyfin.example.com" {
		t.Fatalf("unexpected saved rule: %+v", saved)
	}
	routes := store.RoutingRules()
	if len(routes) != 2 || routes[0].Host != "emby.example.com" || routes[1].Host != "jellyfin.example.com" {
		t.Fatalf("unexpected routing rules: %+v", routes)
	}
	if ok, _ := store.Check("emby.example.com", netip.MustParseAddr("1.2.3.4")); !ok {
		t.Fatal("emby should be allowed for 1.2.3.4")
	}
	if ok, _ := store.Check("jellyfin.example.com", netip.MustParseAddr("1.2.3.4")); !ok {
		t.Fatal("jellyfin should be allowed for 1.2.3.4")
	}
	if ok, _ := store.Check("emby.example.com", netip.MustParseAddr("9.9.9.9")); ok {
		t.Fatal("emby should deny 9.9.9.9")
	}
	if ok, _ := store.Check("jellyfin.example.com", netip.MustParseAddr("8.8.8.8")); ok {
		t.Fatal("jellyfin should deny 8.8.8.8")
	}
	list := store.List()
	if len(list) != 1 || list[0].BlockedCount != 2 || list[0].LastBlockedIP != "8.8.8.8" {
		t.Fatalf("unexpected aggregated stats: %+v", list)
	}
	// 跨规则重复域名应拒绝
	if _, err := store.Save([]Rule{
		{Hosts: []string{"emby.example.com", "jellyfin.example.com"}, Enabled: true, Mode: ModeDomestic},
		{Hosts: []string{"jellyfin.example.com"}, Enabled: true, Mode: ModeDomestic},
	}); err == nil {
		t.Fatal("duplicate domain across rules should fail")
	}
}

func TestDecisionKeepsRuleSnapshot(t *testing.T) {
	s, err := New(t.TempDir(), geoip.Open(), nil)
	if err != nil {
		t.Fatal(err)
	}
	rules := []Rule{{Host: "*.example.com", Enabled: true, Mode: ModeDomestic, AllowTags: []string{"公司"}}}
	if _, err := s.Save(rules); err != nil {
		t.Fatal(err)
	}
	d := s.Decide("hub.example.com", netip.MustParseAddr("9.9.9.9"))
	if d.Allowed || d.RuleHost != "*.example.com" || d.Mode != ModeDomestic || d.Reason != "无法确认来源为中国大陆，且未命中额外放行的 IP / 网段或标识" {
		t.Fatalf("unexpected decision: %+v", d)
	}
	rules[0].Enabled = false
	if _, err := s.Save(rules); err != nil {
		t.Fatal(err)
	}
	if !s.Decide("hub.example.com", netip.MustParseAddr("9.9.9.9")).Allowed {
		t.Fatal("disabled rule should allow")
	}
	if d.AllowTags[0] != "公司" || d.Mode != ModeDomestic {
		t.Fatal("historical decision changed")
	}
	rules[0].Enabled = true
	rules[0].Mode = ModeAllowlist
	if _, err := s.Save(rules); err != nil {
		t.Fatal(err)
	}
	d = s.Decide("hub.example.com", netip.MustParseAddr("9.9.9.9"))
	if d.Allowed || d.Reason != "未命中允许的 IP / 网段或 IP 标识" {
		t.Fatalf("unexpected allowlist decision: %+v", d)
	}
}
