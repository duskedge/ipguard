// Package iptag 管理自定义 IP/网段标识（如“公司”“家里”），持久化到 data/ip_tags.json。
package iptag

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// Item 是一条 IP 或 CIDR 的标识配置。
type Item struct {
	Target string `json:"target"`
	Tag    string `json:"tag"`
	Note   string `json:"note,omitempty"`
}

type prefixRule struct {
	prefix netip.Prefix
	tag    string
}

// Store 维护自定义 IP 标识并提供快速查询。并发安全。
type Store struct {
	derivedExact    map[netip.Addr]string
	derivedPrefixes []prefixRule
	derivedTags     []string
	path            string
	mu              sync.RWMutex
	items           []Item
	exact           map[netip.Addr]string
	prefixes        []prefixRule
	tags            []string
	ver             atomic.Uint64
}

// New 加载或初始化标识存储。dataDir 为空时仅在内存运行（供单测使用）。
func New(dataDir string) (*Store, error) {
	s := &Store{
		exact: map[netip.Addr]string{},
		items: []Item{},
		tags:  []string{},
	}
	if dataDir != "" {
		s.path = filepath.Join(dataDir, "ip_tags.json")
		b, err := os.ReadFile(s.path)
		if err == nil {
			var items []Item
			if err := json.Unmarshal(b, &items); err != nil {
				return nil, errors.New("IP 标识配置文件无法解析")
			}
			norm, exact, prefixes, tags, err := compile(items)
			if err != nil {
				return nil, err
			}
			s.items, s.exact, s.prefixes, s.tags = norm, exact, prefixes, tags
		} else if !os.IsNotExist(err) {
			return nil, errors.New("无法读取 IP 标识配置")
		}
	}
	s.ver.Store(1)
	return s, nil
}

// Version 返回配置版本号，供统计缓存感知变更。
func (s *Store) Version() uint64 {
	if s == nil {
		return 0
	}
	return s.ver.Load()
}

// List 返回当前配置的全部条目。
func (s *Store) List() []Item {
	if s == nil {
		return []Item{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Item, len(s.items))
	copy(out, s.items)
	return out
}

// Tags 返回去重排序后的标识名称列表（供前端筛选下拉框使用）。
func (s *Store) Tags() []string {
	if s == nil {
		return []string{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := map[string]bool{}
	for _, t := range s.tags {
		seen[t] = true
	}
	for _, t := range s.derivedTags {
		seen[t] = true
	}
	out := make([]string, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// Lookup 返回某 IP 命中的标识名称；未命中返回空串。单独配置优先于地址集派生标识。
// 各来源内部精确 IP 优先于网段，长掩码优先于短掩码。
func (s *Store) Lookup(ip netip.Addr) string {
	if s == nil || !ip.IsValid() {
		return ""
	}
	ip = ip.Unmap()
	s.mu.RLock()
	defer s.mu.RUnlock()
	if t, ok := s.exact[ip]; ok {
		return t
	}
	for _, r := range s.prefixes {
		if r.prefix.Contains(ip) {
			return r.tag
		}
	}
	if t, ok := s.derivedExact[ip]; ok {
		return t
	}
	for _, r := range s.derivedPrefixes {
		if r.prefix.Contains(ip) {
			return r.tag
		}
	}
	return ""
}

// Save 校验并保存全量标识列表。
func (s *Store) Save(items []Item) ([]Item, error) {
	norm, exact, prefixes, tags, err := compile(items)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path != "" {
		if err := persist(s.path, norm); err != nil {
			return nil, err
		}
	}
	s.items, s.exact, s.prefixes, s.tags = norm, exact, prefixes, tags
	s.ver.Add(1)
	out := make([]Item, len(s.items))
	copy(out, s.items)
	return out, nil
}

func compile(items []Item) ([]Item, map[netip.Addr]string, []prefixRule, []string, error) {
	if len(items) > 500 {
		return nil, nil, nil, nil, errors.New("IP 标识条目最多 500 条")
	}
	norm := make([]Item, 0, len(items))
	exact := map[netip.Addr]string{}
	var prefixes []prefixRule
	seenTarget := map[string]bool{}
	tagSet := map[string]bool{}

	for i, it := range items {
		target := strings.TrimSpace(it.Target)
		tag := strings.TrimSpace(it.Tag)
		note := strings.TrimSpace(it.Note)
		if target == "" && tag == "" {
			continue
		}
		if target == "" {
			return nil, nil, nil, nil, fmt.Errorf("第 %d 条：请填写 IP 或网段", i+1)
		}
		if tag == "" {
			return nil, nil, nil, nil, fmt.Errorf("第 %d 条（%s）：请填写标识名称", i+1, target)
		}
		if strings.HasPrefix(tag, "!") || strings.HasPrefix(tag, "-") || tag == "*" {
			return nil, nil, nil, nil, fmt.Errorf("第 %d 条：标识名称不能以 !、- 开头或为 *", i+1)
		}
		if len([]rune(tag)) > 24 {
			return nil, nil, nil, nil, fmt.Errorf("第 %d 条：标识名称不能超过 24 个字符", i+1)
		}
		if len([]rune(note)) > 100 {
			return nil, nil, nil, nil, fmt.Errorf("第 %d 条：备注不能超过 100 个字符", i+1)
		}
		var canonical string
		if strings.Contains(target, "/") {
			p, err := netip.ParsePrefix(target)
			if err != nil {
				return nil, nil, nil, nil, fmt.Errorf("第 %d 条：网段 %q 格式不正确", i+1, target)
			}
			p = netip.PrefixFrom(p.Addr().Unmap(), p.Bits()).Masked()
			canonical = p.String()
			prefixes = append(prefixes, prefixRule{prefix: p, tag: tag})
		} else {
			ip, err := netip.ParseAddr(target)
			if err != nil {
				return nil, nil, nil, nil, fmt.Errorf("第 %d 条：IP %q 格式不正确", i+1, target)
			}
			ip = ip.Unmap()
			canonical = ip.String()
			exact[ip] = tag
		}
		if seenTarget[canonical] {
			return nil, nil, nil, nil, fmt.Errorf("重复的 IP 或网段：%s", canonical)
		}
		seenTarget[canonical] = true
		tagSet[tag] = true
		norm = append(norm, Item{Target: canonical, Tag: tag, Note: note})
	}

	// 掩码越长越优先匹配
	sort.SliceStable(prefixes, func(i, j int) bool {
		return prefixes[i].prefix.Bits() > prefixes[j].prefix.Bits()
	})

	tags := make([]string, 0, len(tagSet))
	for t := range tagSet {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	return norm, exact, prefixes, tags, nil
}

func persist(path string, items []Item) error {
	b, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".ip-tags-*")
	if err != nil {
		return errors.New("无法保存 IP 标识配置，请检查数据目录权限")
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
		return errors.New("无法保存 IP 标识配置，请检查数据目录权限")
	}
	return nil
}

// MatchFilter 判断某行的 tag 是否匹配筛选参数 want：
//   - ""   ：不过滤
//   - "*"  ：仅看有标识的 IP
//   - "!*" ：排除所有有标识的 IP
//   - "!X" ：排除标识为 X 的 IP
//   - "X"  ：仅看标识为 X 的 IP
func MatchFilter(tag, want string) bool {
	want = strings.TrimSpace(want)
	switch {
	case want == "":
		return true
	case want == "*":
		return tag != ""
	case want == "!*":
		return tag == ""
	case strings.HasPrefix(want, "!"):
		return tag != strings.TrimPrefix(want, "!")
	default:
		return tag == want
	}
}

// MatchQuery 判断 IP 与 tag 是否匹配搜索框输入 q：
//   - 普通关键字：IP 子串或 tag 子串命中即通过
//   - 以 ! 或 - 开头（如 "!公司" 或 "!223.75"）：排除命中该 IP 子串或 tag 子串的记录
func MatchQuery(ip, tag, q string) bool {
	q = strings.TrimSpace(q)
	if q == "" {
		return true
	}
	exclude := false
	if (strings.HasPrefix(q, "!") || strings.HasPrefix(q, "-")) && len(q) > 1 {
		exclude = true
		q = strings.TrimSpace(q[1:])
		if q == "" {
			return true
		}
	}
	kw := strings.ToLower(q)
	hit := strings.Contains(strings.ToLower(ip), kw) || (tag != "" && strings.Contains(strings.ToLower(tag), kw))
	if exclude {
		return !hit
	}
	return hit
}

// ValidateTag 校验标识名称，地址集与单独 IP 标识保持相同约束。
func ValidateTag(tag string) error {
	if strings.HasPrefix(tag, "!") || strings.HasPrefix(tag, "-") || tag == "*" || len([]rune(tag)) > 24 {
		return errors.New("标识最多 24 字，不能以 !、- 开头或为 *")
	}
	return nil
}

// SetDerived 替换地址集派生的标识。输入来自已校验的地址集，不写入单独的 IP 标识文件。
// 同一地址出现在多个集合时首个集合优先；单独配置的标识始终优先。
func (s *Store) SetDerived(items []Item) {
	if s == nil {
		return
	}
	exact := map[netip.Addr]string{}
	prefixes := []prefixRule{}
	tags := map[string]bool{}
	seen := map[string]bool{}
	for _, it := range items {
		if it.Tag == "" || seen[it.Target] {
			continue
		}
		seen[it.Target] = true
		if p, err := netip.ParsePrefix(it.Target); err == nil {
			prefixes = append(prefixes, prefixRule{prefix: p, tag: it.Tag})
		} else if ip, err := netip.ParseAddr(it.Target); err == nil {
			exact[ip.Unmap()] = it.Tag
		} else {
			continue
		}
		tags[it.Tag] = true
	}
	sort.SliceStable(prefixes, func(i, j int) bool { return prefixes[i].prefix.Bits() > prefixes[j].prefix.Bits() })
	names := make([]string, 0, len(tags))
	for tag := range tags {
		names = append(names, tag)
	}
	s.mu.Lock()
	s.derivedExact = exact
	s.derivedPrefixes = prefixes
	s.derivedTags = names
	s.ver.Add(1)
	s.mu.Unlock()
}
