package geoip

import (
	"log/slog"
	"net/netip"
	"os"
	"strings"
	"sync"

	"github.com/lionsoul2014/ip2region/binding/golang/xdb"
)

// regionDB 包装 ip2region（xdb，整库读进内存约 11MB）。
//
// 为什么要两个库：dbip-city-lite 对国内运营商网段的城市定位很差（常把某省运营商整段标到别的城市，
// 区县级英文名），ip2region 的国内数据是中文、到地级市、带运营商；但它的国外数据粗糙。
// 所以国内（中国大陆）用 ip2region，国外和坐标仍用 dbip。
type regionDB struct {
	mu   sync.Mutex // Searcher 内部有 ioCount 计数，不是并发安全的
	s    *xdb.Searcher
	path string
}

func openRegion(paths ...string) *regionDB {
	for _, p := range paths {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err != nil {
			continue
		}
		buf, err := xdb.LoadContentFromFile(p)
		if err != nil {
			slog.Warn("ip2region 库读取失败", "path", p, "error", err)
			continue
		}
		hdr, err := xdb.LoadHeaderFromBuff(buf)
		if err != nil {
			slog.Warn("ip2region 库头部无效", "path", p, "error", err)
			continue
		}
		ver, err := xdb.VersionFromHeader(hdr)
		if err != nil {
			slog.Warn("ip2region 库版本无法识别", "path", p, "error", err)
			continue
		}
		s, err := xdb.NewWithBuffer(ver, buf)
		if err != nil {
			slog.Warn("ip2region 库打开失败", "path", p, "error", err)
			continue
		}
		slog.Info("ip2region 库已加载", "path", p, "ip_version", ver.Name)
		return &regionDB{s: s, path: p}
	}
	slog.Warn("未找到 ip2region 库，国内归属地只用 dbip（城市可能不准）")
	return nil
}

// cnRegion 是 ip2region 的一条国内结果。
type cnRegion struct {
	Province string
	City     string
	ISP      string
}

// lookup 只返回中国大陆的结果；港澳台、国外、查不到都返回 ok=false。
func (r *regionDB) lookup(ip netip.Addr) (cnRegion, bool) {
	if r == nil || !ip.Is4() {
		return cnRegion{}, false
	}
	r.mu.Lock()
	raw, err := r.s.Search(ip.AsSlice())
	r.mu.Unlock()
	if err != nil {
		return cnRegion{}, false
	}
	return parseRegion(raw)
}

// parseRegion 兼容两种格式：
//
//	新版：国家|省份|城市|ISP|国家码     例：中国|浙江省|杭州市|移动|CN
//	旧版：国家|区域|省份|城市|ISP       例：中国|0|浙江省|杭州市|电信
func parseRegion(raw string) (cnRegion, bool) {
	f := strings.Split(raw, "|")
	if len(f) < 5 || f[0] != "中国" {
		return cnRegion{}, false
	}
	var prov, city, isp string
	if len(f[4]) == 2 && strings.ToUpper(f[4]) == f[4] && f[4] != "0" {
		prov, city, isp = f[1], f[2], f[3]
	} else {
		prov, city, isp = f[2], f[3], f[4]
	}
	prov, city, isp = clean0(prov), clean0(city), clean0(isp)
	prov = cleanProvince(prov)
	switch prov {
	case "香港", "澳门", "台湾":
		return cnRegion{}, false // 港澳台单列，交给 dbip 判定 HK/MO/TW
	case "":
		return cnRegion{}, false
	}
	return cnRegion{Province: prov, City: cleanCity(city), ISP: isp}, true
}

func clean0(s string) string {
	s = strings.TrimSpace(s)
	if s == "0" {
		return ""
	}
	return s
}

// provinceCenter 是各省会坐标（WGS-84），国内 IP 在地图上落到省会。
var provinceCenter = map[string][2]float64{
	"北京": {39.90, 116.41}, "天津": {39.13, 117.20}, "上海": {31.23, 121.47}, "重庆": {29.56, 106.55},
	"河北": {38.04, 114.51}, "山西": {37.87, 112.55}, "辽宁": {41.80, 123.43}, "吉林": {43.82, 125.32},
	"黑龙江": {45.80, 126.53}, "江苏": {32.06, 118.80}, "浙江": {30.27, 120.15}, "安徽": {31.82, 117.23},
	"福建": {26.07, 119.30}, "江西": {28.68, 115.86}, "山东": {36.65, 117.12}, "河南": {34.75, 113.62},
	"湖北": {30.59, 114.31}, "湖南": {28.23, 112.94}, "广东": {23.13, 113.26}, "海南": {20.04, 110.20},
	"四川": {30.57, 104.07}, "贵州": {26.65, 106.63}, "云南": {25.04, 102.71}, "陕西": {34.34, 108.94},
	"甘肃": {36.06, 103.83}, "青海": {36.62, 101.78}, "内蒙古": {40.84, 111.75}, "广西": {22.82, 108.37},
	"西藏": {29.65, 91.14}, "宁夏": {38.49, 106.23}, "新疆": {43.83, 87.62},
}
