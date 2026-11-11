package logparser

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestParseProxyHost(t *testing.T) {
	line := `[03/Nov/2026:04:15:58 +0000] - 200 200 - GET https app.example.net "/sw.js" [Client 198.51.100.189] [Length 1475] [Gzip -] [Sent-to 192.168.1.250] "Mozilla/5.0 (Macintosh)" "https://app.example.net:44444/sw.js"`
	e, err := Parse(line)
	if err != nil {
		t.Fatal(err)
	}
	if e.IP.String() != "198.51.100.189" || e.Host != "app.example.net" || e.Path != "/sw.js" ||
		e.Status != 200 || e.Size != 1475 || e.Backend != "192.168.1.250" || e.Method != "GET" ||
		e.UA != "Mozilla/5.0 (Macintosh)" {
		t.Fatalf("字段不对: %+v", e)
	}
	want := time.Date(2026, 11, 3, 4, 15, 58, 0, time.UTC).UnixMilli()
	if e.TS != want {
		t.Fatalf("时间 %d != %d", e.TS, want)
	}
}

func TestParseProxyHostUpstreamDiffers(t *testing.T) {
	// 上游 502、客户端看到 502；缓存字段为 MISS
	line := `[03/Nov/2026:06:19:32 +0000] MISS 502 502 - POST https a.example.com "/api?x=\"1\"" [Client 1.2.3.4] [Length 90] [Gzip 1.2] [Sent-to 10.0.0.2] "curl/8" "-"`
	e, err := Parse(line)
	if err != nil {
		t.Fatal(err)
	}
	if e.Status != 502 || e.Method != "POST" || e.Path != `/api?x="1"` || e.UA != "curl/8" {
		t.Fatalf("字段不对: %+v", e)
	}
}

func TestParseFallback(t *testing.T) {
	line := `[03/Nov/2026:02:58:13 +0000] 400 - GET http 192.0.2.1 "/" [Client 192.0.2.62] [Length 252] [Gzip -] "Mozilla/5.0 (compatible; Infrawatch/1.0)" "-"`
	e, err := Parse(line)
	if err != nil {
		t.Fatal(err)
	}
	if e.Status != 400 || e.Host != "192.0.2.1" || e.IP.String() != "192.0.2.62" || e.Backend != "" {
		t.Fatalf("字段不对: %+v", e)
	}
}

func TestParseCombined(t *testing.T) {
	line := `192.168.1.250 - - [04/Nov/2026:01:40:14 +0000] "POST /api/actions/FetchTask HTTP/2.0" 200 3 "-" "connect-go/1.16.2 (go1.24.6)"`
	e, err := Parse(line)
	if err != nil {
		t.Fatal(err)
	}
	if e.IP.String() != "192.168.1.250" || e.Method != "POST" || e.Path != "/api/actions/FetchTask" ||
		e.Status != 200 || e.Size != 3 || e.UA != "connect-go/1.16.2 (go1.24.6)" {
		t.Fatalf("字段不对: %+v", e)
	}
}

func TestParseGarbage(t *testing.T) {
	for _, l := range []string{"", "hello", "[bad] nope", `1.2.3 - - [x] "GET / HTTP/1.1" 200 1`} {
		if _, err := Parse(l); err == nil {
			t.Fatalf("应当跳过: %q", l)
		}
	}
}

func TestMatchPatterns(t *testing.T) {
	tl := &Tailer{Patterns: []string{"*_access.log"}, Exclude: []string{"fallback_stream_access.log"}}
	cases := map[string]bool{
		"proxy-host-1_access.log":       true,
		"proxy-host-12_access.log.1":    true,
		"proxy-host-12_access.log.2.gz": true,
		"proxy-host-1_error.log":        false,
		"fallback_stream_access.log":    false,
		"proxy_combined.log":            false,
	}
	for name, want := range cases {
		if got := tl.Match(name); got != want {
			t.Errorf("%s: got %v want %v", name, got, want)
		}
	}
}

func TestTailerIncremental(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "proxy-host-1_access.log")
	l1 := `[03/Nov/2026:04:15:58 +0000] - 200 200 - GET https a.example.com "/1" [Client 1.1.1.1] [Length 1] [Gzip -] [Sent-to 10.0.0.1] "ua" "-"` + "\n"
	l2 := `[03/Nov/2026:04:16:58 +0000] - 404 404 - GET https a.example.com "/2" [Client 1.1.1.2] [Length 1] [Gzip -] [Sent-to 10.0.0.1] "ua" "-"` + "\n"
	if err := os.WriteFile(p, []byte(l1+l2[:20]), 0o644); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var got []Entry
	tl := &Tailer{
		Dir: dir, Patterns: []string{"*_access.log"}, Interval: 20 * time.Millisecond,
		Since: func() time.Time { return time.Unix(0, 0) },
		Emit: func(es []Entry) {
			mu.Lock()
			got = append(got, es...)
			mu.Unlock()
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go tl.Run(ctx)

	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(got) == 1 })
	// 补全半行
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString(l2[20:])
	f.Close()
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(got) == 2 })

	// 轮转：旧文件改名 .1，新文件重新写
	if err := os.Rename(p, p+".1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(l1), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(got) == 3 })
	mu.Lock()
	if got[1].Path != "/2" || got[1].Status != 404 {
		t.Fatalf("半行拼接错误: %+v", got[1])
	}
	mu.Unlock()
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("等待超时")
}
