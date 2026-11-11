package hostacl

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/duskedge/ipguard/internal/iptag"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
)

// AddressSet 是可被多个域名引用的具名 IP / CIDR 白名单。ID 在改名时保持不变。
type AddressSet struct {
	Tag  string   `json:"tag,omitempty"`
	ID   string   `json:"id"`
	Name string   `json:"name"`
	IPs  []string `json:"ips"`
	Note string   `json:"note,omitempty"`
}
type AddressSetView struct {
	AddressSet
	Hosts []string `json:"hosts"`
}

func (s *Store) loadSets(dir string) error {
	s.sets = []AddressSet{}
	if dir == "" {
		return nil
	}
	s.setsPath = filepath.Join(dir, "address_sets.json")
	b, err := os.ReadFile(s.setsPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var sets []AddressSet
	if err := json.Unmarshal(b, &sets); err != nil {
		return fmt.Errorf("放行地址集配置无法解析：%w", err)
	}
	s.sets, err = normalizeSets(sets)
	return err
}
func normalizeSets(sets []AddressSet) ([]AddressSet, error) {
	if len(sets) > 100 {
		return nil, fmt.Errorf("放行地址集最多 100 个")
	}
	out := make([]AddressSet, 0, len(sets))
	ids := map[string]bool{}
	names := map[string]bool{}
	total := 0
	for _, a := range sets {
		a.Tag = strings.TrimSpace(a.Tag)
		if err := iptag.ValidateTag(a.Tag); err != nil {
			return nil, fmt.Errorf("地址集 %s：%w", a.Name, err)
		}
		a.Name = strings.TrimSpace(a.Name)
		a.Note = strings.TrimSpace(a.Note)
		if a.Name == "" || len([]rune(a.Name)) > 40 {
			return nil, fmt.Errorf("地址集名称须为 1–40 个字符")
		}
		if names[a.Name] {
			return nil, fmt.Errorf("地址集名称重复：%s", a.Name)
		}
		names[a.Name] = true
		if len([]rune(a.Note)) > 100 {
			return nil, fmt.Errorf("地址集 %s：备注最多 100 字", a.Name)
		}
		if a.ID == "" {
			b := make([]byte, 16)
			if _, err := rand.Read(b); err != nil {
				return nil, err
			}
			a.ID = hex.EncodeToString(b)
		}
		if len(a.ID) > 64 || ids[a.ID] {
			return nil, fmt.Errorf("地址集 ID 无效或重复")
		}
		ids[a.ID] = true
		// 与直接地址共用校验、规范化和去重逻辑。
		rules, err := compile([]Rule{{Host: "address-set.local", Mode: ModeDomestic, AllowIPs: a.IPs}})
		if err != nil {
			return nil, fmt.Errorf("地址集 %s：%w", a.Name, err)
		}
		a.IPs = rules[0].raw.AllowIPs
		if len(a.IPs) == 0 || len(a.IPs) > 1000 {
			return nil, fmt.Errorf("地址集 %s 须包含 1–1000 个 IP / 网段", a.Name)
		}
		total += len(a.IPs)
		if total > 10000 {
			return nil, fmt.Errorf("全部地址集合计最多 10000 个 IP / 网段")
		}
		out = append(out, a)
	}
	return out, nil
}

// bindSets 在发布快照前展开引用；热路径仍只查询预编译地址，不读取磁盘。
func bindSets(rules []compiledRule, sets []AddressSet) error {
	byID := map[string]AddressSet{}
	for _, a := range sets {
		byID[a.ID] = a
	}
	for i := range rules {
		r := &rules[i]
		ips := append([]string{}, r.raw.AllowIPs...)
		refs := []string{}
		seen := map[string]bool{}
		for _, id := range r.raw.AllowSets {
			a, ok := byID[id]
			if !ok {
				return fmt.Errorf("域名 %s 引用了不存在的地址集；请先移除引用再删除集合", r.host)
			}
			if !seen[id] {
				refs = append(refs, id)
				ips = append(ips, a.IPs...)
				seen[id] = true
			}
		}
		r.raw.AllowSets = refs
		expanded, err := compile([]Rule{{Host: r.host, Mode: ModeDomestic, AllowIPs: ips}})
		if err != nil {
			return err
		}
		r.ips = expanded[0].ips
		r.prefixes = expanded[0].prefixes
		r.effectiveIPs = expanded[0].raw.AllowIPs
	}
	return nil
}
func (s *Store) AddressSets() []AddressSetView {
	out := []AddressSetView{}
	if s == nil {
		return out
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, a := range s.sets {
		a.IPs = append([]string{}, a.IPs...)
		v := AddressSetView{AddressSet: a, Hosts: []string{}}
		for _, r := range s.rules {
			for _, id := range r.raw.AllowSets {
				if id == a.ID {
					v.Hosts = append(v.Hosts, r.raw.Hosts...)
					break
				}
			}
		}
		out = append(out, v)
	}
	return out
}
func (s *Store) SaveAddressSets(sets []AddressSet) ([]AddressSetView, error) {
	normalized, err := normalizeSets(sets)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	err = s.saveAddressSetsLocked(normalized)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return s.AddressSets(), nil
}

// AddAddressSetIP 在锁内基于最新集合追加，避免快速操作覆盖其他集合修改。
func (s *Store) AddAddressSetIP(id, rawIP string) (bool, error) {
	ip, err := netip.ParseAddr(strings.TrimSpace(rawIP))
	if err != nil {
		return false, fmt.Errorf("IP 格式不正确")
	}
	ip = ip.Unmap()
	s.mu.Lock()
	defer s.mu.Unlock()
	sets := append([]AddressSet{}, s.sets...)
	index := -1
	for i, a := range sets {
		if a.ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		return false, fmt.Errorf("地址集不存在，请刷新后重试")
	}
	for _, target := range sets[index].IPs {
		if target == ip.String() {
			return false, nil
		}
		if prefix, err := netip.ParsePrefix(target); err == nil && prefix.Contains(ip) {
			return false, nil
		}
	}
	sets[index].IPs = append(append([]string{}, sets[index].IPs...), ip.String())
	normalized, err := normalizeSets(sets)
	if err != nil {
		return false, err
	}
	if err = s.saveAddressSetsLocked(normalized); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) saveAddressSetsLocked(normalized []AddressSet) error {
	raw := make([]Rule, len(s.rules))
	for i, r := range s.rules {
		raw[i] = r.raw
	}
	compiled, err := compile(raw)
	if err == nil {
		err = bindSets(compiled, normalized)
	}
	if err == nil && s.setsPath != "" {
		err = persist(s.setsPath, normalized)
	}
	if err != nil {
		return err
	}
	s.sets = normalized
	s.rules = compiled
	s.syncSetTags()
	return nil
}

func (s *Store) syncSetTags() {
	if s.tags == nil {
		return
	}
	items := []iptag.Item{}
	for _, a := range s.sets {
		if a.Tag != "" {
			for _, ip := range a.IPs {
				items = append(items, iptag.Item{Target: ip, Tag: a.Tag})
			}
		}
	}
	s.tags.SetDerived(items)
}

// InAnyAddressSet 判断某 IP 是否出现在任一放行地址集中（精确 IP 或 CIDR 网段）。
func (s *Store) InAnyAddressSet(ip netip.Addr) bool {
	if s == nil || !ip.IsValid() {
		return false
	}
	ip = ip.Unmap()
	ipStr := ip.String()
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, a := range s.sets {
		for _, target := range a.IPs {
			if target == ipStr {
				return true
			}
			if prefix, err := netip.ParsePrefix(target); err == nil && prefix.Contains(ip) {
				return true
			}
		}
	}
	return false
}
