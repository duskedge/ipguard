package logparser

import (
	"bufio"
	"compress/gzip"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// maxPathLen 防止异常超长的请求路径（扫描器常见）撑爆内存
const maxPathLen = 2048

// Tailer 轮询日志目录，增量读取匹配的文件。
//
// 不用 fsnotify：日志目录常从其他主机或网络存储只读挂进来，部分文件系统（SMB/overlay）上
// inotify 不可靠；几秒一次的 stat 轮询对几十个文件来说开销可以忽略。
type Tailer struct {
	Dir      string
	Patterns []string
	Exclude  []string
	Interval time.Duration
	// Since 返回初次加载时的截止时间（早于它的记录丢弃），一般是 now - retention
	Since func() time.Time
	// Emit 收到一批新解析出的记录（可能跨文件、未排序）
	Emit func([]Entry)

	mu    sync.Mutex
	files map[string]*fileState
	stats Stats
}

// Stats 用于健康检查展示。
type Stats struct {
	Files     int       `json:"files"`
	Lines     int64     `json:"lines"`
	Skipped   int64     `json:"skipped"`
	LastScan  time.Time `json:"last_scan"`
	LastError string    `json:"last_error,omitempty"`
}

type fileState struct {
	info   os.FileInfo
	offset int64
}

var rotatedRe = regexp.MustCompile(`\.(\d+)(\.gz)?$`)

// Run 先做一次全量加载（含轮转出来的历史文件），然后按 Interval 轮询直到 ctx 结束。
func (t *Tailer) Run(ctx context.Context) {
	t.files = map[string]*fileState{}
	t.initialLoad()
	tk := time.NewTicker(t.Interval)
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
			t.poll()
		}
	}
}

// Snapshot 返回当前统计。
func (t *Tailer) Snapshot() Stats {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.stats
}

func (t *Tailer) setErr(err error) {
	t.mu.Lock()
	if err != nil {
		t.stats.LastError = err.Error()
	} else {
		t.stats.LastError = ""
	}
	t.mu.Unlock()
}

// Match 报告文件名（不含目录）是否参与解析。轮转后缀会先剥掉再匹配。
func (t *Tailer) Match(name string) bool {
	base := rotatedRe.ReplaceAllString(name, "")
	for _, ex := range t.Exclude {
		if ok, _ := filepath.Match(ex, base); ok {
			return false
		}
	}
	for _, p := range t.Patterns {
		if ok, _ := filepath.Match(p, base); ok {
			return true
		}
	}
	return false
}

func (t *Tailer) list() (current []string, rotated []string, err error) {
	ents, err := os.ReadDir(t.Dir)
	if err != nil {
		return nil, nil, err
	}
	for _, e := range ents {
		if e.IsDir() || !t.Match(e.Name()) {
			continue
		}
		if rotatedRe.MatchString(e.Name()) {
			rotated = append(rotated, e.Name())
		} else {
			current = append(current, e.Name())
		}
	}
	return current, rotated, nil
}

func (t *Tailer) initialLoad() {
	current, rotated, err := t.list()
	if err != nil {
		slog.Warn("日志目录不可读", "dir", t.Dir, "error", err)
		t.setErr(err)
		t.markScan(0)
		return
	}
	cutoff := t.Since().UnixMilli()

	// 轮转文件按编号从大到小读（越大越旧），保证同一文件内大致有序
	// 所有文件攒成一批再交出去：分批交会让存储端对乱序数据反复整体排序
	sort.Slice(rotated, func(i, j int) bool { return rotNum(rotated[i]) > rotNum(rotated[j]) })
	var batch []Entry
	for _, name := range rotated {
		batch = append(batch, t.readAll(filepath.Join(t.Dir, name), cutoff)...)
	}
	for _, name := range current {
		path := filepath.Join(t.Dir, name)
		st, err := os.Stat(path)
		if err != nil {
			continue
		}
		fs := &fileState{info: st}
		t.files[path] = fs
		batch = append(batch, t.readFrom(path, fs, cutoff)...)
	}
	if len(batch) > 0 {
		t.Emit(batch)
	}
	t.markScan(len(current))
	slog.Info("访问日志已加载", "dir", t.Dir, "files", len(current), "rotated", len(rotated), "lines", t.Snapshot().Lines)
}

func (t *Tailer) poll() {
	current, _, err := t.list()
	if err != nil {
		t.setErr(err)
		return
	}
	t.setErr(nil)
	var batch []Entry
	seen := map[string]bool{}
	for _, name := range current {
		path := filepath.Join(t.Dir, name)
		seen[path] = true
		st, err := os.Stat(path)
		if err != nil {
			continue
		}
		fs, ok := t.files[path]
		switch {
		case !ok:
			// 新出现的文件（新增反代域名）从头读
			fs = &fileState{info: st}
			t.files[path] = fs
		case !os.SameFile(fs.info, st):
			// 被轮转：先把旧文件（现在通常叫 .1）剩下的尾巴读完，再从头读新文件
			if old := filepath.Join(t.Dir, name+".1"); fileIs(old, fs.info) {
				batch = append(batch, t.readFrom(old, fs, 0)...)
			}
			fs.info, fs.offset = st, 0
		case st.Size() < fs.offset:
			// 被截断（copytruncate）
			fs.offset = 0
		}
		fs.info = st
		if st.Size() > fs.offset {
			batch = append(batch, t.readFrom(path, fs, 0)...)
		}
	}
	for p := range t.files {
		if !seen[p] {
			delete(t.files, p)
		}
	}
	if len(batch) > 0 {
		t.Emit(batch)
	}
	t.markScan(len(current))
}

func (t *Tailer) markScan(files int) {
	t.mu.Lock()
	t.stats.Files = files
	t.stats.LastScan = time.Now()
	t.mu.Unlock()
}

func fileIs(path string, info os.FileInfo) bool {
	st, err := os.Stat(path)
	return err == nil && os.SameFile(st, info)
}

func rotNum(name string) int {
	m := rotatedRe.FindStringSubmatch(name)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// readFrom 从 fs.offset 读到最后一个完整行，推进 offset。不完整的末行留到下次。
func (t *Tailer) readFrom(path string, fs *fileState, cutoff int64) []Entry {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	if _, err := f.Seek(fs.offset, io.SeekStart); err != nil {
		return nil
	}
	r := bufio.NewReaderSize(f, 64*1024)
	var out []Entry
	var lines, skipped int64
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			// 没有换行结尾的半行不消费
			break
		}
		fs.offset += int64(len(line))
		lines++
		e, perr := Parse(line)
		if perr != nil {
			skipped++
			continue
		}
		if e.TS < cutoff {
			continue
		}
		out = append(out, clip(e))
	}
	t.count(lines, skipped)
	return out
}

func (t *Tailer) readAll(path string, cutoff int64) []Entry {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var src io.Reader = f
	if strings.HasSuffix(path, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil
		}
		defer gz.Close()
		src = gz
	}
	sc := bufio.NewScanner(src)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	var out []Entry
	var lines, skipped int64
	for sc.Scan() {
		lines++
		e, err := Parse(sc.Text())
		if err != nil {
			skipped++
			continue
		}
		if e.TS < cutoff {
			continue
		}
		out = append(out, clip(e))
	}
	t.count(lines, skipped)
	return out
}

func (t *Tailer) count(lines, skipped int64) {
	t.mu.Lock()
	t.stats.Lines += lines
	t.stats.Skipped += skipped
	t.mu.Unlock()
}

func clip(e Entry) Entry {
	if len(e.Path) > maxPathLen {
		e.Path = e.Path[:maxPathLen]
	}
	if len(e.UA) > 512 {
		e.UA = e.UA[:512]
	}
	return e
}
