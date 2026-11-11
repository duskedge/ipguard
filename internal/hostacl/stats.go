package hostacl

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// RuleTotal 是累计统计的持久化快照，独立于采样明细的保留与清理。
type RuleTotal struct {
	Host   string `json:"host"`
	Count  uint64 `json:"count"`
	LastIP string `json:"last_ip,omitempty"`
	LastAt int64  `json:"last_at,omitempty"`
}
type statsFile struct {
	Schema int         `json:"schema"`
	Rules  []RuleTotal `json:"rules"`
}

func (s *Store) loadStats(dataDir string) error {
	if dataDir == "" {
		return nil
	}
	s.statsPath = filepath.Join(dataDir, "host_rule_stats.json")
	data, err := os.ReadFile(s.statsPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return errors.New("无法读取域名累计拦截统计")
	}
	var saved statsFile
	if json.Unmarshal(data, &saved) != nil || saved.Schema != 1 {
		return errors.New("域名累计拦截统计文件无法解析")
	}
	for _, row := range saved.Rules {
		if row.Host != "" {
			s.stats[row.Host] = &ruleStat{blockedCount: row.Count, lastBlockedIP: row.LastIP, lastBlockedAt: row.LastAt}
		}
	}
	s.statsLoaded = true
	return nil
}

// BootstrapStats 只在从未保存累计计数时，使用尚存明细作为可恢复的统计起点。
// 一旦有累计文件，不再按明细回填，避免重启时重复累加或受清理影响。
func (s *Store) BootstrapStats(load func() ([]RuleTotal, error)) error {
	s.mu.RLock()
	loaded := s.statsLoaded
	s.mu.RUnlock()
	if loaded {
		return nil
	}
	rows, err := load()
	if err != nil {
		return err
	}
	s.mu.Lock()
	for _, row := range rows {
		if row.Host != "" {
			s.stats[row.Host] = &ruleStat{blockedCount: row.Count, lastBlockedIP: row.LastIP, lastBlockedAt: row.LastAt}
		}
	}
	s.statsVersion++
	s.mu.Unlock()
	return s.FlushStats()
}

// FlushStats 原子替换统计文件；鉴权请求本身只累加内存，不逐请求写盘。
func (s *Store) FlushStats() error {
	if s == nil || s.statsPath == "" {
		return nil
	}
	s.statsFlushMu.Lock()
	defer s.statsFlushMu.Unlock()
	s.mu.RLock()
	if s.statsLoaded && s.statsVersion == s.statsSavedVersion {
		s.mu.RUnlock()
		return nil
	}
	version := s.statsVersion
	saved := statsFile{Schema: 1, Rules: make([]RuleTotal, 0, len(s.stats))}
	for host, st := range s.stats {
		saved.Rules = append(saved.Rules, RuleTotal{Host: host, Count: st.blockedCount, LastIP: st.lastBlockedIP, LastAt: st.lastBlockedAt})
	}
	s.mu.RUnlock()
	data, err := json.Marshal(saved)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.statsPath), ".host-rule-stats-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), s.statsPath)
	}
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.statsLoaded = true
	s.statsSavedVersion = version
	s.mu.Unlock()
	return nil
}

// RunStats 每 5 秒仅保存有变化的计数。优雅停服在 HTTP 请求结束后另做最终保存。
func (s *Store) RunStats(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.FlushStats(); err != nil {
				slog.Error("保存域名累计拦截统计失败", "error", err)
			}
		}
	}
}
