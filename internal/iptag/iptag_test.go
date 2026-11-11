package iptag

import (
	"net/netip"
	"testing"
)

func TestStoreLookupAndFilters(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	items, err := s.Save([]Item{
		{Target: "198.51.100.189", Tag: "公司", Note: "办公网出口"},
		{Target: "198.51.100.0/24", Tag: "某运营商网段"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("len=%d", len(items))
	}
	if got := s.Lookup(netip.MustParseAddr("198.51.100.189")); got != "公司" {
		t.Fatalf("exact lookup got %q", got)
	}
	if got := s.Lookup(netip.MustParseAddr("198.51.100.99")); got != "某运营商网段" {
		t.Fatalf("cidr lookup got %q", got)
	}
	if got := s.Lookup(netip.MustParseAddr("1.1.1.1")); got != "" {
		t.Fatalf("unmatched lookup got %q", got)
	}

	// 重新加载验证持久化
	s2, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := s2.Lookup(netip.MustParseAddr("198.51.100.189")); got != "公司" {
		t.Fatalf("reloaded lookup got %q", got)
	}

	if !MatchFilter("公司", "公司") || MatchFilter("", "公司") {
		t.Fatal("exact tag filter failed")
	}
	if MatchFilter("公司", "!公司") || !MatchFilter("家里", "!公司") || !MatchFilter("", "!公司") {
		t.Fatal("exclude tag filter failed")
	}
	if !MatchFilter("公司", "*") || MatchFilter("", "*") {
		t.Fatal("any tag filter failed")
	}
	if MatchFilter("公司", "!*") || !MatchFilter("", "!*") {
		t.Fatal("exclude all tags filter failed")
	}

	if !MatchQuery("198.51.100.189", "公司", "公司") {
		t.Fatal("query by tag should match")
	}
	if MatchQuery("198.51.100.189", "公司", "!公司") {
		t.Fatal("exclude query by tag should not match")
	}
	if !MatchQuery("1.2.3.4", "", "!公司") {
		t.Fatal("exclude query should keep non-matching row")
	}
}
