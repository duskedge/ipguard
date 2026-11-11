package geoip

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"time"

	"github.com/lionsoul2014/ip2region/binding/golang/xdb"
	"github.com/oschwald/geoip2-golang"
)

// DBFileStatus 是单个归属地数据库文件的状态。
type DBFileStatus struct {
	Loaded  bool   `json:"loaded"`
	Path    string `json:"path"`
	Target  string `json:"target"`
	Type    string `json:"type,omitempty"`
	BuildTS int64  `json:"build_ts,omitempty"`
	ModTime int64  `json:"mod_time,omitempty"`
	Size    int64  `json:"size,omitempty"`
}

// StatusView 是 /api/settings/geoip 返回的完整状态。
type StatusView struct {
	City         DBFileStatus `json:"city"`
	Region       DBFileStatus `json:"region"`
	CacheEntries int          `json:"cache_entries"`
	Updating     bool         `json:"updating"`
	LastUpdateTS int64        `json:"last_update_ts"`
	LastError    string       `json:"last_error,omitempty"`
}

func firstNonEmpty(paths []string) string {
	for _, p := range paths {
		if p != "" {
			return p
		}
	}
	return ""
}

// Status 返回当前 GeoIP 与 ip2region 数据库的状态详情。
func (r *Resolver) Status() StatusView {
	r.mu.RLock()
	cityLoaded := r.db != nil
	cityPath := r.path
	cityTarget := firstNonEmpty(r.cityPaths)
	var cityType string
	var cityBuildTS int64
	if cityLoaded {
		md := r.db.Metadata()
		cityType = md.DatabaseType
		if md.BuildEpoch > 0 {
			cityBuildTS = int64(md.BuildEpoch) * 1000
		}
	}
	regionLoaded := r.region != nil
	var regionPath string
	if regionLoaded {
		regionPath = r.region.path
	}
	regionTarget := firstNonEmpty(r.regionPaths)
	cacheCount := len(r.cache)
	r.mu.RUnlock()

	cityStat := DBFileStatus{
		Loaded:  cityLoaded,
		Path:    cityPath,
		Target:  cityTarget,
		Type:    cityType,
		BuildTS: cityBuildTS,
	}
	if cityPath != "" {
		if fi, err := os.Stat(cityPath); err == nil {
			cityStat.Size = fi.Size()
			cityStat.ModTime = fi.ModTime().UnixMilli()
		}
	}

	regStat := DBFileStatus{
		Loaded: regionLoaded,
		Path:   regionPath,
		Target: regionTarget,
		Type:   "ip2region v4 (xdb)",
	}
	if regionPath != "" {
		if fi, err := os.Stat(regionPath); err == nil {
			regStat.Size = fi.Size()
			regStat.ModTime = fi.ModTime().UnixMilli()
		}
	}

	r.updateMu.Lock()
	updating := r.updating
	lastTS := r.lastUpdateTS
	lastErr := r.lastError
	r.updateMu.Unlock()

	return StatusView{
		City:         cityStat,
		Region:       regStat,
		CacheEntries: cacheCount,
		Updating:     updating,
		LastUpdateTS: lastTS,
		LastError:    lastErr,
	}
}

// Reload 从磁盘重新加载 GeoIP 城市库与 ip2region 库，并清空内存归属地缓存。
func (r *Resolver) Reload() error {
	r.mu.RLock()
	cPaths := append([]string(nil), r.cityPaths...)
	rPaths := append([]string(nil), r.regionPaths...)
	r.mu.RUnlock()

	newDB, newPath := openCityDB(cPaths)
	if newDB == nil {
		return errors.New("未找到可用的 GeoIP 城市库文件")
	}
	newReg := openRegion(rPaths...)

	r.mu.Lock()
	oldDB := r.db
	r.db = newDB
	r.path = newPath
	r.region = newReg
	r.cache = map[netip.Addr]Place{}
	r.version++
	r.mu.Unlock()

	if oldDB != nil {
		_ = oldDB.Close()
	}

	r.updateMu.Lock()
	r.lastUpdateTS = time.Now().UnixMilli()
	r.lastError = ""
	r.updateMu.Unlock()
	return nil
}

// UpdateOnline 在线下载最新的 dbip-city-lite 与 ip2region 到持久化目录并热重载。
func (r *Resolver) UpdateOnline(ctx context.Context) error {
	r.updateMu.Lock()
	if r.updating {
		r.updateMu.Unlock()
		return errors.New("GeoIP 库正在更新中，请稍候")
	}
	r.updating = true
	r.lastError = ""
	r.updateMu.Unlock()

	var retErr error
	defer func() {
		r.updateMu.Lock()
		r.updating = false
		if retErr != nil {
			r.lastError = retErr.Error()
		} else {
			r.lastUpdateTS = time.Now().UnixMilli()
			r.lastError = ""
		}
		r.updateMu.Unlock()
	}()

	r.mu.RLock()
	cityTarget := firstNonEmpty(r.cityPaths)
	regionTarget := firstNonEmpty(r.regionPaths)
	r.mu.RUnlock()

	if cityTarget == "" {
		retErr = errors.New("未配置 GeoIP 城市库存储路径")
		return retErr
	}

	client := &http.Client{Timeout: 3 * time.Minute}
	if err := downloadDBIPCity(ctx, client, cityTarget); err != nil {
		retErr = fmt.Errorf("下载全球城市库失败: %w", err)
		return retErr
	}
	if regionTarget != "" {
		if err := downloadIP2Region(ctx, client, regionTarget); err != nil {
			slog.Warn("在线更新 ip2region 失败，保留现有国内库", "error", err)
		}
	}
	if err := r.Reload(); err != nil {
		retErr = err
		return retErr
	}
	slog.Info("GeoIP 数据库在线更新并热重载完成", "city", cityTarget, "region", regionTarget)
	return nil
}

func downloadDBIPCity(ctx context.Context, client *http.Client, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	now := time.Now().UTC()
	months := []string{
		now.Format("2006-01"),
		now.AddDate(0, -1, 0).Format("2006-01"),
	}
	tmpPath := target + ".tmp"
	var lastErr error
	for _, m := range months {
		url := fmt.Sprintf("https://download.db-ip.com/free/dbip-city-lite-%s.mmdb.gz", m)
		if err := fetchAndGunzip(ctx, client, url, tmpPath); err != nil {
			lastErr = err
			continue
		}
		// 校验下载的 mmdb 是否可正常打开
		db, err := geoip2.Open(tmpPath)
		if err != nil {
			_ = os.Remove(tmpPath)
			lastErr = err
			continue
		}
		_ = db.Close()
		if err := os.Rename(tmpPath, target); err != nil {
			_ = os.Remove(tmpPath)
			return err
		}
		return nil
	}
	return lastErr
}

func fetchAndGunzip(ctx context.Context, client *http.Client, url, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d (%s)", resp.StatusCode, url)
	}
	zr, err := gzip.NewReader(resp.Body)
	if err != nil {
		return err
	}
	defer zr.Close()

	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(f, io.LimitReader(zr, 256*1024*1024))
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(dst)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(dst)
		return closeErr
	}
	if n < 1024*1024 {
		_ = os.Remove(dst)
		return errors.New("解压后文件过小，可能不完整")
	}
	return nil
}

func downloadIP2Region(ctx context.Context, client *http.Client, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	urls := []string{
		"https://gitee.com/lionsoul/ip2region/raw/master/data/ip2region_v4.xdb",
		"https://raw.githubusercontent.com/lionsoul2014/ip2region/master/data/ip2region_v4.xdb",
	}
	tmpPath := target + ".tmp"
	var lastErr error
	for _, url := range urls {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			lastErr = err
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d (%s)", resp.StatusCode, url)
			continue
		}
		buf, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024*1024))
		resp.Body.Close()
		if err != nil || len(buf) < 1024*1024 {
			lastErr = errors.New("下载 ip2region 文件不完整")
			continue
		}
		if hdr, err := xdb.LoadHeaderFromBuff(buf); err != nil {
			lastErr = err
			continue
		} else if _, err := xdb.VersionFromHeader(hdr); err != nil {
			lastErr = err
			continue
		}
		if err := os.WriteFile(tmpPath, buf, 0o644); err != nil {
			return err
		}
		if err := os.Rename(tmpPath, target); err != nil {
			_ = os.Remove(tmpPath)
			return err
		}
		return nil
	}
	return lastErr
}

// RunAutoUpdate 后台定期检查 GeoIP 库是否跨月过期（每 12 小时检查一次），过期时自动拉取热重载。
func (r *Resolver) RunAutoUpdate(ctx context.Context) {
	check := func() {
		st := r.Status()
		if st.Updating {
			return
		}
		now := time.Now()
		needUpdate := !st.City.Loaded
		if !needUpdate && now.Day() >= 3 {
			// dbip-city-lite 每月初发布；若当前库构建时间距今超过 35 天，自动尝试拉取当月新库
			refTS := st.City.BuildTS
			if refTS == 0 {
				refTS = st.City.ModTime
			}
			if refTS > 0 && now.Sub(time.UnixMilli(refTS)) > 35*24*time.Hour {
				needUpdate = true
			}
		}
		if !needUpdate {
			return
		}
		slog.Info("检测到 GeoIP 库缺失或已跨月过期，开始后台自动更新")
		uCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		if err := r.UpdateOnline(uCtx); err != nil {
			slog.Warn("后台自动更新 GeoIP 库失败", "error", err)
		}
	}

	// 启动 3 分钟后做首次检查，之后每 12 小时巡检一次
	initTimer := time.NewTimer(3 * time.Minute)
	defer initTimer.Stop()
	ticker := time.NewTicker(12 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-initTimer.C:
			check()
		case <-ticker.C:
			check()
		}
	}
}
