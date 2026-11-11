// Package logparser 解析 npm-guard（Nginx Proxy Manager）的访问日志，并增量 tail 日志目录。
//
// 支持两种格式：
//
//   - NPM 自带格式（proxy-host-*_access.log / fallback_http_access.log 等），带真实客户端 IP、
//     域名与后端地址，是首选数据源：
//     [03/Nov/2026:04:15:58 +0000] - 200 200 - GET https app.example.com "/sw.js" [Client 1.2.3.4] [Length 1475] [Gzip -] [Sent-to 192.168.1.250] "UA" "referer"
//     [03/Nov/2026:02:58:13 +0000] 400 - GET http 192.0.2.1 "/" [Client 1.2.3.4] [Length 252] [Gzip -] "UA" "-"
//   - 标准 nginx combined 格式（兜底，无域名/后端）：
//     1.2.3.4 - - [04/Nov/2026:01:40:14 +0000] "GET /path HTTP/2.0" 200 3 "-" "UA"
package logparser

import (
	"errors"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Entry 是一条访问记录。
type Entry struct {
	TS      int64 // 毫秒
	IP      netip.Addr
	Method  string
	Host    string
	Path    string
	Status  int
	Size    int64
	UA      string
	Backend string
}

var (
	npmRe      = regexp.MustCompile(`^\[([^\]]+)\] (.*?) - (\S+) (\S+) (\S+) "((?:[^"\\]|\\.)*)" \[Client ([^\]]+)\] \[Length ([^\]]*)\](?: \[Gzip [^\]]*\])?(?: \[Sent-to ([^\]]*)\])? "((?:[^"\\]|\\.)*)"`)
	combinedRe = regexp.MustCompile(`^(\S+) \S+ \S+ \[([^\]]+)\] "((?:[^"\\]|\\.)*)" (\d{3}) (\d+|-)(?: "((?:[^"\\]|\\.)*)" "((?:[^"\\]|\\.)*)")?`)
)

const timeLayout = "02/Jan/2006:15:04:05 -0700"

// ErrSkip 表示这一行不是访问记录（空行、其他格式），调用方静默跳过即可。
var ErrSkip = errors.New("not an access log line")

// Parse 解析一行日志。
func Parse(line string) (Entry, error) {
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return Entry{}, ErrSkip
	}
	if line[0] == '[' {
		return parseNPM(line)
	}
	return parseCombined(line)
}

func parseNPM(line string) (Entry, error) {
	m := npmRe.FindStringSubmatch(line)
	if m == nil {
		return Entry{}, ErrSkip
	}
	ts, err := time.Parse(timeLayout, m[1])
	if err != nil {
		return Entry{}, ErrSkip
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(m[7]))
	if err != nil {
		return Entry{}, ErrSkip
	}
	// m[2] 在 proxy-host 里是 "<cache> <upstream_status> <status>"，fallback 里只有 "<status>"。
	// 两者的最后一个字段都是返回给客户端的状态码。
	fields := strings.Fields(m[2])
	status := 0
	if len(fields) > 0 {
		status, _ = strconv.Atoi(fields[len(fields)-1])
	}
	size, _ := strconv.ParseInt(strings.TrimSpace(m[8]), 10, 64)
	backend := m[9]
	if backend == "-" {
		backend = ""
	}
	return Entry{
		TS:      ts.UnixMilli(),
		IP:      ip.Unmap(),
		Method:  m[3],
		Host:    m[5],
		Path:    unescape(m[6]),
		Status:  status,
		Size:    size,
		UA:      dash(unescape(m[10])),
		Backend: backend,
	}, nil
}

func parseCombined(line string) (Entry, error) {
	m := combinedRe.FindStringSubmatch(line)
	if m == nil {
		return Entry{}, ErrSkip
	}
	ip, err := netip.ParseAddr(m[1])
	if err != nil {
		return Entry{}, ErrSkip
	}
	ts, err := time.Parse(timeLayout, m[2])
	if err != nil {
		return Entry{}, ErrSkip
	}
	method, path := "", unescape(m[3])
	if parts := strings.SplitN(path, " ", 3); len(parts) >= 2 {
		method, path = parts[0], parts[1]
	}
	status, _ := strconv.Atoi(m[4])
	size, _ := strconv.ParseInt(m[5], 10, 64)
	return Entry{
		TS:     ts.UnixMilli(),
		IP:     ip.Unmap(),
		Method: method,
		Path:   path,
		Status: status,
		Size:   size,
		UA:     dash(unescape(m[7])),
	}, nil
}

func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	return strings.NewReplacer(`\"`, `"`, `\\`, `\`).Replace(s)
}

func dash(s string) string {
	if s == "-" {
		return ""
	}
	return s
}
