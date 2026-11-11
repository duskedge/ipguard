package geoip

import (
	"net/netip"
	"testing"
)

func TestMissingDBDegrades(t *testing.T) {
	r := Open("/nonexistent/city.mmdb")
	if r.Loaded() {
		t.Fatal("不应加载")
	}
	p := r.Lookup(netip.MustParseAddr("8.8.8.8"))
	if p.Label() != "未知" || p.CC != "" {
		t.Fatalf("应降级为未知: %+v", p)
	}
	lan := r.Lookup(netip.MustParseAddr("192.168.1.8"))
	if lan.CC != "LAN" || lan.Label() != "内网" {
		t.Fatalf("内网识别错误: %+v", lan)
	}
}

func TestLabel(t *testing.T) {
	cases := []struct {
		p    Place
		want string
	}{
		{Place{CC: "CN", Country: "中国", Province: "浙江", City: "杭州"}, "中国·杭州"},
		{Place{CC: "SG", Country: "新加坡", City: "新加坡"}, "新加坡"},
		{Place{CC: "US", Country: "美国", Province: "Virginia"}, "美国·Virginia"},
		{Place{CC: "LAN"}, "内网"},
		{Place{}, "未知"},
	}
	for _, c := range cases {
		if got := c.p.Label(); got != c.want {
			t.Errorf("%+v: got %q want %q", c.p, got, c.want)
		}
	}
}

func TestCleanNames(t *testing.T) {
	if cleanProvince("浙江省") != "浙江" || cleanProvince("广西壮族自治区") != "广西" || cleanProvince("北京市") != "北京" {
		t.Fatal("省名清洗错误")
	}
	if cleanCity("杭州市") != "杭州" || cleanCity("Hangzhou") != "Hangzhou" {
		t.Fatal("市名清洗错误")
	}
}

func TestParseRegion(t *testing.T) {
	cases := map[string]cnRegion{
		"中国|浙江省|杭州市|移动|CN":     {"浙江", "杭州", "移动"},
		"中国|0|浙江省|杭州市|阿里":      {"浙江", "杭州", "阿里"},
		"中国|北京|北京市|联通|CN":      {"北京", "北京", "联通"},
		"中国|广西壮族自治区|南宁市|电信|CN": {"广西", "南宁", "电信"},
		"中国|浙江省|0|0|CN":        {"浙江", "", ""},
	}
	for in, want := range cases {
		got, ok := parseRegion(in)
		if !ok || got != want {
			t.Errorf("%q → %+v %v", in, got, ok)
		}
	}
	for _, in := range []string{"中国|香港|0|0|HK", "Netherlands|South Holland|0|0|NL", "0|0|0|内网IP|内网IP", "bad"} {
		if _, ok := parseRegion(in); ok {
			t.Errorf("%q 应返回 false", in)
		}
	}
}

func TestLabelWithISP(t *testing.T) {
	p := Place{CC: "CN", Country: "中国", Province: "浙江", City: "杭州", ISP: "移动"}
	if p.Label() != "中国·杭州 · 移动" {
		t.Fatal(p.Label())
	}
}
