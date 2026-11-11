package hostacl

import (
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestCumulativeStatsSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	seed := func() ([]RuleTotal, error) {
		calls++
		return []RuleTotal{{Host: "hub.example.com", Count: 2, LastIP: "8.8.8.8", LastAt: 1}}, nil
	}
	if err := s.BootstrapStats(seed); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save([]Rule{{Host: "hub.example.com", Enabled: true, Mode: ModeDomestic}}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		s.Check("hub.example.com", netip.MustParseAddr("9.9.9.9"))
	}
	if err := s.FlushStats(); err != nil {
		t.Fatal(err)
	}
	restored, err := New(dir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := restored.BootstrapStats(seed); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("seed repeated %d times", calls)
	}
	row := restored.List()[0]
	if row.BlockedCount != 7 || row.LastBlockedIP != "9.9.9.9" || row.LastBlockedAt <= 1 {
		t.Fatalf("restart lost stats: %+v", row)
	}
	if _, err := restored.Save([]Rule{{Host: "hub.example.com", Enabled: false, Mode: ModeDomestic}}); err != nil {
		t.Fatal(err)
	}
	if restored.List()[0].BlockedCount != 7 {
		t.Fatal("editing rule reset total")
	}
	mode, err := os.Stat(filepath.Join(dir, "host_rule_stats.json"))
	if err != nil {
		t.Fatal(err)
	}
	if mode.Mode().Perm() != 0600 {
		t.Fatalf("stats permissions: %o", mode.Mode().Perm())
	}
}

func TestCumulativeStatsConcurrentFlushAndCorruptFile(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Save([]Rule{{Host: "hub.example.com", Enabled: true, Mode: ModeDomestic}}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				s.Check("hub.example.com", netip.MustParseAddr("9.9.9.9"))
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 10; j++ {
			if err := s.FlushStats(); err != nil {
				t.Error(err)
			}
		}
	}()
	wg.Wait()
	if err = s.FlushStats(); err != nil {
		t.Fatal(err)
	}
	restored, err := New(dir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if restored.List()[0].BlockedCount != 400 {
		t.Fatalf("count %d", restored.List()[0].BlockedCount)
	}
	if err = os.WriteFile(filepath.Join(dir, "host_rule_stats.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = New(dir, nil, nil); err == nil {
		t.Fatal("corrupt stats silently reset")
	}
}
