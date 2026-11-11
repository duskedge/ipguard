// Package notification 提供异常通知配置、模板渲染与后台推送。
package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/duskedge/ipguard/internal/stats"
)

const DefaultBody = `{
  "msg_type": "news",
  "title": "#{title}",
  "content": "#{content}",
  "url": "#{url}"
}`

type Config struct {
	Enabled     bool   `json:"enabled"`
	URL         string `json:"url"`
	Headers     string `json:"headers"`
	Body        string `json:"body"`
	DetailURL   string `json:"detail_url"`
	CooldownMin int    `json:"cooldown_min"`
	MinLevel    string `json:"min_level"`
}

type Input struct {
	Config
	ClearHeaders bool `json:"clear_headers"`
}

type Status struct {
	LastAttempt int64  `json:"last_attempt"`
	LastSuccess int64  `json:"last_success"`
	Error       string `json:"error"`
}

type View struct {
	Config
	HasHeaders bool   `json:"has_headers"`
	Status     Status `json:"status"`
}

type delivery struct {
	AttemptAt int64  `json:"attempt_at"`
	SentAt    int64  `json:"sent_at"`
	LastTS    int64  `json:"last_ts"`
	Level     string `json:"level"`
}

type diskState struct {
	Config Config              `json:"config"`
	Sent   map[string]delivery `json:"sent"`
	Status Status              `json:"status"`
}

type Service struct {
	mu       sync.Mutex
	path     string
	state    diskState
	since    int64
	revision uint64
	client   *http.Client
}

func New(dataDir string) (*Service, error) {
	s := &Service{path: filepath.Join(dataDir, "notifications.json"), since: time.Now().UnixMilli(), client: &http.Client{
		Timeout: 10 * time.Second,
		// 不跟随重定向，避免认证请求头被发往其他目标。
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}}
	s.state.Config = Config{URL: "", Body: DefaultBody, CooldownMin: 30, MinLevel: "warn"}
	b, err := os.ReadFile(s.path)
	if err == nil {
		if err = json.Unmarshal(b, &s.state); err != nil {
			return nil, errors.New("通知配置文件无法解析")
		}
		if err = validate(s.state.Config); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, errors.New("无法读取通知配置")
	}
	if s.state.Sent == nil {
		s.state.Sent = map[string]delivery{}
	}
	return s, nil
}

// 配置与投递记录一起原子写入，凭据文件仅服务用户可读写。
func (s *Service) persist(state diskState) error {
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".notification-*")
	if err != nil {
		return errors.New("无法保存通知配置，请检查数据目录权限")
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), s.path)
	}
	if err != nil {
		return errors.New("无法保存通知配置，请检查数据目录权限")
	}
	return nil
}

func (s *Service) viewLocked() View {
	c := s.state.Config
	has := c.Headers != ""
	c.Headers = "" // 请求头不回传浏览器，留空保存时保留原值。
	return View{Config: c, HasHeaders: has, Status: s.state.Status}
}
func (s *Service) View() View { s.mu.Lock(); defer s.mu.Unlock(); return s.viewLocked() }

func (s *Service) resolve(in Input) Config {
	c := in.Config
	if in.ClearHeaders {
		c.Headers = ""
	} else if c.Headers == "" {
		c.Headers = s.state.Config.Headers
	}
	return c
}
func (s *Service) Save(in Input) (View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.resolve(in)
	if err := validate(c); err != nil {
		return View{}, err
	}
	next := s.state
	next.Config = c
	if err := s.persist(next); err != nil {
		return View{}, err
	}
	if c.Enabled && !s.state.Config.Enabled {
		s.since = time.Now().UnixMilli()
	}
	s.state = next
	s.revision++
	return s.viewLocked(), nil
}

func parseHeaders(raw string) (http.Header, error) {
	h := http.Header{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if !ok || k == "" || strings.ContainsAny(v, "\r\n") {
			return nil, errors.New("请求头格式应为每行一个 Header: value")
		}
		for _, r := range k {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", r)) {
				return nil, errors.New("请求头名称包含无效字符")
			}
		}
		k = textproto.CanonicalMIMEHeaderKey(k)
		if k == "Host" || k == "Content-Length" || k == "Connection" || k == "Transfer-Encoding" {
			return nil, errors.New("无需设置 Host、Content-Length、Connection 或 Transfer-Encoding 请求头")
		}
		h.Set(k, v)
	}
	return h, nil
}

func validate(c Config) error {
	u, err := url.Parse(c.URL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" {
		return errors.New("请填写有效的 HTTP/HTTPS 接收地址（不含用户名、密码或片段）")
	}
	if c.CooldownMin < 1 || c.CooldownMin > 1440 {
		return errors.New("重复提醒间隔应为 1～1440 分钟")
	}
	if c.MinLevel != "warn" && c.MinLevel != "scan" {
		return errors.New("请选择可疑及扫描或仅扫描异常")
	}
	if len(c.Headers) > 8192 || len(c.Body) > 32768 || len(c.URL) > 2048 || len(c.DetailURL) > 2048 {
		return errors.New("通知配置超出长度限制")
	}
	if c.DetailURL != "" {
		d, e := url.Parse(c.DetailURL)
		if e != nil || d.Hostname() == "" || (d.Scheme != "http" && d.Scheme != "https") || d.User != nil {
			return errors.New("详情地址必须是有效的 HTTP/HTTPS 地址")
		}
	}
	if _, err := parseHeaders(c.Headers); err != nil {
		return err
	}
	if c.Body != "" {
		var v any
		if json.Unmarshal([]byte(c.Body), &v) != nil {
			return errors.New("消息模板必须是有效 JSON；变量请放在字符串内")
		}
	}
	return nil
}

// 在 JSON 字符串值内替换，再编码，避免路径中的引号、换行破坏 JSON。
func render(body string, vars map[string]string) ([]byte, error) {
	var v any
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		return nil, err
	}
	pairs := []string{}
	for k, value := range vars {
		pairs = append(pairs, "#{"+k+"}", value)
	}
	replacer := strings.NewReplacer(pairs...)
	var walk func(any) any
	walk = func(v any) any {
		switch x := v.(type) {
		case string:
			return replacer.Replace(x)
		case []any:
			for i := range x {
				x[i] = walk(x[i])
			}
		case map[string]any:
			for k := range x {
				x[k] = walk(x[k])
			}
		}
		return v
	}
	return json.Marshal(walk(v))
}

func (s *Service) send(ctx context.Context, c Config, vars map[string]string) error {
	method := http.MethodGet
	var body []byte
	if c.Body != "" {
		method = http.MethodPost
		var err error
		body, err = render(c.Body, vars)
		if err != nil {
			return errors.New("消息模板渲染失败")
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.URL, bytes.NewReader(body))
	if err != nil {
		return errors.New("无法创建推送请求")
	}
	req.Header, err = parseHeaders(c.Headers)
	if err != nil {
		return err
	}
	if method == http.MethodPost && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return errors.New("推送连接失败或超时，请检查接收地址、证书和网络")
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024+1))
	if err != nil || len(b) > 64*1024 {
		return errors.New("推送响应读取失败或过大")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("推送接口返回 HTTP %d", resp.StatusCode)
	}
	// 部分推送服务会在 HTTP 200 中报告业务失败；不回显原始响应，以免泄露凭据。
	var result map[string]json.RawMessage
	if json.Unmarshal(b, &result) == nil {
		if raw, ok := result["success"]; ok && string(raw) == "false" {
			return errors.New("推送接口报告失败，请检查 Token 和接收端配置")
		}
		if raw, ok := result["code"]; ok {
			var n int
			if json.Unmarshal(raw, &n) == nil && n != 0 && n != 200 {
				return fmt.Errorf("推送接口报告失败（code=%d）", n)
			}
		}
	}
	return nil
}

func variables(r stats.IPRow, c Config, test bool) map[string]string {
	title := "ipguard 发现可疑访问"
	level := "可疑"
	if r.Level == "scan" {
		level = "扫描"
		title = "ipguard 发现扫描行为"
	}
	content := fmt.Sprintf("IP：%s（%s）\n异常等级：%s\n近 24 小时请求：%d，4xx/5xx 占比：%.1f%%\n主要域名：%s\n路径：%s\n最近访问：%s", r.IP, r.Geo, level, r.Count, r.ErrRatio*100, r.Host, strings.Join(r.Paths, "、"), time.UnixMilli(r.LastTS).In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05"))
	if test {
		title = "ipguard 测试通知"
		content = "这是一条测试通知，以下为模拟异常数据。\n" + content
	}
	return map[string]string{"title": title, "content": content, "url": c.DetailURL, "ip": r.IP, "geo": r.Geo, "level": level, "count": fmt.Sprint(r.Count), "err_ratio": fmt.Sprintf("%.1f%%", r.ErrRatio*100), "host": r.Host, "paths": strings.Join(r.Paths, "、"), "timestamp": time.Now().In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05")}
}

func (s *Service) Test(ctx context.Context, in Input) error {
	s.mu.Lock()
	c := s.resolve(in)
	s.mu.Unlock()
	if err := validate(c); err != nil {
		return err
	}
	return s.send(ctx, c, variables(stats.IPRow{IP: "203.0.113.10", Geo: "模拟地址", Level: "scan", Count: 30, ErrRatio: 0.9, Host: "example.com", Paths: []string{"/.env", "/.git/config"}, LastTS: time.Now().UnixMilli()}, c, true))
}

// Run 不依赖浏览器打开页面；每分钟检查所有异常，不受概览前 8 条限制。
func (s *Service) Run(ctx context.Context, engine *stats.Engine) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !s.View().Enabled {
				continue
			}
			var rows []stats.IPRow
			for page := 1; ; page++ {
				result := engine.IPs(stats.IPQuery{Range: "24h", Level: "suspicious", HideLAN: true, Sort: "ip", Dir: "asc", Page: page, Size: 200})
				rows = append(rows, result.Rows...)
				if page >= result.Pages {
					break
				}
			}
			s.check(ctx, rows, time.Now())
		}
	}
}

func (s *Service) check(ctx context.Context, rows []stats.IPRow, now time.Time) {
	s.mu.Lock()
	c, revision, since := s.state.Config, s.revision, s.since
	sent := make(map[string]delivery, len(s.state.Sent))
	for k, v := range s.state.Sent {
		sent[k] = v
	}
	s.mu.Unlock()
	if !c.Enabled {
		return
	}
	// 每轮最多 20 次推送；其余下轮继续，失败也受一分钟重试间隔限制。
	// 优先处理未尝试或最久未尝试的 IP，失败目标不会占住每轮配额。
	rows = append([]stats.IPRow(nil), rows...)
	sort.SliceStable(rows, func(i, j int) bool { return sent[rows[i].IP].AttemptAt < sent[rows[j].IP].AttemptAt })
	attempts := 0
	for _, r := range rows {
		if ctx.Err() != nil {
			return
		}
		// 带 IP 标识（含放行地址集派生标识）的 IP 视为可信，与自动封禁豁免保持一致，不再发可疑访问告警。
		if r.Banned || r.CC == "LAN" || r.Tag != "" || r.Level == "normal" || (c.MinLevel == "scan" && r.Level != "scan") || r.LastTS <= since {
			continue
		}
		old := sent[r.IP]
		if r.LastTS <= old.LastTS || (old.SentAt > 0 && now.UnixMilli()-old.SentAt < int64(c.CooldownMin)*60000 && !(r.Level == "scan" && old.Level == "warn")) {
			continue
		}
		s.mu.Lock()
		if revision != s.revision {
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
		err := s.send(ctx, c, variables(r, c, false))
		s.mu.Lock()
		if revision != s.revision {
			s.mu.Unlock()
			return
		}
		attempt := s.state.Sent[r.IP]
		attempt.AttemptAt = now.UnixMilli()
		s.state.Sent[r.IP] = attempt
		s.state.Status.LastAttempt = now.UnixMilli()
		if err != nil {
			s.state.Status.Error = err.Error()
		} else {
			s.state.Status.Error = ""
			s.state.Status.LastSuccess = now.UnixMilli()
			s.state.Sent[r.IP] = delivery{AttemptAt: now.UnixMilli(), SentAt: now.UnixMilli(), LastTS: r.LastTS, Level: r.Level}
		}
		for ip, v := range s.state.Sent {
			if now.UnixMilli()-v.AttemptAt > int64(48*time.Hour/time.Millisecond) {
				delete(s.state.Sent, ip)
			}
		}
		if e := s.persist(s.state); e != nil {
			s.state.Status.Error = e.Error()
		}
		s.mu.Unlock()
		attempts++
		if attempts >= 20 {
			return
		}
	}
}

// NotifyAutoBan 在自动封禁成功时复用已配置的推送通道发送通知（若未配置接收地址则静默跳过）。
func (s *Service) NotifyAutoBan(ctx context.Context, ip, geo, duration, reason, host, paths string, count int) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	c := s.state.Config
	s.mu.Unlock()
	if strings.TrimSpace(c.URL) == "" {
		return nil
	}
	if geo == "" {
		geo = "未知"
	}
	if host == "" {
		host = "-"
	}
	if paths == "" {
		paths = "-"
	}
	now := time.Now()
	ts := now.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05")
	title := "ipguard 自动封禁已生效"
	content := fmt.Sprintf("IP：%s（%s）\n处置结果：已自动封禁 %s\n触发原因：%s\n关联域名：%s\n关联路径：%s\n封禁时间：%s", ip, geo, duration, reason, host, paths, ts)
	vars := map[string]string{
		"title":     title,
		"content":   content,
		"url":       c.DetailURL,
		"ip":        ip,
		"geo":       geo,
		"level":     "自动封禁",
		"count":     fmt.Sprint(count),
		"err_ratio": "100.0%",
		"host":      host,
		"paths":     paths,
		"timestamp": ts,
	}
	err := s.send(ctx, c, vars)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Status.LastAttempt = now.UnixMilli()
	if err != nil {
		s.state.Status.Error = err.Error()
		return err
	}
	s.state.Status.Error = ""
	s.state.Status.LastSuccess = now.UnixMilli()
	s.state.Sent[ip] = delivery{AttemptAt: now.UnixMilli(), SentAt: now.UnixMilli(), LastTS: now.UnixMilli(), Level: "scan"}
	_ = s.persist(s.state)
	return nil
}
