package npmconf

import (
	"os"
	"path/filepath"
	"testing"
)

const sample = `# ------------------------------------------------------------
# hub.example.com
# ------------------------------------------------------------
map $scheme $hsts_header {
    https   "max-age=63072000; preload";
}

server {
  set $forward_scheme http;
  set $server         "192.168.1.250";
  set $port           28080;

  listen 80;
  server_name hub.example.com App.example.net;

  location / {
    include conf.d/include/proxy.conf;
  }
}
`

func TestResolve(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "65.conf"), []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	r := New(dir, ParseAliases("192.168.1.250=HOST, bad"))
	if r.Count() != 2 {
		t.Fatalf("应解析出 2 个域名，得到 %d", r.Count())
	}
	if got := r.Label("hub.example.com", "192.168.1.250"); got != "HOST:28080" {
		t.Fatalf("got %q", got)
	}
	if got := r.Label("app.example.net", ""); got != "HOST:28080" {
		t.Fatalf("域名大小写/多域名: got %q", got)
	}
	// 未配置的域名退回原始值（仍套别名）
	if got := r.Label("unknown.example.com", "192.168.1.250"); got != "HOST" {
		t.Fatalf("got %q", got)
	}
	// 日志里的 server 与配置不一致（配置已改），以日志为准
	if got := r.Label("hub.example.com", "10.0.0.9"); got != "10.0.0.9" {
		t.Fatalf("got %q", got)
	}
}

func TestMissingDir(t *testing.T) {
	r := New("/nonexistent", nil)
	if r.Label("a.com", "1.2.3.4") != "1.2.3.4" || r.Label("a.com", "") != "" {
		t.Fatal("目录不存在时应原样返回")
	}
}
