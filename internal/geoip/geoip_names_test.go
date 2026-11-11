package geoip

import "testing"

func TestMatchCountry(t *testing.T) {
	cases := []struct {
		label, want string
		ok          bool
	}{
		{"中国", "", true},
		{"中国", ForeignFilter, false},
		{"内网", ForeignFilter, false},
		{"美国", ForeignFilter, true},
		{"中国香港", ForeignFilter, true},
		{"美国", "美国", true},
		{"美国", "德国", false},
	}
	for _, c := range cases {
		if got := MatchCountry(c.label, c.want); got != c.ok {
			t.Errorf("MatchCountry(%q,%q)=%v", c.label, c.want, got)
		}
	}
}
