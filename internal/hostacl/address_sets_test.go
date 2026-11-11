package hostacl

import (
	"fmt"
	"github.com/duskedge/ipguard/internal/iptag"
	"net/netip"
	"sync"
	"testing"
)

func TestAddressSetReferencesAndPersistence(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	sets, err := s.SaveAddressSets([]AddressSet{{Name: "服务器", IPs: []string{"192.0.2.151", "8.8.8.0/24", "192.0.2.151"}}})
	if err != nil {
		t.Fatal(err)
	}
	id := sets[0].ID
	if len(sets[0].IPs) != 2 {
		t.Fatal("未去重")
	}
	rules := []Rule{{Host: "a.example.com", Enabled: true, Mode: ModeAllowlist, AllowSets: []string{id}}, {Host: "b.example.com", Enabled: true, Mode: ModeDomestic, AllowSets: []string{id}, AllowIPs: []string{"1.1.1.1"}}}
	if _, err = s.Save(rules); err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"a.example.com", "b.example.com"} {
		for _, ip := range []string{"192.0.2.151", "8.8.8.8"} {
			if !s.Decide(h, netip.MustParseAddr(ip)).Allowed {
				t.Fatal("集合未放行", h, ip)
			}
		}
	}
	d := s.Decide("a.example.com", netip.MustParseAddr("9.9.9.9"))
	if d.Allowed || len(d.AllowIPs) != 2 {
		t.Fatal("拒绝快照未保留集合地址", d)
	}
	if _, err = s.SaveAddressSets(nil); err == nil {
		t.Fatal("不应删除引用中的集合")
	}
	if len(s.AddressSets()) != 1 {
		t.Fatal("失败写入改变状态")
	}
	sets[0].Name = "可信服务器"
	sets[0].IPs = []string{"9.9.9.9"}
	if _, err = s.SaveAddressSets([]AddressSet{sets[0].AddressSet}); err != nil {
		t.Fatal(err)
	}
	if s.Decide("a.example.com", netip.MustParseAddr("192.0.2.151")).Allowed {
		t.Fatal("旧地址仍被放行")
	}
	if !s.Decide("a.example.com", netip.MustParseAddr("9.9.9.9")).Allowed {
		t.Fatal("新地址未生效")
	}
	s, err = New(dir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.AddressSets()[0].ID != id || !s.Decide("a.example.com", netip.MustParseAddr("9.9.9.9")).Allowed {
		t.Fatal("重启未恢复集合引用")
	}
	if _, err = s.Save([]Rule{{Host: "bad.example.com", Mode: ModeDomestic, AllowSets: []string{"missing"}}}); err == nil {
		t.Fatal("接受了无效引用")
	}
	if _, err = s.Save(nil); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveAddressSets(nil); err != nil {
		t.Fatal(err)
	}
}

func TestAddressSetValidation(t *testing.T) {
	s, _ := New("", nil, nil)
	for _, sets := range [][]AddressSet{
		{{Name: "空集合"}},
		{{Name: "错误地址", IPs: []string{"bad-ip"}}},
		{{Name: "重复", IPs: []string{"8.8.8.8"}}, {Name: "重复", IPs: []string{"1.1.1.1"}}},
	} {
		if _, err := s.SaveAddressSets(sets); err == nil {
			t.Fatal("应拒绝无效集合", sets)
		}
	}
}

func TestAddressSetTags(t *testing.T) {
	dir := t.TempDir()
	tags, err := iptag.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tags.Save([]iptag.Item{{Target: "8.8.8.0/24", Tag: "单独配置"}}); err != nil {
		t.Fatal(err)
	}
	s, err := New(dir, nil, tags)
	if err != nil {
		t.Fatal(err)
	}
	version := tags.Version()
	sets, err := s.SaveAddressSets([]AddressSet{{Name: "服务器", Tag: "服务器标识", IPs: []string{"8.8.8.8", "9.9.9.0/24"}}})
	if err != nil {
		t.Fatal(err)
	}
	if tags.Version() <= version {
		t.Fatal("未失效统计缓存")
	}
	if tags.Lookup(netip.MustParseAddr("8.8.8.8")) != "单独配置" {
		t.Fatal("覆盖单独标识")
	}
	if tags.Lookup(netip.MustParseAddr("9.9.9.9")) != "服务器标识" {
		t.Fatal("集合网段未继承标识")
	}
	if len(tags.List()) != 1 || len(tags.Tags()) != 2 {
		t.Fatal("派生标识污染手工配置或未加入筛选")
	}
	// 单独标识保存不能清除派生标识。
	if _, err = tags.Save(nil); err != nil {
		t.Fatal(err)
	}
	if tags.Lookup(netip.MustParseAddr("8.8.8.8")) != "服务器标识" {
		t.Fatal("派生标识丢失")
	}
	// 可仅通过标识放行另一个域名。
	if _, err = s.Save([]Rule{{Host: "tag.example.com", Enabled: true, Mode: ModeAllowlist, AllowTags: []string{"服务器标识"}}}); err != nil {
		t.Fatal(err)
	}
	if !s.Decide("tag.example.com", netip.MustParseAddr("9.9.9.9")).Allowed {
		t.Fatal("标识放行未生效")
	}
	tags, err = iptag.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	s, err = New(dir, nil, tags)
	if err != nil {
		t.Fatal(err)
	}
	if tags.Lookup(netip.MustParseAddr("9.9.9.9")) != "服务器标识" {
		t.Fatal("重启丢失派生标识")
	}
	a := sets[0].AddressSet
	a.Tag = "新标识"
	a.IPs = []string{"1.1.1.1"}
	if _, err = s.SaveAddressSets([]AddressSet{a}); err != nil {
		t.Fatal(err)
	}
	if tags.Lookup(netip.MustParseAddr("9.9.9.9")) != "" || tags.Lookup(netip.MustParseAddr("1.1.1.1")) != "新标识" {
		t.Fatal("未同步标识更新")
	}
	a.Tag = "!非法标识"
	if _, err = s.SaveAddressSets([]AddressSet{a}); err == nil {
		t.Fatal("接受非法标识")
	}
	if _, err = s.SaveAddressSets(nil); err != nil {
		t.Fatal(err)
	}
	if len(tags.Tags()) != 0 {
		t.Fatal("删除集合后残留标识")
	}
}

func TestAddAddressSetIP(t *testing.T) {
	dir := t.TempDir()
	tags, _ := iptag.New(dir)
	s, err := New(dir, nil, tags)
	if err != nil {
		t.Fatal(err)
	}
	sets, err := s.SaveAddressSets([]AddressSet{{Name: "常用", Tag: "可信", IPs: []string{"8.8.8.0/24"}, Note: "保留备注"}, {Name: "其他", IPs: []string{"1.1.1.1"}}})
	if err != nil {
		t.Fatal(err)
	}
	id := sets[0].ID
	if _, err = s.Save([]Rule{{Host: "add.example.com", Enabled: true, Mode: ModeAllowlist, AllowSets: []string{id}}}); err != nil {
		t.Fatal(err)
	}
	for _, ip := range []string{"8.8.8.8", "8.8.8.0"} {
		if added, err := s.AddAddressSetIP(id, ip); err != nil || added {
			t.Fatal("重复网段地址不应追加", ip, err)
		}
	}
	if _, err = s.AddAddressSetIP("missing", "9.9.9.9"); err == nil {
		t.Fatal("接受不存在的集合")
	}
	if _, err = s.AddAddressSetIP(id, "bad-ip"); err == nil {
		t.Fatal("接受错误 IP")
	}
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 1; i <= 20; i++ {
		wg.Add(1)
		go func(n int) { defer wg.Done(); _, err := s.AddAddressSetIP(id, fmt.Sprintf("9.9.9.%d", n)); errs <- err }(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	got := s.AddressSets()
	if len(got[0].IPs) != 21 || got[0].Note != "保留备注" || len(got[1].IPs) != 1 {
		t.Fatal("并发追加丢失地址或改动其他配置", got)
	}
	if tags.Lookup(netip.MustParseAddr("9.9.9.1")) != "可信" || !s.Decide("add.example.com", netip.MustParseAddr("9.9.9.1")).Allowed {
		t.Fatal("未同步标识和引用域名")
	}
	if added, err := s.AddAddressSetIP(id, "9.9.9.1"); err != nil || added {
		t.Fatal("重复 IP 应幂等")
	}
	s, err = New(dir, nil, tags)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.AddressSets()[0].IPs) != 21 {
		t.Fatal("未持久化")
	}
}
