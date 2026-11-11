// Package npmconf 读取 Nginx Proxy Manager 生成的 proxy_host/*.conf，得到“域名 → 后端地址:端口”。
//
// 为什么需要：npm-guard 的访问日志只记 [Sent-to $server]，没有端口。npm-guard 在 macvlan 网络里，
// 所有反代都打到宿主机的 shim 地址（192.168.1.250），只有端口能区分是哪个服务。
// 配置文件里 server_name / $server / $port 都是 NPM 按固定模板生成的，解析很稳定。
package npmconf

import (
	"bufio"
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

// Backend 是一个反代目标。
type Backend struct {
	Scheme string
	Server string
	Port   string
}

// Resolver 按域名查后端，定期重读配置目录（文件有变化才重新解析）。并发安全。
type Resolver struct {
	dir     string
	aliases map[string]string

	mu      sync.RWMutex
	hosts   map[string]Backend
	sig     string
	checked time.Time
}

var (
	serverNameRe = regexp.MustCompile(`^\s*server_name\s+([^;]+);`)
	setRe        = regexp.MustCompile(`^\s*set\s+\$(server|port|forward_scheme)\s+"?([^";\s]+)"?\s*;`)
)

// New 创建解析器。dir 为空或不存在时 Lookup 一律返回 false（退回日志里的原始值）。
// aliases 把后端地址换成好记的名字，如 {"192.168.1.250": "HOST"}。
func New(dir string, aliases map[string]string) *Resolver {
	r := &Resolver{dir: dir, aliases: aliases, hosts: map[string]Backend{}}
	r.reload(true)
	return r
}

// ParseAliases 解析 "192.168.1.250=HOST,10.0.0.2=pve" 形式的别名配置。
func ParseAliases(s string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && strings.TrimSpace(k) != "" && strings.TrimSpace(v) != "" {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}

// Count 返回已解析的域名数。
func (r *Resolver) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.hosts)
}

// Hosts 返回已解析的全部域名（排序后）。
func (r *Resolver) Hosts() []string {
	r.reload(false)
	r.mu.RLock()
	out := make([]string, 0, len(r.hosts))
	for h := range r.hosts {
		if h != "" && h != "_" {
			out = append(out, h)
		}
	}
	r.mu.RUnlock()
	sort.Strings(out)
	return out
}

// Label 返回后端的展示文本：优先按域名查配置（带端口），否则用日志里的原始地址；都套用别名。
func (r *Resolver) Label(host, raw string) string {
	r.reload(false)
	r.mu.RLock()
	b, ok := r.hosts[strings.ToLower(host)]
	r.mu.RUnlock()
	if ok && (raw == "" || raw == b.Server) {
		return r.alias(b.Server) + ":" + b.Port
	}
	return r.alias(raw)
}

func (r *Resolver) alias(server string) string {
	if a, ok := r.aliases[server]; ok {
		return a
	}
	return server
}

// reload 最多每 30 秒检查一次目录；文件名+修改时间+大小的签名变了才重新解析。
func (r *Resolver) reload(force bool) {
	if r.dir == "" {
		return
	}
	r.mu.RLock()
	recent := !force && time.Since(r.checked) < 30*time.Second
	r.mu.RUnlock()
	if recent {
		return
	}
	files, _ := filepath.Glob(filepath.Join(r.dir, "*.conf"))
	var sb strings.Builder
	for _, f := range files {
		if st, err := os.Stat(f); err == nil {
			sb.WriteString(f)
			sb.WriteString(st.ModTime().String())
			sb.WriteString(strconv.FormatInt(st.Size(), 10))
		}
	}
	sig := sb.String()

	r.mu.Lock()
	r.checked = time.Now()
	if sig == r.sig {
		r.mu.Unlock()
		return
	}
	r.mu.Unlock()

	hosts := map[string]Backend{}
	for _, f := range files {
		for _, h := range parseFile(f) {
			hosts[h.name] = h.b
		}
	}
	r.mu.Lock()
	r.hosts, r.sig = hosts, sig
	r.mu.Unlock()
	if force || len(files) > 0 {
		slog.Info("已读取 npm 反代配置", "dir", r.dir, "files", len(files), "hosts", len(hosts))
	}
}

type entry struct {
	name string
	b    Backend
}

func parseFile(path string) []entry {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var names []string
	var b Backend
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if m := serverNameRe.FindStringSubmatch(line); m != nil && names == nil {
			names = strings.Fields(m[1])
			continue
		}
		// 只取第一次出现的值：server 级别的 set 在 location 之前
		if m := setRe.FindStringSubmatch(line); m != nil {
			switch m[1] {
			case "server":
				if b.Server == "" {
					b.Server = m[2]
				}
			case "port":
				if b.Port == "" {
					b.Port = m[2]
				}
			case "forward_scheme":
				if b.Scheme == "" {
					b.Scheme = m[2]
				}
			}
		}
	}
	if b.Server == "" || b.Port == "" {
		return nil
	}
	out := make([]entry, 0, len(names))
	for _, n := range names {
		out = append(out, entry{strings.ToLower(n), b})
	}
	return out
}
