// Package hostacl 实现按域名的访问控制（支持“仅限国内 IP”与“仅限指定 IP/标识”），
// 持久化到 data/host_rules.json，并供 Nginx auth_request 子请求实时判定。
package hostacl

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/duskedge/ipguard/internal/geoip"
	"github.com/duskedge/ipguard/internal/iptag"
)

const (
	ModeDomestic  = "domestic"  // 仅限中国大陆（+ 内网 + 可选额外白名单）
	ModeAllowlist = "allowlist" // 仅限指定 IP / 标识（+ 内网）
)

// Rule 是一条域名访问控制规则。支持单域名或多域名共用同一策略。
type Rule struct {
	Host      string   `json:"host"`            // 域名显示串（单域名原样，多域名以 ", " 连接）
	Hosts     []string `json:"hosts,omitempty"` // 域名列表，如 ["emby.example.com", "*.example.com"]
	Enabled   bool     `json:"enabled"`         // 是否启用
	Mode      string   `json:"mode"`            // domestic | allowlist
	AllowTags []string `json:"allow_tags"`      // 允许放行的 IP 标识名称（如 ["公司"]）
	AllowSets []string `json:"allow_sets,omitempty"`
	AllowIPs  []string `json:"allow_ips"` // 允许放行的额外 IP 或 CIDR（如 ["198.51.100.189"]）
	Note      string   `json:"note,omitempty"`
}

// RuleHostStat 是单条规则内每个域名的独立拦截统计。
type RuleHostStat struct {
	BlockedCount  uint64 `json:"blocked_count"`
	LastBlockedIP string `json:"last_blocked_ip,omitempty"`
	LastBlockedAt int64  `json:"last_blocked_at,omitempty"`
}

// RuleView 是带实时拦截计数的规则视图。
type RuleView struct {
	Rule
	BlockedCount  uint64                  `json:"blocked_count"`
	LastBlockedIP string                  `json:"last_blocked_ip,omitempty"`
	LastBlockedAt int64                   `json:"last_blocked_at,omitempty"`
	HostStats     map[string]RuleHostStat `json:"host_stats,omitempty"`
}

type hostPattern struct {
	host     string // 小写域名
	wildcard string // 若为 *.example.com 则存 .example.com
}

type compiledRule struct {
	effectiveIPs []string
	raw          Rule
	host         string        // 主显示串
	patterns     []hostPattern // 规则包含的所有域名模式
	tags         map[string]bool
	ips          map[netip.Addr]bool
	prefixes     []netip.Prefix
}

type ruleStat struct {
	blockedCount  uint64
	lastBlockedIP string
	lastBlockedAt int64
}

// Store 管理域名访问控制规则并执行实时判定。并发安全。
type Store struct {
	setsPath          string
	sets              []AddressSet
	path              string
	geo               *geoip.Resolver
	tags              *iptag.Store
	mu                sync.RWMutex
	rules             []compiledRule
	stats             map[string]*ruleStat
	statsPath         string
	statsLoaded       bool
	statsVersion      uint64
	statsSavedVersion uint64
	statsFlushMu      sync.Mutex
}

// New 创建并加载域名访问控制存储。
func New(dataDir string, geo *geoip.Resolver, tags *iptag.Store) (*Store, error) {
	s := &Store{
		geo:   geo,
		tags:  tags,
		rules: []compiledRule{},
		stats: map[string]*ruleStat{},
	}
	if err := s.loadSets(dataDir); err != nil {
		return nil, err
	}
	if dataDir != "" {
		s.path = filepath.Join(dataDir, "host_rules.json")
		b, err := os.ReadFile(s.path)
		if err == nil {
			var rules []Rule
			if err := json.Unmarshal(b, &rules); err != nil {
				return nil, errors.New("域名访问控制配置文件无法解析")
			}
			compiled, err := compile(rules)
			if err != nil {
				return nil, err
			}
			if err := bindSets(compiled, s.sets); err != nil {
				return nil, err
			}
			s.rules = compiled
		} else if !os.IsNotExist(err) {
			return nil, errors.New("无法读取域名访问控制配置")
		}
	}
	if err := s.loadStats(dataDir); err != nil {
		return nil, err
	}
	s.syncSetTags()
	return s, nil
}

// List 返回当前全部规则及其实时拦截统计。
func (s *Store) List() []RuleView {
	if s == nil {
		return []RuleView{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]RuleView, len(s.rules))
	for i, r := range s.rules {
		v := RuleView{Rule: r.raw}
		for _, p := range r.patterns {
			if st := s.stats[p.host]; st != nil {
				v.BlockedCount += st.blockedCount
				if st.blockedCount > 0 {
					if v.HostStats == nil {
						v.HostStats = make(map[string]RuleHostStat, len(r.patterns))
					}
					v.HostStats[p.host] = RuleHostStat{
						BlockedCount:  st.blockedCount,
						LastBlockedIP: st.LastBlockedIP(),
						LastBlockedAt: st.lastBlockedAt,
					}
				}
				if st.lastBlockedAt >= v.LastBlockedAt && st.LastBlockedIP() != "" {
					v.LastBlockedIP = st.LastBlockedIP()
					v.LastBlockedAt = st.lastBlockedAt
				}
			}
		}
		out[i] = v
	}
	return out
}

func (st *ruleStat) LastBlockedIP() string {
	if st == nil {
		return ""
	}
	return st.lastBlockedIP
}

// Save 校验并保存全量规则。
func (s *Store) Save(rules []Rule) ([]RuleView, error) {
	compiled, err := compile(rules)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if err := bindSets(compiled, s.sets); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if s.path != "" {
		raw := make([]Rule, len(compiled))
		for i := range compiled {
			raw[i] = compiled[i].raw
		}
		if err := persist(s.path, raw); err != nil {
			s.mu.Unlock()
			return nil, err
		}
	}
	s.rules = compiled
	s.mu.Unlock()
	return s.List(), nil
}

// Decision 保存判定时的规则和来源快照，历史记录不依赖当前配置。
type Decision struct {
	Allowed   bool     `json:"allowed"`
	Reason    string   `json:"reason"`
	RuleHost  string   `json:"rule_host"`
	Mode      string   `json:"mode"`
	Geo       string   `json:"geo"`
	CC        string   `json:"cc"`
	Tag       string   `json:"tag"`
	AllowTags []string `json:"allow_tags"`
	AllowIPs  []string `json:"allow_ips"`
}

func (s *Store) Check(host string, ip netip.Addr) (bool, string) {
	d := s.Decide(host, ip)
	if d.Allowed {
		return true, d.Reason
	}
	return false, "denied:" + d.Mode
}

// Decide 返回请求判定及命中的规则快照。
func (s *Store) Decide(host string, ip netip.Addr) Decision {
	if s == nil || !ip.IsValid() {
		return Decision{Allowed: true, Reason: "no-store"}
	}
	ip = ip.Unmap()
	// 内网/保留地址始终放行，避免锁死内网管理与容器互访
	if geoip.IsPrivate(ip) {
		return Decision{Allowed: true, Reason: "lan"}
	}
	h := normalizeHost(host)
	if h == "" {
		return Decision{Allowed: true, Reason: "no-host"}
	}

	s.mu.RLock()
	rule, matchedHost := s.matchRuleLocked(h)
	s.mu.RUnlock()

	if rule == nil || !rule.raw.Enabled {
		return Decision{Allowed: true, Reason: "unrestricted"}
	}

	if s.allowedByRule(rule, ip) {
		return Decision{Allowed: true, Reason: "allowed"}
	}

	s.mu.Lock()
	st := s.stats[matchedHost]
	if st == nil {
		st = &ruleStat{}
		s.stats[matchedHost] = st
	}
	st.blockedCount++
	s.statsVersion++
	st.lastBlockedIP = ip.String()
	st.lastBlockedAt = time.Now().UnixMilli()
	s.mu.Unlock()

	d := Decision{RuleHost: matchedHost, Mode: rule.raw.Mode, AllowTags: append([]string{}, rule.raw.AllowTags...), AllowIPs: append([]string{}, rule.effectiveIPs...), Geo: "未知"}
	if s.geo != nil {
		p := s.geo.Lookup(ip)
		d.Geo, d.CC = p.Label(), p.CC
	}
	if s.tags != nil {
		d.Tag = s.tags.Lookup(ip)
	}
	switch {
	case d.Mode == ModeAllowlist:
		d.Reason = "未命中允许的 IP / 网段或 IP 标识"
	case d.CC == "":
		d.Reason = "无法确认来源为中国大陆，且未命中额外放行的 IP / 网段或标识"
	default:
		d.Reason = "来源不属于中国大陆，且未命中额外放行的 IP / 网段或标识"
	}
	return d
}

func (s *Store) matchRuleLocked(host string) (*compiledRule, string) {
	for i := range s.rules {
		for _, p := range s.rules[i].patterns {
			if p.wildcard == "" && p.host == host {
				return &s.rules[i], p.host
			}
		}
	}
	for i := range s.rules {
		for _, p := range s.rules[i].patterns {
			w := p.wildcard
			if w != "" && strings.HasSuffix(host, w) && len(host) > len(w) {
				return &s.rules[i], p.host
			}
		}
	}
	return nil, ""
}

func (s *Store) allowedByRule(r *compiledRule, ip netip.Addr) bool {
	// 1. 检查显式白名单 IP / 网段
	if r.ips[ip] {
		return true
	}
	for _, p := range r.prefixes {
		if p.Contains(ip) {
			return true
		}
	}
	// 2. 检查允许的 IP 标识（如“公司”）
	if len(r.tags) > 0 && s.tags != nil {
		if t := s.tags.Lookup(ip); t != "" && r.tags[t] {
			return true
		}
	}
	// 3. 若模式为“仅限国内 IP”，检查归属地是否为中国大陆（CC == "CN"）
	if r.raw.Mode == ModeDomestic && s.geo != nil {
		if s.geo.Lookup(ip).Domestic() {
			return true
		}
	}
	return false
}

func normalizeHost(h string) string {
	h = strings.TrimSpace(strings.ToLower(h))
	if i := strings.IndexByte(h, ':'); i >= 0 {
		h = h[:i]
	}
	return strings.TrimSuffix(h, ".")
}

func isHostSep(c rune) bool {
	return c == ',' || c == '，' || c == ';' || c == '；' || c == '\n' || c == '\r'
}

func extractRawHosts(r Rule) []string {
	var parts []string
	if len(r.Hosts) > 0 {
		for _, item := range r.Hosts {
			parts = append(parts, strings.FieldsFunc(item, isHostSep)...)
		}
	}
	if len(parts) == 0 && r.Host != "" {
		parts = strings.FieldsFunc(r.Host, isHostSep)
	}
	return parts
}

func compile(rules []Rule) ([]compiledRule, error) {
	if len(rules) > 200 {
		return nil, errors.New("域名规则最多 200 条")
	}
	out := make([]compiledRule, 0, len(rules))
	seen := map[string]bool{}

	for i, r := range rules {
		rawHosts := extractRawHosts(r)
		var hosts []string
		var patterns []hostPattern
		ruleSeen := map[string]bool{}
		for _, rawHost := range rawHosts {
			host := normalizeHost(rawHost)
			if host == "" || ruleSeen[host] {
				continue
			}
			if strings.ContainsAny(host, " /\\?#") || !strings.Contains(host, ".") {
				return nil, fmt.Errorf("第 %d 条：域名 %q 格式不正确", i+1, rawHost)
			}
			wildcard := ""
			if strings.HasPrefix(host, "*.") {
				wildcard = host[1:] // ".example.com"
				if len(wildcard) < 4 {
					return nil, fmt.Errorf("第 %d 条：通配符域名 %q 格式不正确", i+1, rawHost)
				}
			} else if strings.Contains(host, "*") {
				return nil, fmt.Errorf("第 %d 条：通配符仅支持 *.domain.com 形式", i+1)
			}
			if seen[host] {
				return nil, fmt.Errorf("重复的域名规则：%s", host)
			}
			seen[host] = true
			ruleSeen[host] = true
			hosts = append(hosts, host)
			patterns = append(patterns, hostPattern{host: host, wildcard: wildcard})
		}
		if len(hosts) == 0 {
			continue
		}
		hostLabel := strings.Join(hosts, ", ")

		mode := strings.TrimSpace(r.Mode)
		if mode == "" {
			mode = ModeDomestic
		}
		if mode != ModeDomestic && mode != ModeAllowlist {
			return nil, fmt.Errorf("第 %d 条（%s）：不支持的访问模式", i+1, hostLabel)
		}

		tagMap := map[string]bool{}
		cleanTags := make([]string, 0, len(r.AllowTags))
		for _, t := range r.AllowTags {
			t = strings.TrimSpace(t)
			if t != "" && !tagMap[t] {
				tagMap[t] = true
				cleanTags = append(cleanTags, t)
			}
		}

		ipMap := map[netip.Addr]bool{}
		var prefixes []netip.Prefix
		cleanIPs := make([]string, 0, len(r.AllowIPs))
		seenIP := map[string]bool{}
		for _, rawIP := range r.AllowIPs {
			for _, part := range strings.FieldsFunc(rawIP, func(c rune) bool {
				return c == ',' || c == '，' || c == ';' || c == ' ' || c == '\n' || c == '\t'
			}) {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				if strings.Contains(part, "/") {
					p, err := netip.ParsePrefix(part)
					if err != nil {
						return nil, fmt.Errorf("第 %d 条（%s）：网段 %q 格式不正确", i+1, hostLabel, part)
					}
					p = netip.PrefixFrom(p.Addr().Unmap(), p.Bits()).Masked()
					s := p.String()
					if !seenIP[s] {
						seenIP[s] = true
						prefixes = append(prefixes, p)
						cleanIPs = append(cleanIPs, s)
					}
				} else {
					ip, err := netip.ParseAddr(part)
					if err != nil {
						return nil, fmt.Errorf("第 %d 条（%s）：IP %q 格式不正确", i+1, hostLabel, part)
					}
					ip = ip.Unmap()
					s := ip.String()
					if !seenIP[s] {
						seenIP[s] = true
						ipMap[ip] = true
						cleanIPs = append(cleanIPs, s)
					}
				}
			}
		}

		if mode == ModeAllowlist && len(cleanTags) == 0 && len(cleanIPs) == 0 && len(r.AllowSets) == 0 {
			return nil, fmt.Errorf("第 %d 条（%s）：选择“仅指定 IP / 标识”时，请至少选择一个地址集、标识或填写一个允许的 IP", i+1, hostLabel)
		}

		note := strings.TrimSpace(r.Note)
		if len([]rune(note)) > 100 {
			return nil, fmt.Errorf("第 %d 条（%s）：备注不能超过 100 个字符", i+1, hostLabel)
		}

		norm := Rule{
			Host:      hostLabel,
			Hosts:     hosts,
			Enabled:   r.Enabled,
			Mode:      mode,
			AllowTags: cleanTags,
			AllowSets: append([]string{}, r.AllowSets...),
			AllowIPs:  cleanIPs,
			Note:      note,
		}
		out = append(out, compiledRule{
			raw:      norm,
			host:     hostLabel,
			patterns: patterns,
			tags:     tagMap,
			ips:      ipMap,
			prefixes: prefixes,
		})
	}
	return out, nil
}

func persist(path string, rules any) error {
	b, err := json.MarshalIndent(rules, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".host-rules-*")
	if err != nil {
		return errors.New("无法保存域名访问控制配置，请检查数据目录权限")
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
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		return errors.New("无法保存域名访问控制配置，请检查数据目录权限")
	}
	return nil
}

// RoutingRule 仅用于 Nginx 本地决定是否调用鉴权，不包含白名单或备注。
// 保留停用规则以维持“精确规则优先、泛域名按配置顺序”的覆盖语义。
type RoutingRule struct {
	Host    string `json:"host"`
	Enabled bool   `json:"enabled"`
}

func (s *Store) RoutingRules() []RoutingRule {
	out := []RoutingRule{}
	if s == nil {
		return out
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.rules {
		for _, p := range r.patterns {
			out = append(out, RoutingRule{Host: p.host, Enabled: r.raw.Enabled})
		}
	}
	return out
}
