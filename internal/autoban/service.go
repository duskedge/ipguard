// Package autoban 实现轻量级自动封禁策略：
//  1. 域名访问控制高频 403 拦截自动升级封禁（滑动窗口实时计数）
//  2. 敏感路径扫描 / 可疑异常 IP 自动封禁（后台定时巡检）
//
// 所有自动封禁均经过四重防误封豁免校验（内网/保留地址、IP 标识、放行地址集、已封禁状态），
// 并通过 CrowdSec LAPI 下发（Origin=ipguard-auto，归类为“自动封禁”）。
package autoban

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/duskedge/ipguard/internal/geoip"
	"github.com/duskedge/ipguard/internal/service"
	"github.com/duskedge/ipguard/internal/stats"
)

// Banner 抽象封禁执行与状态查询。
type Banner interface {
	AutoBan(ctx context.Context, in service.BanInput) (service.BanResult, error)
	Banned(ip netip.Addr) bool
}

// TagLookup 抽象 IP 标识查询（含单独标识与放行地址集派生标识）。
type TagLookup interface {
	Lookup(ip netip.Addr) string
}

// AddressSetChecker 抽象放行地址集成员检查（即使地址集未设置标识也豁免）。
type AddressSetChecker interface {
	InAnyAddressSet(ip netip.Addr) bool
}

// Notifier 抽象自动封禁后的消息推送。
type Notifier interface {
	NotifyAutoBan(ctx context.Context, ip, geo, duration, reason, host, paths string, count int) error
}

// IPSource 抽象聚合引擎的异常 IP 查询。
type IPSource interface {
	IPs(q stats.IPQuery) stats.IPsResult
}

// Config 定义自动封禁策略配置。
type Config struct {
	GuardEnabled   bool   `json:"guard_enabled"`
	GuardWindowMin int    `json:"guard_window_min"`
	GuardThreshold int    `json:"guard_threshold"`
	GuardDuration  string `json:"guard_duration"`

	ScanEnabled  bool   `json:"scan_enabled"`
	ScanMinLevel string `json:"scan_min_level"`
	ScanMinCount int    `json:"scan_min_count"`
	ScanDuration string `json:"scan_duration"`

	NotifyEnabled bool `json:"notify_enabled"`

	// SensitivePaths 为空表示沿用内置默认列表（随版本更新）；非空则完全替换默认列表。
	SensitivePaths []string `json:"sensitive_paths,omitempty"`
}

// Status 记录自动封禁运行状态与统计。
type Status struct {
	TotalBanned   int    `json:"total_banned"`
	GuardBanned   int    `json:"guard_banned"`
	ScanBanned    int    `json:"scan_banned"`
	LastBanAt     int64  `json:"last_ban_at"`
	LastBanIP     string `json:"last_ban_ip"`
	LastBanReason string `json:"last_ban_reason"`
	Error         string `json:"error"`
}

// View 是前端设置页读取与保存后的响应视图。
type View struct {
	Config
	// DefaultSensitivePaths 是内置默认敏感路径，供前端“恢复默认”。
	DefaultSensitivePaths []string `json:"default_sensitive_paths"`
	SensitiveCustom       bool     `json:"sensitive_custom"`
	Status                Status   `json:"status"`
}

type diskState struct {
	Config Config `json:"config"`
	Status Status `json:"status"`
}

type guardWindow struct {
	hits          []int64
	lastAttemptAt int64
}

type scanAttempt struct {
	attemptAt int64
	lastTS    int64
}

// Service 管理自动封禁配置、滑动窗口计数与后台扫描巡检。
type Service struct {
	mu          sync.Mutex
	path        string
	state       diskState
	scanSince   int64
	guardHits   map[netip.Addr]*guardWindow
	scanHistory map[string]scanAttempt
	rateWindow  []int64

	bans     Banner
	tags     TagLookup
	acl      AddressSetChecker
	notifier Notifier
	now      func() time.Time

	defaultSens []string
	applySens   func([]string)
}

func defaultConfig() Config {
	return Config{
		GuardEnabled:   false,
		GuardWindowMin: 5,
		GuardThreshold: 20,
		GuardDuration:  "24h",
		ScanEnabled:    false,
		ScanMinLevel:   "scan",
		ScanMinCount:   15,
		ScanDuration:   "7d",
		NotifyEnabled:  true,
	}
}

// New 创建或加载自动封禁服务。dataDir 为空时仅在内存运行（供单测使用）。
func New(dataDir string, bans Banner, tags TagLookup, acl AddressSetChecker, notifier Notifier) (*Service, error) {
	now := time.Now()
	s := &Service{
		state:       diskState{Config: defaultConfig()},
		scanSince:   now.UnixMilli(),
		guardHits:   map[netip.Addr]*guardWindow{},
		scanHistory: map[string]scanAttempt{},
		bans:        bans,
		tags:        tags,
		acl:         acl,
		notifier:    notifier,
		now:         time.Now,
	}
	if dataDir != "" {
		s.path = filepath.Join(dataDir, "autoban.json")
		b, err := os.ReadFile(s.path)
		if err == nil {
			if err := json.Unmarshal(b, &s.state); err != nil {
				return nil, errors.New("自动封禁配置文件无法解析")
			}
			s.state.Config = normalizeDefaults(s.state.Config)
			if err := validate(s.state.Config); err != nil {
				return nil, err
			}
		} else if !os.IsNotExist(err) {
			return nil, errors.New("无法读取自动封禁配置")
		}
	}
	return s, nil
}

func normalizeDefaults(c Config) Config {
	def := defaultConfig()
	if c.GuardWindowMin == 0 {
		c.GuardWindowMin = def.GuardWindowMin
	}
	if c.GuardThreshold == 0 {
		c.GuardThreshold = def.GuardThreshold
	}
	c.GuardDuration = strings.TrimSpace(strings.ToLower(c.GuardDuration))
	if c.GuardDuration == "" {
		c.GuardDuration = def.GuardDuration
	}
	c.ScanMinLevel = strings.TrimSpace(strings.ToLower(c.ScanMinLevel))
	if c.ScanMinLevel == "" {
		c.ScanMinLevel = def.ScanMinLevel
	}
	if c.ScanMinCount == 0 {
		c.ScanMinCount = def.ScanMinCount
	}
	c.ScanDuration = strings.TrimSpace(strings.ToLower(c.ScanDuration))
	if c.ScanDuration == "" {
		c.ScanDuration = def.ScanDuration
	}
	c.SensitivePaths = normalizePaths(c.SensitivePaths)
	return c
}

// 敏感路径条目上限，避免误粘贴超大列表拖慢每条日志的匹配。
const (
	maxSensitivePaths   = 300
	maxSensitivePathLen = 128
)

// normalizePaths 去空白、转小写、去重，保持原有顺序；结果为空时返回 nil（沿用默认）。
func normalizePaths(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range in {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

func samePaths(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]bool, len(a))
	for _, p := range a {
		set[p] = true
	}
	for _, p := range b {
		if !set[p] {
			return false
		}
	}
	return true
}

var allowedDurations = map[string]bool{
	"1h":      true,
	"6h":      true,
	"24h":     true,
	"7d":      true,
	"30d":     true,
	"forever": true,
}

func validate(c Config) error {
	if c.GuardWindowMin < 1 || c.GuardWindowMin > 60 {
		return errors.New("域名拦截统计窗口须在 1～60 分钟之间")
	}
	if c.GuardThreshold < 3 || c.GuardThreshold > 1000 {
		return errors.New("域名拦截触发次数阈值须在 3～1000 次之间")
	}
	if !allowedDurations[c.GuardDuration] {
		return errors.New("请选择有效的域名拦截封禁时长")
	}
	if c.ScanMinLevel != "scan" && c.ScanMinLevel != "warn" {
		return errors.New("扫描自动封禁等级仅支持“仅扫描异常”或“可疑及扫描”")
	}
	if c.ScanMinCount < 3 || c.ScanMinCount > 1000 {
		return errors.New("扫描自动封禁最小请求数须在 3～1000 次之间")
	}
	if !allowedDurations[c.ScanDuration] {
		return errors.New("请选择有效的扫描自动封禁时长")
	}
	if len(c.SensitivePaths) > maxSensitivePaths {
		return fmt.Errorf("敏感路径最多 %d 条", maxSensitivePaths)
	}
	for _, p := range c.SensitivePaths {
		if len(p) < 2 || len(p) > maxSensitivePathLen {
			return fmt.Errorf("敏感路径“%s”长度须在 2～%d 个字符之间", p, maxSensitivePathLen)
		}
		if strings.ContainsAny(p, " \t\r\n") {
			return fmt.Errorf("敏感路径“%s”不能包含空白字符", p)
		}
	}
	return nil
}

// BindSensitivePaths 关联内置默认敏感路径与生效回调（统计引擎），并立即应用当前配置。
func (s *Service) BindSensitivePaths(defaults []string, apply func([]string)) {
	s.mu.Lock()
	s.defaultSens = normalizePaths(defaults)
	s.applySens = apply
	s.mu.Unlock()
	s.applySensitive()
}

func (s *Service) applySensitive() {
	s.mu.Lock()
	apply, paths := s.applySens, s.effectivePathsLocked()
	s.mu.Unlock()
	if apply != nil {
		apply(paths)
	}
}

func (s *Service) effectivePathsLocked() []string {
	if len(s.state.Config.SensitivePaths) > 0 {
		return append([]string(nil), s.state.Config.SensitivePaths...)
	}
	return append([]string(nil), s.defaultSens...)
}

func (s *Service) viewLocked() View {
	v := View{Config: s.state.Config, Status: s.state.Status}
	v.SensitiveCustom = len(s.state.Config.SensitivePaths) > 0
	v.SensitivePaths = s.effectivePathsLocked()
	v.DefaultSensitivePaths = append([]string{}, s.defaultSens...)
	if v.SensitivePaths == nil {
		v.SensitivePaths = []string{}
	}
	return v
}

// View 返回当前自动封禁配置与运行状态。
func (s *Service) View() View {
	if s == nil {
		return View{Config: defaultConfig()}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.viewLocked()
}

// Save 校验并保存自动封禁配置。
func (s *Service) Save(in Config) (View, error) {
	c := normalizeDefaults(in)
	if err := validate(c); err != nil {
		return View{}, err
	}
	s.mu.Lock()
	// 与默认列表一致时不落盘，后续版本更新默认列表能自动生效
	if samePaths(c.SensitivePaths, s.defaultSens) {
		c.SensitivePaths = nil
	}
	wasGuard := s.state.Config.GuardEnabled
	wasScan := s.state.Config.ScanEnabled
	next := s.state
	next.Config = c
	next.Status.Error = ""
	if s.path != "" {
		if err := s.persist(next); err != nil {
			s.mu.Unlock()
			return View{}, err
		}
	}
	s.state = next
	nowMs := s.now().UnixMilli()
	if !c.GuardEnabled || !wasGuard {
		s.guardHits = map[netip.Addr]*guardWindow{}
	}
	if c.ScanEnabled && !wasScan {
		// 新开启扫描封禁时仅对开启后的新活动生效，不追溯历史旧请求。
		s.scanSince = nowMs
	}
	s.mu.Unlock()
	s.applySensitive()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.viewLocked(), nil
}

func (s *Service) persist(state diskState) error {
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".autoban-*")
	if err != nil {
		return errors.New("无法保存自动封禁配置，请检查数据目录权限")
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
		return errors.New("无法保存自动封禁配置，请检查数据目录权限")
	}
	return nil
}

// IsExempt 执行四重安全豁免校验：
//  1. 内网 / 回环 / 未指定 / 组播地址永不自动封禁
//  2. 命中任意自定义 IP 标识（含地址集派生标识）永不自动封禁
//  3. 命中任意放行地址集（精确 IP 或网段）永不自动封禁
//  4. 当前已在 CrowdSec 封禁列表中无需重复封禁
func (s *Service) IsExempt(ip netip.Addr) bool {
	if !ip.IsValid() {
		return true
	}
	ip = ip.Unmap()
	if geoip.IsPrivate(ip) || ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	if s.tags != nil && s.tags.Lookup(ip) != "" {
		return true
	}
	if s.acl != nil && s.acl.InAnyAddressSet(ip) {
		return true
	}
	if s.bans != nil && s.bans.Banned(ip) {
		return true
	}
	return false
}

// allowRateLocked 限制每分钟最多自动封禁 10 个 IP，防止异常洪峰冲击 LAPI。
func (s *Service) allowRateLocked(nowMs int64) bool {
	cutoff := nowMs - 60_000
	kept := s.rateWindow[:0]
	for _, ts := range s.rateWindow {
		if ts >= cutoff {
			kept = append(kept, ts)
		}
	}
	s.rateWindow = kept
	if len(s.rateWindow) >= 10 {
		return false
	}
	s.rateWindow = append(s.rateWindow, nowMs)
	return true
}

// RecordGuardBlock 在每次域名访问控制返回 403 时调用，按滑动窗口统计拦截频次并在达标时异步升级为 CrowdSec 封禁。
func (s *Service) RecordGuardBlock(ip netip.Addr, host, uri, geo string) {
	if s == nil || !ip.IsValid() {
		return
	}
	// guard 关闭时直接短路，避免每个被拒请求都白跑豁免校验（热路径）。
	s.mu.Lock()
	enabled := s.state.Config.GuardEnabled
	s.mu.Unlock()
	if !enabled {
		return
	}
	ip = ip.Unmap()
	if s.IsExempt(ip) {
		return
	}
	nowMs := s.now().UnixMilli()

	s.mu.Lock()
	c := s.state.Config
	if !c.GuardEnabled {
		s.mu.Unlock()
		return
	}
	cutoff := nowMs - int64(c.GuardWindowMin)*60_000
	if len(s.guardHits) > 2048 {
		for k, w := range s.guardHits {
			if len(w.hits) == 0 || w.hits[len(w.hits)-1] < cutoff {
				delete(s.guardHits, k)
			}
		}
	}
	w := s.guardHits[ip]
	if w == nil {
		w = &guardWindow{}
		s.guardHits[ip] = w
	}
	kept := w.hits[:0]
	for _, ts := range w.hits {
		if ts >= cutoff {
			kept = append(kept, ts)
		}
	}
	w.hits = append(kept, nowMs)
	hitCount := len(w.hits)
	if hitCount < c.GuardThreshold || (w.lastAttemptAt > 0 && nowMs-w.lastAttemptAt < 60_000) {
		s.mu.Unlock()
		return
	}
	if !s.allowRateLocked(nowMs) {
		s.mu.Unlock()
		return
	}
	w.lastAttemptAt = nowMs
	w.hits = nil
	duration := c.GuardDuration
	notify := c.NotifyEnabled
	windowMin := c.GuardWindowMin
	s.mu.Unlock()

	reason := fmt.Sprintf("auto/guard-403 (%dm内拦截%d次)", windowMin, hitCount)
	if host != "" {
		reason = fmt.Sprintf("auto/guard-403 (%dm内拦截%d次, %s)", windowMin, hitCount, host)
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		s.executeBan(ctx, "guard", ip, geo, duration, reason, host, uri, hitCount, notify)
	}()
}

// Run 后台每分钟巡检聚合引擎中的扫描/可疑异常 IP，满足阈值时自动封禁。
func (s *Service) Run(ctx context.Context, engine IPSource) {
	if s == nil || engine == nil {
		return
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !s.View().ScanEnabled {
				continue
			}
			var rows []stats.IPRow
			for page := 1; ; page++ {
				res := engine.IPs(stats.IPQuery{
					Range:   "1h",
					Level:   "suspicious",
					HideLAN: true,
					Sort:    "last",
					Dir:     "desc",
					Page:    page,
					Size:    200,
				})
				rows = append(rows, res.Rows...)
				if page >= res.Pages {
					break
				}
			}
			s.checkScanRows(ctx, rows)
		}
	}
}

func (s *Service) checkScanRows(ctx context.Context, rows []stats.IPRow) {
	nowMs := s.now().UnixMilli()
	s.mu.Lock()
	c := s.state.Config
	since := s.scanSince
	for ipStr, att := range s.scanHistory {
		if nowMs-att.attemptAt > int64(24*time.Hour/time.Millisecond) {
			delete(s.scanHistory, ipStr)
		}
	}
	s.mu.Unlock()
	if !c.ScanEnabled {
		return
	}

	for _, r := range rows {
		if ctx.Err() != nil {
			return
		}
		if r.Banned || r.CC == "LAN" || r.Tag != "" || r.LastTS <= since || r.Count < c.ScanMinCount {
			continue
		}
		if r.Level != "scan" && !(c.ScanMinLevel == "warn" && r.Level == "warn") {
			continue
		}
		ip, err := netip.ParseAddr(r.IP)
		if err != nil || s.IsExempt(ip) {
			continue
		}

		s.mu.Lock()
		prev := s.scanHistory[r.IP]
		if r.LastTS <= prev.lastTS || (prev.attemptAt > 0 && nowMs-prev.attemptAt < 5*60_000) {
			s.mu.Unlock()
			continue
		}
		if !s.allowRateLocked(nowMs) {
			s.mu.Unlock()
			return
		}
		s.scanHistory[r.IP] = scanAttempt{attemptAt: nowMs, lastTS: r.LastTS}
		duration := c.ScanDuration
		notify := c.NotifyEnabled
		s.mu.Unlock()

		paths := strings.Join(r.Paths, "、")
		reason := fmt.Sprintf("auto/%s (1h内%d次, 错误率%.0f%%)", r.Level, r.Count, r.ErrRatio*100)
		if len(r.Paths) > 0 {
			reason = fmt.Sprintf("auto/%s (1h内%d次, %s)", r.Level, r.Count, r.Paths[0])
		}
		banCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		s.executeBan(banCtx, "scan", ip, r.Geo, duration, reason, r.Host, paths, r.Count, notify)
		cancel()
	}
}

func (s *Service) executeBan(ctx context.Context, kind string, ip netip.Addr, geo, duration, reason, host, paths string, count int, notify bool) {
	if s.IsExempt(ip) {
		return
	}
	if s.bans == nil {
		return
	}
	_, err := s.bans.AutoBan(ctx, service.BanInput{
		Target:   ip.String(),
		Duration: duration,
		Reason:   reason,
	})
	if errors.Is(err, service.ErrAlreadyBanned) {
		return
	}

	nowMs := s.now().UnixMilli()
	s.mu.Lock()
	if err != nil {
		s.state.Status.Error = service.ErrorText(err)
		if s.path != "" {
			_ = s.persist(s.state)
		}
		s.mu.Unlock()
		slog.Warn("自动封禁执行失败", "kind", kind, "ip", ip.String(), "reason", reason, "error", err)
		return
	}
	s.state.Status.Error = ""
	s.state.Status.TotalBanned++
	if kind == "guard" {
		s.state.Status.GuardBanned++
	} else {
		s.state.Status.ScanBanned++
	}
	s.state.Status.LastBanAt = nowMs
	s.state.Status.LastBanIP = ip.String()
	s.state.Status.LastBanReason = reason
	if s.path != "" {
		_ = s.persist(s.state)
	}
	s.mu.Unlock()

	slog.Info("自动封禁已生效", "kind", kind, "ip", ip.String(), "duration", duration, "reason", reason)
	if notify && s.notifier != nil {
		if nErr := s.notifier.NotifyAutoBan(ctx, ip.String(), geo, duration, reason, host, paths, count); nErr != nil {
			slog.Warn("自动封禁通知发送失败", "ip", ip.String(), "error", nErr)
		}
	}
}
