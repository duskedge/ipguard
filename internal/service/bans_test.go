package service

import (
	"testing"
	"time"
)

func TestParseTarget(t *testing.T) {
	ok := map[string][2]string{
		"192.0.2.214":     {"Ip", "192.0.2.214"},
		" 203.0.113.5/24": {"Range", "203.0.113.0/24"},
		"8.8.8.8/32":      {"Ip", "8.8.8.8"},
		"2001:db8::/48":   {"Range", "2001:db8::/48"},
	}
	for in, want := range ok {
		s, v, err := ParseTarget(in)
		if err != nil || s != want[0] || v != want[1] {
			t.Errorf("%q → %s %s %v", in, s, v, err)
		}
	}
	bad := map[string]error{
		"":            ErrInvalidTarget,
		"1.2.3":       ErrInvalidTarget,
		"192.168.1.8": ErrPrivateTarget,
		"10.0.0.0/8":  ErrPrivateTarget,
		"1.0.0.0/4":   ErrTooWide,
		"1.2.3.4/33":  ErrInvalidTarget,
	}
	for in, want := range bad {
		if _, _, err := ParseTarget(in); err != want {
			t.Errorf("%q: 期望 %v 得到 %v", in, want, err)
		}
	}
}

func TestParseDuration(t *testing.T) {
	cases := map[string]time.Duration{
		"": 24 * time.Hour, "1h": time.Hour, "7d": 7 * 24 * time.Hour, "90": 90 * time.Minute,
		"forever": foreverDuration, "永久": foreverDuration,
	}
	for in, want := range cases {
		if d, err := ParseDuration(in); err != nil || d != want {
			t.Errorf("%q → %v %v", in, d, err)
		}
	}
	for _, in := range []string{"abc", "0", "-1h", "100000d"} {
		if _, err := ParseDuration(in); err == nil {
			t.Errorf("%q 应报错", in)
		}
	}
}

func TestClassify(t *testing.T) {
	if classify("cscli") != "manual" || classify("CAPI") != "community" || classify("lists") != "community" || classify("crowdsec") != "auto" {
		t.Fatal("来源分类不对")
	}
}
