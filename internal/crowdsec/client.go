// Package crowdsec 是 CrowdSec LAPI 的最小客户端：以 machine（watcher）身份登录，
// 查询 / 新增 / 删除决策。
//
// 为什么查决策走 /v1/alerts 而不是 /v1/decisions：后者是 bouncer 接口（X-Api-Key），
// machine 的 JWT 调不了。cscli decisions list 本身也是查 alerts 再展开 decisions。
package crowdsec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Decision 是 LAPI 返回的一条决策（字段对齐 swagger 的 Decision）。
type Decision struct {
	ID        int64  `json:"id"`
	Origin    string `json:"origin"`
	Type      string `json:"type"`
	Scope     string `json:"scope"`
	Value     string `json:"value"`
	Duration  string `json:"duration"`
	Until     string `json:"until,omitempty"`
	Scenario  string `json:"scenario"`
	Simulated bool   `json:"simulated"`
}

// Alert 只取我们关心的字段。
type Alert struct {
	ID        int64      `json:"id"`
	Scenario  string     `json:"scenario"`
	CreatedAt string     `json:"created_at"`
	StartAt   string     `json:"start_at"`
	Decisions []Decision `json:"decisions"`
}

// Client 是线程安全的 LAPI 客户端，JWT 过期前自动续登。
type Client struct {
	base      string
	machineID string
	password  string
	http      *http.Client

	mu     sync.Mutex
	token  string
	expire time.Time
}

// New 创建客户端。
func New(baseURL, machineID, password string, timeout time.Duration) *Client {
	return &Client{
		base:      strings.TrimRight(baseURL, "/"),
		machineID: machineID,
		password:  password,
		http:      &http.Client{Timeout: timeout},
	}
}

// BaseURL 返回 LAPI 地址（展示用）。
func (c *Client) BaseURL() string { return c.base }

// APIError 是 LAPI 返回的非 2xx 错误。
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("crowdsec LAPI %d: %s", e.Status, e.Message)
}

func (c *Client) login(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Until(c.expire) > time.Minute {
		return c.token, nil
	}
	body, _ := json.Marshal(map[string]any{
		"machine_id": c.machineID,
		"password":   c.password,
		"scenarios":  []string{},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/v1/watchers/login", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("连接 CrowdSec LAPI 失败: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", &APIError{Status: resp.StatusCode, Message: "machine 登录失败: " + errMessage(data)}
	}
	var out struct {
		Token  string `json:"token"`
		Expire string `json:"expire"`
	}
	if err := json.Unmarshal(data, &out); err != nil || out.Token == "" {
		return "", errors.New("machine 登录响应无法解析")
	}
	exp, err := time.Parse(time.RFC3339, out.Expire)
	if err != nil {
		exp = time.Now().Add(30 * time.Minute)
	}
	c.token, c.expire = out.Token, exp
	return c.token, nil
}

func (c *Client) invalidate() {
	c.mu.Lock()
	c.token = ""
	c.mu.Unlock()
}

// do 发请求，401 时重新登录重试一次。
func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		tok, err := c.login(ctx)
		if err != nil {
			return err
		}
		var rd io.Reader
		if payload != nil {
			rd = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return fmt.Errorf("请求 CrowdSec LAPI 失败: %w", err)
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
		resp.Body.Close()
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			c.invalidate()
			continue
		}
		if resp.StatusCode/100 != 2 {
			return &APIError{Status: resp.StatusCode, Message: errMessage(data)}
		}
		if out != nil && len(data) > 0 && string(data) != "null" {
			if err := json.Unmarshal(data, out); err != nil {
				return fmt.Errorf("解析 LAPI 响应: %w", err)
			}
		}
		return nil
	}
	return &APIError{Status: http.StatusUnauthorized, Message: "鉴权失败"}
}

func errMessage(data []byte) string {
	var e struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(data, &e) == nil && e.Message != "" {
		return e.Message
	}
	s := strings.TrimSpace(string(data))
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// Ping 检查连通性与凭据。
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.login(ctx)
	return err
}

// ActiveDecisions 列出所有生效中的决策（含社区黑名单 CAPI/lists）。
// 返回的 createdAt 取自所属 alert（决策本身没有创建时间字段）。
func (c *Client) ActiveDecisions(ctx context.Context) ([]DecisionWithTime, error) {
	q := url.Values{}
	q.Set("has_active_decision", "true")
	q.Set("include_capi", "true")
	q.Set("limit", "0")
	var alerts []Alert
	if err := c.do(ctx, http.MethodGet, "/v1/alerts?"+q.Encode(), nil, &alerts); err != nil {
		return nil, err
	}
	var out []DecisionWithTime
	seen := map[int64]bool{}
	for _, a := range alerts {
		created := parseTime(a.CreatedAt)
		if created.IsZero() {
			created = parseTime(a.StartAt)
		}
		for _, d := range a.Decisions {
			if seen[d.ID] || d.Simulated {
				continue
			}
			// has_active_decision 只保证 alert 里至少一条决策生效，过期的要自己滤掉
			if dur, err := time.ParseDuration(d.Duration); err == nil && dur <= 0 {
				continue
			}
			if strings.HasPrefix(d.Duration, "-") {
				continue
			}
			seen[d.ID] = true
			out = append(out, DecisionWithTime{Decision: d, CreatedAt: created})
		}
	}
	return out, nil
}

// DecisionWithTime 携带所属 alert 的创建时间。
type DecisionWithTime struct {
	Decision
	CreatedAt time.Time
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// BanRequest 描述一次手动封禁。
type BanRequest struct {
	Scope    string // Ip | Range
	Value    string
	Duration time.Duration
	Reason   string // 写入 scenario，展示为“场景 / 原因”
	Origin   string // 一般为 cscli
}

// AddBan 以 alert 形式写入一条 ban 决策（与 cscli decisions add 相同的做法）。
func (c *Client) AddBan(ctx context.Context, b BanRequest) error {
	now := time.Now().UTC().Format(time.RFC3339)
	dur := formatDuration(b.Duration)
	msg := fmt.Sprintf("manual 'ban' from ipguard: %s for %s", b.Value, dur)
	alert := map[string]any{
		"scenario":         b.Reason,
		"scenario_hash":    "",
		"scenario_version": "",
		"message":          msg,
		"events":           []any{},
		"events_count":     1,
		"start_at":         now,
		"stop_at":          now,
		"capacity":         0,
		"leakspeed":        "0",
		"simulated":        false,
		"labels":           nil,
		"source": map[string]any{
			"scope": b.Scope,
			"value": b.Value,
			"ip":    ipOf(b),
			"range": rangeOf(b),
		},
		"decisions": []map[string]any{{
			"duration": dur,
			"origin":   b.Origin,
			"scenario": b.Reason,
			"scope":    b.Scope,
			"type":     "ban",
			"value":    b.Value,
		}},
	}
	var ids []string
	return c.do(ctx, http.MethodPost, "/v1/alerts", []any{alert}, &ids)
}

func ipOf(b BanRequest) string {
	if b.Scope == "Ip" {
		return b.Value
	}
	return ""
}

func rangeOf(b BanRequest) string {
	if b.Scope == "Range" {
		return b.Value
	}
	return ""
}

// formatDuration 转成 LAPI 接受的 Go duration 文本（只用小时/分钟，LAPI 不认 d）。
func formatDuration(d time.Duration) string {
	if d <= 0 {
		d = 24 * time.Hour
	}
	m := int64(d / time.Minute)
	if m%60 == 0 {
		return strconv.FormatInt(m/60, 10) + "h"
	}
	return strconv.FormatInt(m, 10) + "m"
}

// DeleteDecision 按 ID 删除一条决策。
func (c *Client) DeleteDecision(ctx context.Context, id int64) error {
	var out struct {
		NbDeleted string `json:"nbDeleted"`
	}
	return c.do(ctx, http.MethodDelete, "/v1/decisions/"+strconv.FormatInt(id, 10), nil, &out)
}
