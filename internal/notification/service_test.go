package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/duskedge/ipguard/internal/stats"
)

func TestRenderEscapesValues(t *testing.T) {
	input := "路径含\"引号\"\n下一行\\test #{ip}"
	b, err := render(`{"text":"#{content}","nested":["#{ip}"],"number":1}`, map[string]string{"content": input, "ip": "203.0.113.1"})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Text   string
		Nested []string
		Number int
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Text != input || got.Nested[0] != "203.0.113.1" || got.Number != 1 {
		t.Fatalf("模板转义或替换错误: %s", b)
	}
}

func TestSaveProtectsHeadersAndIsAtomic(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	c := s.View().Config
	c.URL = "https://push.example.com/api/v1/message" // 默认不预置接收地址
	c.Headers = "Authorization: Bearer private-token"
	view, err := s.Save(Input{Config: c})
	if err != nil {
		t.Fatal(err)
	}
	if view.Headers != "" || !view.HasHeaders {
		t.Fatal("凭据不得回传")
	}
	st, err := os.Stat(s.path)
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatal("凭据文件必须仅服务用户可读写")
	}
	view.Config.CooldownMin = 60
	if _, err := s.Save(Input{Config: view.Config}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.state.Config.Headers != c.Headers || reloaded.state.Config.CooldownMin != 60 {
		t.Fatal("留空保存和重启应保留凭据")
	}
	c.CooldownMin = 0
	if _, err := s.Save(Input{Config: c}); err == nil {
		t.Fatal("应拒绝无效间隔")
	}
	if s.View().CooldownMin != 60 {
		t.Fatal("校验失败不得修改已有配置")
	}
	c.CooldownMin = 30
	if _, err := s.Save(Input{Config: c, ClearHeaders: true}); err != nil {
		t.Fatal(err)
	}
	if s.View().HasHeaders {
		t.Fatal("显式清除请求头应生效")
	}
}

func TestDeliveryCooldownEscalationAndRestart(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer test" {
			t.Error("缺少认证头")
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["msg_type"] != "news" {
			t.Error("消息格式错误")
		}
		w.Write([]byte(`{"code":200}`))
	}))
	defer server.Close()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := s.View().Config
	c.Enabled, c.URL, c.Headers = true, server.URL, "Authorization: Bearer test"
	if _, err := s.Save(Input{Config: c}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(time.Second)
	row := stats.IPRow{IP: "203.0.113.1", Geo: "测试", Level: "warn", LastTS: now.UnixMilli(), Count: 30, ErrRatio: .5}
	check := func(r stats.IPRow, at time.Time, want int32) {
		t.Helper()
		s.check(context.Background(), []stats.IPRow{r}, at)
		if got := calls.Load(); got != want {
			t.Fatalf("推送次数 got=%d want=%d", got, want)
		}
	}
	old := row
	old.LastTS = s.since - 1
	check(old, now, 0)
	banned := row
	banned.Banned = true
	check(banned, now, 0)
	lan := row
	lan.CC = "LAN"
	check(lan, now, 0)
	normal := row
	normal.Level = "normal"
	check(normal, now, 0)
	check(row, now, 1)
	row.LastTS++
	check(row, now.Add(time.Minute), 1)
	row.Level = "scan"
	check(row, now.Add(2*time.Minute), 2)
	check(row, now.Add(time.Hour), 2) // 没有新请求不得重复推送
	row.LastTS++
	check(row, now.Add(time.Hour), 3)
	restarted, err := New(strings.TrimSuffix(s.path, "/notifications.json"))
	if err != nil {
		t.Fatal(err)
	}
	restarted.since = 0
	restarted.check(context.Background(), []stats.IPRow{row}, now.Add(2*time.Hour))
	if calls.Load() != 3 {
		t.Fatal("持久化投递记录应阻止重启重复推送")
	}
}

func TestFailureRetryAndResponseValidation(t *testing.T) {
	var calls atomic.Int32
	fail := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if fail {
			w.Write([]byte(`{"success":false}`))
		} else {
			w.Write([]byte(`{"success":true}`))
		}
	}))
	defer server.Close()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := s.View().Config
	c.URL = server.URL
	c.Enabled = true
	if _, err := s.Save(Input{Config: c}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(time.Second)
	row := stats.IPRow{IP: "203.0.113.1", Level: "scan", LastTS: now.UnixMilli()}
	s.check(context.Background(), []stats.IPRow{row}, now)
	if s.View().Status.Error == "" || s.View().Status.LastSuccess != 0 {
		t.Fatal("业务失败不得标记成功")
	}
	fail = false
	s.check(context.Background(), []stats.IPRow{row}, now.Add(time.Minute))
	if calls.Load() != 2 || s.View().Status.Error != "" || s.View().Status.LastSuccess == 0 {
		t.Fatal("失败后应重试")
	}
}

func TestRedirectDoesNotForwardCredentials(t *testing.T) {
	var leaked atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Store(true) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer redirect.Close()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := s.View().Config
	c.URL = redirect.URL
	c.Headers = "Authorization: Bearer test"
	if err := s.Test(context.Background(), Input{Config: c}); err == nil {
		t.Fatal("应提示重定向状态")
	}
	if leaked.Load() {
		t.Fatal("不得跟随重定向发送凭据")
	}
	for _, raw := range []string{"Invalid", "Authorization: Bearer test\rInjected: bad", "Host: example.com"} {
		if _, err := parseHeaders(raw); err == nil {
			t.Fatalf("应拒绝请求头 %q", raw)
		}
	}
}

func TestBatchLimitDoesNotStarveLaterIPs(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := s.View().Config
	c.URL, c.Enabled, c.MinLevel = server.URL, true, "scan"
	if _, err := s.Save(Input{Config: c}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(time.Second)
	var rows []stats.IPRow
	for i := 1; i <= 21; i++ {
		rows = append(rows, stats.IPRow{IP: fmt.Sprintf("203.0.113.%d", i), Level: "scan", LastTS: now.UnixMilli()})
	}
	s.check(context.Background(), rows, now)
	if calls.Load() != 20 {
		t.Fatal("每轮最多推送 20 次")
	}
	if s.state.Sent[rows[20].IP].AttemptAt != 0 {
		t.Fatal("第 21 个地址应留到下一轮")
	}
	s.check(context.Background(), rows, now.Add(time.Minute))
	if s.state.Sent[rows[20].IP].AttemptAt == 0 {
		t.Fatal("失败地址不得阻塞后续异常")
	}
	warn := stats.IPRow{IP: "203.0.113.99", Level: "warn", LastTS: now.UnixMilli()}
	before := calls.Load()
	s.check(context.Background(), []stats.IPRow{warn}, now)
	if calls.Load() != before {
		t.Fatal("仅扫描范围不应推送可疑事件")
	}
}
