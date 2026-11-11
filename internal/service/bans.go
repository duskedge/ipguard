// Package service 编排业务：封禁列表缓存、封禁/解封与审计。
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/duskedge/ipguard/internal/crowdsec"
	"github.com/duskedge/ipguard/internal/geoip"
	"github.com/duskedge/ipguard/internal/model"
	"github.com/duskedge/ipguard/internal/repository"
)

// 永久封禁在 CrowdSec 里用 10 年表示（LAPI 不支持无限期）。
const (
	foreverDuration  = 87600 * time.Hour
	foreverThreshold = 5 * 365 * 24 * time.Hour
	maxBanDuration   = foreverDuration
)

// 业务错误（handler 据此映射 HTTP 状态码）。
var (
	ErrNotConfigured = errors.New("未配置 CrowdSec machine 凭据")
	ErrInvalidTarget = errors.New("请填写合法的 IP 或 CIDR")
	ErrPrivateTarget = errors.New("不能封禁内网/保留地址")
	ErrTooWide       = errors.New("网段过大（IPv4 至少 /8，IPv6 至少 /32）")
	ErrAlreadyBanned = errors.New("该地址已在封禁列表中")
	ErrNotFound      = errors.New("封禁记录不存在或已过期")
	ErrBadDuration   = errors.New("时长不合法")
)

// Ban 是封禁列表的一行。
type Ban struct {
	ID        int64  `json:"id"`
	Target    string `json:"target"`
	Scope     string `json:"scope"` // ip | range
	Geo       string `json:"geo"`
	CC        string `json:"cc"`
	Country   string `json:"country"`
	Source    string `json:"source"` // manual | auto | community
	Origin    string `json:"origin"`
	Scenario  string `json:"scenario"`
	RemainMin int64  `json:"remain_min"`
	Permanent bool   `json:"permanent"`
	CreatedTS int64  `json:"created_ts"`

	expires time.Time
}

// Counts 是各来源的封禁数量。
type Counts struct {
	Total     int `json:"total"`
	Manual    int `json:"manual"`
	Auto      int `json:"auto"`
	Community int `json:"community"`
}

// Status 是 CrowdSec 连接状态。
type Status struct {
	Configured  bool   `json:"configured"`
	Connected   bool   `json:"connected"`
	URL         string `json:"url"`
	Error       string `json:"error,omitempty"`
	RefreshedAt int64  `json:"refreshed_at"`
}

type snapshot struct {
	bans   []Ban
	counts Counts
	ips    map[netip.Addr]int64 // IP → 决策 ID
	ranges []rangeBan
	at     time.Time
}

type rangeBan struct {
	prefix netip.Prefix
	id     int64
}

// BanService 缓存 CrowdSec 决策并提供封禁/解封。
type BanService struct {
	cs     *crowdsec.Client // nil 表示未配置
	geo    *geoip.Resolver
	audit  *repository.AuditRepo
	ttl    time.Duration
	origin string

	refreshMu sync.Mutex // 防止并发刷新打爆 LAPI
	mu        sync.RWMutex
	snap      *snapshot
	lastErr   string
	version   atomic.Uint64
}

// NewBanService 创建服务。cs 为 nil 时所有写操作返回 ErrNotConfigured，列表为空。
func NewBanService(cs *crowdsec.Client, geo *geoip.Resolver, audit *repository.AuditRepo, ttl time.Duration, origin string) *BanService {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	if origin == "" {
		origin = "cscli"
	}
	return &BanService{cs: cs, geo: geo, audit: audit, ttl: ttl, origin: origin}
}

// Version 每次封禁数据变化（刷新或写操作）递增，供统计缓存失效。
func (s *BanService) Version() uint64 { return s.version.Load() }

// Run 定期后台刷新决策缓存，让页面请求基本不用同步等 LAPI。
func (s *BanService) Run(ctx context.Context) {
	if s.cs == nil {
		return
	}
	_, _ = s.refresh(ctx, false)
	tk := time.NewTicker(s.ttl)
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
			_, _ = s.refresh(ctx, false)
		}
	}
}

// classify 把 origin 映射成三类来源。
func classify(origin string) string {
	switch strings.ToLower(origin) {
	case "cscli", "cscli-import", "console", "ipguard", "manual":
		return "manual"
	case "capi", "lists":
		return "community"
	default:
		return "auto"
	}
}

func (s *BanService) refresh(ctx context.Context, force bool) (*snapshot, error) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	// 拿到锁时可能别人刚刷新完
	s.mu.RLock()
	cur := s.snap
	s.mu.RUnlock()
	if !force && cur != nil && time.Since(cur.at) < time.Second {
		return cur, nil
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ds, err := s.cs.ActiveDecisions(ctx)
	if err != nil {
		s.mu.Lock()
		s.lastErr = err.Error()
		s.mu.Unlock()
		slog.Warn("刷新 CrowdSec 决策失败", "error", err)
		return cur, err
	}
	now := time.Now()
	snap := &snapshot{ips: map[netip.Addr]int64{}, at: now}
	for _, d := range ds {
		if !strings.EqualFold(d.Type, "ban") {
			continue
		}
		b := Ban{ID: d.ID, Target: d.Value, Origin: d.Origin, Scenario: d.Scenario, Source: classify(d.Origin)}
		if !d.CreatedAt.IsZero() {
			b.CreatedTS = d.CreatedAt.UnixMilli()
		}
		if dur, err := time.ParseDuration(d.Duration); err == nil {
			b.expires = now.Add(dur)
			b.RemainMin = int64(dur / time.Minute)
			b.Permanent = dur >= foreverThreshold
		}
		var base netip.Addr
		switch strings.ToLower(d.Scope) {
		case "ip":
			ip, err := netip.ParseAddr(d.Value)
			if err != nil {
				continue
			}
			b.Scope, base = "ip", ip.Unmap()
			snap.ips[base] = d.ID
		case "range":
			pfx, err := netip.ParsePrefix(d.Value)
			if err != nil {
				continue
			}
			b.Scope, base = "range", pfx.Addr()
			snap.ranges = append(snap.ranges, rangeBan{pfx.Masked(), d.ID})
		default:
			continue // country / AS 等范围不展示
		}
		p := s.geo.Lookup(base)
		b.Geo, b.CC = p.Label(), p.CC
		b.Country = p.Country
		if b.Country == "" {
			b.Country = "未知"
		}
		snap.bans = append(snap.bans, b)
		switch b.Source {
		case "manual":
			snap.counts.Manual++
		case "auto":
			snap.counts.Auto++
		default:
			snap.counts.Community++
		}
	}
	snap.counts.Total = len(snap.bans)
	s.mu.Lock()
	s.snap = snap
	s.lastErr = ""
	s.mu.Unlock()
	s.version.Add(1)
	return snap, nil
}

// current 返回缓存；过期则同步刷新，刷新失败时退回旧缓存。
func (s *BanService) current(ctx context.Context) (*snapshot, error) {
	if s.cs == nil {
		return &snapshot{ips: map[netip.Addr]int64{}}, ErrNotConfigured
	}
	s.mu.RLock()
	cur := s.snap
	s.mu.RUnlock()
	if cur != nil && time.Since(cur.at) < s.ttl*2 {
		return cur, nil
	}
	snap, err := s.refresh(ctx, false)
	if snap == nil {
		return &snapshot{ips: map[netip.Addr]int64{}}, err
	}
	return snap, err
}

// Status 返回连接状态。
func (s *BanService) Status() Status {
	st := Status{Configured: s.cs != nil}
	if s.cs == nil {
		st.Error = ErrNotConfigured.Error()
		return st
	}
	st.URL = s.cs.BaseURL()
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.snap != nil {
		st.RefreshedAt = s.snap.at.UnixMilli()
	}
	st.Connected = s.snap != nil && s.lastErr == ""
	st.Error = s.lastErr
	return st
}

// Banned 实现 stats.BanChecker：只用缓存，不触发网络请求。
func (s *BanService) Banned(ip netip.Addr) bool {
	_, ok := s.matchCached(ip)
	return ok
}

func (s *BanService) matchCached(ip netip.Addr) (int64, bool) {
	s.mu.RLock()
	snap := s.snap
	s.mu.RUnlock()
	if snap == nil {
		return 0, false
	}
	ip = ip.Unmap()
	if id, ok := snap.ips[ip]; ok {
		return id, true
	}
	for _, r := range snap.ranges {
		if r.prefix.Contains(ip) {
			return r.id, true
		}
	}
	return 0, false
}

// Counts 返回各来源数量。
func (s *BanService) Counts(ctx context.Context) Counts {
	snap, _ := s.current(ctx)
	return snap.counts
}

// ListQuery 是 /api/bans 的参数。
type ListQuery struct {
	Source  string
	Q       string
	Country string
	Sort    string // ts | remain | target | source
	Dir     string
	Page    int
	Size    int
}

// ListResult 是 /api/bans 的响应。
type ListResult struct {
	Rows      []Ban    `json:"rows"`
	Counts    Counts   `json:"counts"`
	Countries []string `json:"countries"`
	Page      int      `json:"page"`
	Pages     int      `json:"pages"`
	Total     int      `json:"total"`
	Error     string   `json:"error,omitempty"`
}

// List 查询封禁列表。LAPI 不可用时返回旧缓存并带上 error。
func (s *BanService) List(ctx context.Context, q ListQuery) ListResult {
	snap, err := s.current(ctx)
	res := ListResult{Counts: snap.counts, Rows: []Ban{}, Countries: []string{}, Page: 1, Pages: 1}
	if err != nil {
		res.Error = err.Error()
	}
	now := time.Now()
	qq := strings.TrimSpace(q.Q)
	countries := map[string]bool{}
	rows := make([]Ban, 0, len(snap.bans))
	for _, b := range snap.bans {
		// 缓存期间可能有决策到期，按当前时间重算剩余时长
		if !b.expires.IsZero() {
			left := b.expires.Sub(now)
			if left <= 0 {
				continue
			}
			b.RemainMin = int64(left / time.Minute)
		}
		countries[b.Country] = true
		if q.Source != "" && q.Source != "all" && b.Source != q.Source {
			continue
		}
		if qq != "" && !strings.Contains(b.Target, qq) && !banContains(b, qq) {
			continue
		}
		if !geoip.MatchCountry(b.Country, q.Country) {
			continue
		}
		rows = append(rows, b)
	}
	for c := range countries {
		res.Countries = append(res.Countries, c)
	}
	sort.Strings(res.Countries)

	asc := q.Dir == "asc"
	var less func(a, b *Ban) bool
	switch q.Sort {
	case "remain":
		less = func(a, b *Ban) bool { return a.RemainMin < b.RemainMin }
	case "target":
		less = func(a, b *Ban) bool { return targetAddr(a).Less(targetAddr(b)) }
	case "source":
		less = func(a, b *Ban) bool { return a.Source < b.Source }
	default:
		less = func(a, b *Ban) bool { return a.CreatedTS < b.CreatedTS }
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if asc {
			return less(&rows[i], &rows[j])
		}
		return less(&rows[j], &rows[i])
	})

	size := q.Size
	if size <= 0 || size > 200 {
		size = 10
	}
	res.Total = len(rows)
	res.Pages = (len(rows) + size - 1) / size
	if res.Pages < 1 {
		res.Pages = 1
	}
	page := q.Page
	if page < 1 {
		page = 1
	}
	if page > res.Pages {
		page = res.Pages
	}
	res.Page = page
	start := (page - 1) * size
	end := start + size
	if end > len(rows) {
		end = len(rows)
	}
	res.Rows = append(res.Rows, rows[start:end]...)
	return res
}

// banContains：搜索框里输入一个 IP 时，也能找到覆盖它的网段封禁。
func banContains(b Ban, q string) bool {
	if b.Scope != "range" {
		return false
	}
	ip, err := netip.ParseAddr(q)
	if err != nil {
		return false
	}
	pfx, err := netip.ParsePrefix(b.Target)
	return err == nil && pfx.Contains(ip)
}

func targetAddr(b *Ban) netip.Addr {
	if b.Scope == "range" {
		if p, err := netip.ParsePrefix(b.Target); err == nil {
			return p.Addr()
		}
	}
	a, _ := netip.ParseAddr(b.Target)
	return a
}

// ParseTarget 校验并规范化封禁目标，返回 (scope, value)。
func ParseTarget(s string) (string, string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", ErrInvalidTarget
	}
	if strings.Contains(s, "/") {
		pfx, err := netip.ParsePrefix(s)
		if err != nil {
			return "", "", ErrInvalidTarget
		}
		pfx = netip.PrefixFrom(pfx.Addr().Unmap(), pfx.Bits()).Masked()
		if pfx.Addr().Is4() && pfx.Bits() < 8 || pfx.Addr().Is6() && pfx.Bits() < 32 {
			return "", "", ErrTooWide
		}
		if geoip.IsPrivate(pfx.Addr()) {
			return "", "", ErrPrivateTarget
		}
		if pfx.IsSingleIP() {
			return "Ip", pfx.Addr().String(), nil
		}
		return "Range", pfx.String(), nil
	}
	ip, err := netip.ParseAddr(s)
	if err != nil {
		return "", "", ErrInvalidTarget
	}
	ip = ip.Unmap()
	if geoip.IsPrivate(ip) || ip.IsUnspecified() || ip.IsMulticast() {
		return "", "", ErrPrivateTarget
	}
	return "Ip", ip.String(), nil
}

// ParseDuration 解析封禁时长："1h"、"24h"、"7d"、"90m"、"forever"/"永久"，或纯数字分钟。
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	switch s {
	case "":
		return 24 * time.Hour, nil
	case "forever", "permanent", "永久":
		return foreverDuration, nil
	}
	var d time.Duration
	if n, err := strconv.Atoi(s); err == nil {
		d = time.Duration(n) * time.Minute
	} else if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, ErrBadDuration
		}
		d = time.Duration(n) * 24 * time.Hour
	} else {
		var err error
		if d, err = time.ParseDuration(s); err != nil {
			return 0, ErrBadDuration
		}
	}
	if d < time.Minute || d > maxBanDuration {
		return 0, ErrBadDuration
	}
	return d, nil
}

// Actor 标识操作者（写进审计）。
type Actor struct {
	User     string
	ClientIP string
}

// BanInput 是一次封禁请求。
type BanInput struct {
	Target   string
	Duration string
	Reason   string
}

// BanResult 回给前端用于 toast。
type BanResult struct {
	Target    string `json:"target"`
	Scope     string `json:"scope"`
	Duration  string `json:"duration"`
	Permanent bool   `json:"permanent"`
}

// Ban 封禁一个 IP / 网段。
func (s *BanService) Ban(ctx context.Context, actor Actor, in BanInput) (BanResult, error) {
	scope, value, err := ParseTarget(in.Target)
	if err != nil {
		return BanResult{}, err
	}
	dur, err := ParseDuration(in.Duration)
	if err != nil {
		return BanResult{}, err
	}
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		reason = "manual-ipguard"
	}
	if len(reason) > 200 {
		reason = reason[:200]
	}
	res := BanResult{Target: value, Scope: strings.ToLower(scope), Duration: dur.String(), Permanent: dur >= foreverThreshold}
	if s.cs == nil {
		return res, ErrNotConfigured
	}
	if snap, _ := s.current(ctx); snap != nil {
		for _, b := range snap.bans {
			if b.Target == value {
				return res, ErrAlreadyBanned
			}
		}
	}
	err = s.cs.AddBan(ctx, crowdsec.BanRequest{Scope: scope, Value: value, Duration: dur, Reason: reason, Origin: s.origin})
	durText := dur.String()
	if res.Permanent {
		durText = "forever"
	}
	s.record(actor, "ban", value, durText, reason, "manual", err)
	if err != nil {
		return res, err
	}
	s.forceRefresh(ctx)
	return res, nil
}

// Unban 解封：按决策 ID，或按目标（删除该目标上的全部决策）。
func (s *BanService) Unban(ctx context.Context, actor Actor, id int64, target string) ([]string, error) {
	if s.cs == nil {
		return nil, ErrNotConfigured
	}
	snap, err := s.current(ctx)
	if err != nil && snap.bans == nil {
		return nil, err
	}
	var victims []Ban
	if id > 0 {
		for _, b := range snap.bans {
			if b.ID == id {
				victims = append(victims, b)
				break
			}
		}
	} else if target = strings.TrimSpace(target); target != "" {
		if _, v, err := ParseTarget(target); err == nil {
			target = v
		}
		for _, b := range snap.bans {
			if b.Target == target {
				victims = append(victims, b)
			}
		}
	}
	if len(victims) == 0 {
		return nil, ErrNotFound
	}
	var done []string
	var firstErr error
	for _, b := range victims {
		err := s.cs.DeleteDecision(ctx, b.ID)
		var ae *crowdsec.APIError
		if errors.As(err, &ae) && ae.Status == 404 {
			err = nil // 已经被别处删了，视为成功
		}
		s.record(actor, "unban", b.Target, "", b.Scenario, b.Source, err)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		done = append(done, b.Target)
	}
	s.forceRefresh(ctx)
	return done, firstErr
}

func (s *BanService) forceRefresh(ctx context.Context) {
	if _, err := s.refresh(ctx, true); err != nil {
		slog.Warn("写操作后刷新决策失败", "error", err)
	}
}

func (s *BanService) record(actor Actor, action, target, dur, reason, source string, err error) {
	if s.audit == nil {
		return
	}
	a := &model.AuditLog{
		CreatedAt: time.Now().UTC(), Operator: actor.User, ClientIP: actor.ClientIP,
		Action: action, Target: target, Duration: dur, Reason: reason, Source: source, Result: "ok",
	}
	if err != nil {
		a.Result, a.Error = "fail", err.Error()
	}
	if e := s.audit.Add(a); e != nil {
		slog.Error("写审计日志失败", "error", e, "action", action, "target", target)
	}
	slog.Info("封禁操作", "action", action, "target", target, "duration", dur, "operator", actor.User, "result", a.Result)
}

// ErrorText 给前端更友好的错误描述。
func ErrorText(err error) string {
	var ae *crowdsec.APIError
	if errors.As(err, &ae) {
		return fmt.Sprintf("CrowdSec 返回 %d：%s", ae.Status, ae.Message)
	}
	return err.Error()
}

// AutoBan 由自动封禁策略触发封禁：Origin 记为 ipguard-auto（归类到“自动封禁”），审计操作人记为 auto-policy。
func (s *BanService) AutoBan(ctx context.Context, in BanInput) (BanResult, error) {
	scope, value, err := ParseTarget(in.Target)
	if err != nil {
		return BanResult{}, err
	}
	dur, err := ParseDuration(in.Duration)
	if err != nil {
		return BanResult{}, err
	}
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		reason = "ipguard/auto-policy"
	}
	if r := []rune(reason); len(r) > 120 {
		reason = string(r[:120])
	}
	res := BanResult{Target: value, Scope: strings.ToLower(scope), Duration: dur.String(), Permanent: dur >= foreverThreshold}
	if s.cs == nil {
		return res, ErrNotConfigured
	}
	if snap, _ := s.current(ctx); snap != nil {
		for _, b := range snap.bans {
			if b.Target == value {
				return res, ErrAlreadyBanned
			}
		}
	}
	err = s.cs.AddBan(ctx, crowdsec.BanRequest{Scope: scope, Value: value, Duration: dur, Reason: reason, Origin: "ipguard-auto"})
	durText := dur.String()
	if res.Permanent {
		durText = "forever"
	}
	s.record(Actor{User: "auto-policy", ClientIP: "127.0.0.1"}, "ban", value, durText, reason, "auto", err)
	if err != nil {
		return res, err
	}
	s.forceRefresh(ctx)
	return res, nil
}
