// Package geoip 用城市级 mmdb（dbip-city-lite / GeoLite2-City）解析 IP 归属地。
//
// 约定：
//   - 中国大陆 cc=CN，计入“国内”；中国香港/澳门/台湾单列（cc=HK/MO/TW），不计入国内。
//   - RFC1918、回环、链路本地、CGNAT 等保留地址 cc=LAN，显示“内网”，不计入国外。
//   - 库缺失或查不到时返回“未知”（cc 为空），绝不报错。
package geoip

import (
	"log/slog"
	"net/netip"
	"os"
	"strings"
	"sync"

	"github.com/oschwald/geoip2-golang"
)

// Place 是一个 IP 的归属地。
type Place struct {
	CC       string  `json:"cc"`       // ISO 国家码；内网为 LAN；未知为空
	Country  string  `json:"country"`  // 中文国家/地区名
	Province string  `json:"province"` // 省/州（已去掉“省/市/自治区”等后缀）
	City     string  `json:"city"`
	ISP      string  `json:"isp"` // 运营商（仅国内，来自 ip2region）
	Lat      float64 `json:"lat"`
	Lng      float64 `json:"lng"`
}

// Label 返回“国家·城市”形式的展示文本；国内带运营商，如“中国·杭州 · 移动”。
func (p Place) Label() string {
	l := p.label()
	if p.ISP != "" {
		l += " · " + p.ISP
	}
	return l
}

func (p Place) label() string {
	switch {
	case p.CC == "LAN":
		return "内网"
	case p.Country == "":
		return "未知"
	case p.City != "" && p.City != p.Country:
		return p.Country + "·" + p.City
	case p.Province != "" && p.Province != p.Country:
		return p.Country + "·" + p.Province
	default:
		return p.Country
	}
}

// Domestic 报告是否算“国内”（中国大陆）。
func (p Place) Domestic() bool { return p.CC == "CN" }

// Resolver 带缓存的归属地解析器。并发安全，支持热重载。
type Resolver struct {
	mu          sync.RWMutex
	db          *geoip2.Reader
	region      *regionDB
	path        string
	cityPaths   []string
	regionPaths []string
	cache       map[netip.Addr]Place
	version     uint64

	updateMu     sync.Mutex
	updating     bool
	lastUpdateTS int64
	lastError    string
}

func openCityDB(paths []string) (*geoip2.Reader, string) {
	path := ""
	for _, p := range paths {
		if p == "" {
			continue
		}
		if path == "" {
			path = p // 都不存在时，日志里报第一个
		}
		if _, err := os.Stat(p); err == nil {
			path = p
			break
		}
	}
	if path == "" {
		slog.Warn("未配置 GeoIP 库，归属地将显示为未知")
		return nil, ""
	}
	if _, err := os.Stat(path); err != nil {
		slog.Warn("GeoIP 库不存在，归属地将显示为未知", "path", path)
		return nil, path
	}
	db, err := geoip2.Open(path)
	if err != nil {
		slog.Warn("GeoIP 库打开失败，归属地将显示为未知", "path", path, "error", err)
		return nil, path
	}
	md := db.Metadata()
	slog.Info("GeoIP 库已加载", "path", path, "type", md.DatabaseType, "build", md.BuildEpoch)
	return db, path
}

// Open 按顺序尝试候选路径，打开第一个存在的城市库。都不存在时返回一个只能识别内网的
// 降级解析器（不报错）。典型用法：先挂载目录里按月更新的库，再退回镜像里打包的库。
func Open(paths ...string) *Resolver {
	db, path := openCityDB(paths)
	return &Resolver{
		db:        db,
		path:      path,
		cityPaths: append([]string(nil), paths...),
		cache:     map[netip.Addr]Place{},
		version:   1,
	}
}

// WithRegion 加载 ip2region（按顺序取第一个存在的路径），用于修正国内 IP 的省市与运营商。
func (r *Resolver) WithRegion(paths ...string) *Resolver {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.regionPaths = append([]string(nil), paths...)
	r.region = openRegion(paths...)
	r.cache = map[netip.Addr]Place{}
	r.version++
	return r
}

// Version 返回当前库版本，每次热重载后递增。
func (r *Resolver) Version() uint64 {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.version
}

// Loaded 报告库是否可用。
func (r *Resolver) Loaded() bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.db != nil
}

// Info 返回库信息，供健康检查展示。
func (r *Resolver) Info() map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	loaded := r.db != nil
	info := map[string]any{"loaded": loaded, "path": r.path, "ip2region": r.region != nil}
	if loaded {
		md := r.db.Metadata()
		info["type"], info["build_epoch"] = md.DatabaseType, md.BuildEpoch
	}
	return info
}

// Close 释放库文件。
func (r *Resolver) Close() {
	r.mu.Lock()
	old := r.db
	r.db = nil
	r.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
}

// Lookup 解析一个 IP。结果按 IP 缓存（家庭站点的独立 IP 规模很小）。
func (r *Resolver) Lookup(ip netip.Addr) Place {
	if !ip.IsValid() {
		return Place{}
	}
	ip = ip.Unmap()
	if IsPrivate(ip) {
		return Place{CC: "LAN", Country: "内网"}
	}
	r.mu.RLock()
	if p, ok := r.cache[ip]; ok {
		r.mu.RUnlock()
		return p
	}
	p := r.lookupDBLocked(ip)
	p = r.refineCNLocked(ip, p)
	r.mu.RUnlock()

	r.mu.Lock()
	if len(r.cache) > 200_000 {
		// 极端情况下（被大范围扫描）防止无限增长；清空后自然重建
		r.cache = map[netip.Addr]Place{}
	}
	r.cache[ip] = p
	r.mu.Unlock()
	return p
}

// refineCNLocked：dbip 判为中国大陆（或查不到）时，用 ip2region 的省/市/运营商替换，
// 坐标改为省会。dbip 判为别的国家/地区（含港澳台）时不动，以免两库打架。调用方需持有读锁。
func (r *Resolver) refineCNLocked(ip netip.Addr, p Place) Place {
	if p.CC != "CN" && p.CC != "" {
		return p
	}
	cn, ok := r.region.lookup(ip)
	if !ok {
		return p
	}
	out := Place{CC: "CN", Country: "中国", Province: cn.Province, City: cn.City, ISP: cn.ISP}
	if out.City == "" {
		out.City = out.Province
	}
	if c, ok := provinceCenter[out.Province]; ok {
		out.Lat, out.Lng = c[0], c[1]
	} else {
		out.Lat, out.Lng = p.Lat, p.Lng
	}
	return out
}

func (r *Resolver) lookupDBLocked(ip netip.Addr) Place {
	if r.db == nil {
		return Place{}
	}
	rec, err := r.db.City(ip.AsSlice())
	if err != nil || rec == nil {
		return Place{}
	}
	cc := rec.Country.IsoCode
	if cc == "" {
		return Place{}
	}
	p := Place{
		CC:      cc,
		Country: CountryName(cc, rec.Country.Names),
		Lat:     rec.Location.Latitude,
		Lng:     rec.Location.Longitude,
	}
	if len(rec.Subdivisions) > 0 {
		p.Province = provinceName(rec.Subdivisions[0].Names)
	}
	p.City = cityName(rec.City.Names, cc, p.Province)
	return p
}

func provinceName(names map[string]string) string {
	if n := names["zh-CN"]; n != "" {
		return cleanProvince(n)
	}
	if n, ok := provinceZH[names["en"]]; ok {
		return n
	}
	return names["en"]
}

// cityName 优先用库里的中文名，其次查表；国内查不到时退回省名
// （dbip-lite 的城市常是区县级英文名，直接展示没有意义）。
func cityName(names map[string]string, cc, province string) string {
	if n := names["zh-CN"]; n != "" {
		return cleanCity(n)
	}
	en := names["en"]
	if n, ok := cityZH[en]; ok {
		return n
	}
	if cc == "CN" {
		return province
	}
	return en
}

var provinceSuffixes = []string{"维吾尔自治区", "壮族自治区", "回族自治区", "特别行政区", "自治区", "省", "市"}

func cleanProvince(s string) string {
	s = strings.TrimSpace(s)
	for _, suf := range provinceSuffixes {
		if strings.HasSuffix(s, suf) && len(s) > len(suf) {
			return strings.TrimSuffix(s, suf)
		}
	}
	return s
}

func cleanCity(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "市") && len([]rune(s)) > 2 {
		return strings.TrimSuffix(s, "市")
	}
	return s
}

var privatePrefixes = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{
		"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8",
		"169.254.0.0/16", "100.64.0.0/10", "0.0.0.0/8", "198.18.0.0/15",
		"::1/128", "fc00::/7", "fe80::/10",
	} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

// IsPrivate 报告是否为内网/保留地址。
func IsPrivate(ip netip.Addr) bool {
	ip = ip.Unmap()
	for _, p := range privatePrefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}
