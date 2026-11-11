// Package stats 保存近期访问记录（内存），并按时间窗口做频次 / 归属地聚合。
//
// 家庭站点的日志量（每天几千到几十万行）放内存完全够用：记录按时间升序存放，
// 窗口查询用二分定位起点。字符串字段做了驻留（intern），同一个 UA / 域名只存一份。
package stats

import (
	"net/netip"
	"sort"
	"sync"
	"time"

	"github.com/duskedge/ipguard/internal/logparser"
)

// Record 是一条入库后的访问记录。
type Record struct {
	Seq     uint64
	TS      int64
	IP      netip.Addr
	Method  string
	Host    string
	Path    string
	UA      string
	Backend string
	Status  int
	Size    int64
}

// Store 是按时间排序的环形记录集。
type Store struct {
	mu         sync.RWMutex
	recs       []Record
	seq        uint64
	version    uint64
	retention  time.Duration
	maxEntries int
	strs       map[string]string
	hosts      map[string]int64 // host → 最近一次出现的时间
	now        func() time.Time
}

// NewStore 创建存储。
func NewStore(retention time.Duration, maxEntries int) *Store {
	if maxEntries <= 0 {
		maxEntries = 2_000_000
	}
	return &Store{
		retention:  retention,
		maxEntries: maxEntries,
		strs:       map[string]string{},
		hosts:      map[string]int64{},
		now:        time.Now,
	}
}

func (s *Store) intern(v string) string {
	if v == "" {
		return ""
	}
	if x, ok := s.strs[v]; ok {
		return x
	}
	// 驻留表本身也要防爆：扫描器的 UA / 路径五花八门，满了就重建
	if len(s.strs) > 200_000 {
		s.strs = map[string]string{}
	}
	s.strs[v] = v
	return v
}

// Add 写入一批记录（可乱序），并清理过期数据。
func (s *Store) Add(entries []logparser.Entry) {
	if len(entries) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sorted := true
	last := int64(0)
	if n := len(s.recs); n > 0 {
		last = s.recs[n-1].TS
	}
	for _, e := range entries {
		s.seq++
		r := Record{
			Seq:     s.seq,
			TS:      e.TS,
			IP:      e.IP,
			Method:  s.intern(e.Method),
			Host:    s.intern(e.Host),
			Path:    s.intern(e.Path),
			UA:      s.intern(e.UA),
			Backend: s.intern(e.Backend),
			Status:  e.Status,
			Size:    e.Size,
		}
		if r.TS < last {
			sorted = false
		}
		last = r.TS
		s.recs = append(s.recs, r)
		if r.Host != "" && r.TS > s.hosts[r.Host] {
			s.hosts[r.Host] = r.TS
		}
	}
	if !sorted {
		sort.SliceStable(s.recs, func(i, j int) bool { return s.recs[i].TS < s.recs[j].TS })
	}
	s.pruneLocked()
	s.version++
}

func (s *Store) pruneLocked() {
	cutoff := s.now().Add(-s.retention).UnixMilli()
	i := sort.Search(len(s.recs), func(i int) bool { return s.recs[i].TS >= cutoff })
	if over := len(s.recs) - i - s.maxEntries; over > 0 {
		i += over
	}
	if i > 0 {
		// 复制到新切片，让旧底层数组能被回收
		s.recs = append([]Record(nil), s.recs[i:]...)
	}
	for h, ts := range s.hosts {
		if ts < cutoff {
			delete(s.hosts, h)
		}
	}
}

// Prune 按当前时间清理过期数据（定时调用，日志长时间无新增时也能释放内存）。
func (s *Store) Prune() {
	s.mu.Lock()
	n := len(s.recs)
	s.pruneLocked()
	if len(s.recs) != n {
		s.version++
	}
	s.mu.Unlock()
}

// Version 每次数据变化递增，用于缓存失效。
func (s *Store) Version() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.version
}

// Len 返回当前记录数。
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.recs)
}

// Hosts 返回保留期内出现过的域名（字典序）。
func (s *Store) Hosts() []string {
	s.mu.RLock()
	out := make([]string, 0, len(s.hosts))
	for h := range s.hosts {
		out = append(out, h)
	}
	s.mu.RUnlock()
	sort.Strings(out)
	return out
}

// View 在读锁内回调 [from, to) 时间窗内的记录（升序）。回调里不要持有切片引用。
func (s *Store) View(from, to int64, fn func([]Record)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	lo := sort.Search(len(s.recs), func(i int) bool { return s.recs[i].TS >= from })
	hi := sort.Search(len(s.recs), func(i int) bool { return s.recs[i].TS >= to })
	fn(s.recs[lo:hi])
}
