// Package handler 实现 HTTP 接口（契约见 docs/实现指南.md §3）。
package handler

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/duskedge/ipguard/internal/autoban"
	"github.com/duskedge/ipguard/internal/config"
	"github.com/duskedge/ipguard/internal/geoip"
	"github.com/duskedge/ipguard/internal/hostacl"
	"github.com/duskedge/ipguard/internal/iptag"
	"github.com/duskedge/ipguard/internal/logparser"
	"github.com/duskedge/ipguard/internal/middleware"
	"github.com/duskedge/ipguard/internal/notification"
	"github.com/duskedge/ipguard/internal/repository"
	"github.com/duskedge/ipguard/internal/service"
	"github.com/duskedge/ipguard/internal/stats"
)

// BuildInfo 是版本信息（ldflags 注入）。
type BuildInfo struct {
	Version   string `json:"version"`
	GitCommit string `json:"git_commit"`
	BuildTime string `json:"build_time"`
}

// Handler 汇总依赖。
type Handler struct {
	LogPreview    config.Logs
	AutoBan       *autoban.Service
	Notifications *notification.Service
	IPTags        *iptag.Store
	HostACL       *hostacl.Store
	Engine        *stats.Engine
	Store         *stats.Store
	BanSvc        *service.BanService
	Auth          *service.AuthService
	GuardLogs     *repository.GuardRepo
	Audit         *repository.AuditRepo
	GeoDB         *geoip.Resolver
	Tailer        *logparser.Tailer
	Build         BuildInfo
	Site          string
	Start         time.Time
}

func intQ(c *gin.Context, key string, def int) int {
	if v, err := strconv.Atoi(c.Query(key)); err == nil {
		return v
	}
	return def
}

func fail(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{"error": msg})
}

// ── 公共 ───────────────────────────────────────────────────────────────────

// Healthz 健康检查（无需登录，只给出概要，不含敏感信息）。
func (h *Handler) Healthz(c *gin.Context) {
	st := h.Tailer.Snapshot()
	c.JSON(http.StatusOK, gin.H{
		"status":   "ok",
		"version":  h.Build.Version,
		"commit":   h.Build.GitCommit,
		"build":    h.Build.BuildTime,
		"uptime_s": int(time.Since(h.Start).Seconds()),
		"logs":     gin.H{"files": st.Files, "entries": h.Store.Len(), "last_scan": st.LastScan, "error": st.LastError},
		"geoip":    h.GeoDB.Loaded(),
		"crowdsec": h.BanSvc.Status().Connected,
	})
}

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Login POST /api/auth/login
func (h *Handler) Login(c *gin.Context) {
	var in loginReq
	if err := c.ShouldBindJSON(&in); err != nil || in.Username == "" || in.Password == "" {
		fail(c, http.StatusBadRequest, "请填写用户名和密码")
		return
	}
	tok, exp, err := h.Auth.Login(in.Username, in.Password, c.ClientIP())
	switch {
	case errors.Is(err, service.ErrLocked):
		fail(c, http.StatusTooManyRequests, err.Error())
		return
	case errors.Is(err, service.ErrBadCredentials):
		// 拖一下，降低在线猜口令的速度
		time.Sleep(500 * time.Millisecond)
		fail(c, http.StatusUnauthorized, err.Error())
		return
	case err != nil:
		fail(c, http.StatusInternalServerError, "签发登录凭据失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": tok, "expires_at": exp.UnixMilli(), "username": in.Username})
}

// ── 需登录 ─────────────────────────────────────────────────────────────────

// Meta GET /api/meta：顶栏/侧栏需要的信息。
func (h *Handler) Meta(c *gin.Context) {
	user, _ := c.Get(middleware.UserKey)
	c.JSON(http.StatusOK, gin.H{
		"user":     user,
		"site":     h.Site,
		"build":    h.Build,
		"crowdsec": h.BanSvc.Status(),
		"geoip":    h.GeoDB.Info(),
		"logs":     gin.H{"dir": h.Tailer.Dir, "stats": h.Tailer.Snapshot(), "entries": h.Store.Len(), "hosts": len(h.Store.Hosts()), "preview": gin.H{"scheme": h.LogPreview.PreviewScheme, "port": h.LogPreview.PreviewPort}},
	})
}

// Overview GET /api/overview?trend=1h|24h|7d
func (h *Handler) Overview(c *gin.Context) {
	ov := h.Engine.Overview(c.Query("trend"))
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	c.JSON(http.StatusOK, gin.H{"overview": ov, "bans": h.BanSvc.Counts(ctx)})
}

// Logs GET /api/logs
func (h *Handler) Logs(c *gin.Context) {
	c.JSON(http.StatusOK, h.Engine.Logs(stats.LogQuery{
		Range: c.Query("range"), Q: c.Query("q"), Tag: c.Query("tag"), Host: c.Query("host"), Status: c.Query("status"),
		Path: c.Query("path"), Country: c.Query("country"), Sort: c.Query("sort"), Dir: c.Query("dir"),
		HideLAN: c.Query("hide_lan") == "1", Page: intQ(c, "page", 1), Size: intQ(c, "size", 12),
	}))
}

// LogFilters GET /api/logs/filters：域名、国家与自定义标识下拉选项。
func (h *Handler) LogFilters(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"hosts": h.Engine.Hosts(), "countries": h.Engine.Countries(), "tags": h.Engine.Tags()})
}

// Hosts GET /api/hosts
func (h *Handler) Hosts(c *gin.Context) {
	c.JSON(http.StatusOK, h.Engine.HostsStats(stats.HostQuery{
		Range: c.Query("range"), Q: c.Query("q"), Host: c.Query("host"),
		HideLAN: c.Query("hide_lan") == "1", Sort: c.Query("sort"), Dir: c.Query("dir"),
		Page: intQ(c, "page", 1), Size: intQ(c, "size", 10),
	}))
}

// IPs GET /api/ips
func (h *Handler) IPs(c *gin.Context) {
	c.JSON(http.StatusOK, h.Engine.IPs(stats.IPQuery{
		Range: c.Query("range"), Q: c.Query("q"), Tag: c.Query("tag"), Level: c.Query("level"), Sort: c.Query("sort"),
		HideLAN: c.Query("hide_lan") == "1",
		Dir:     c.Query("dir"), Page: intQ(c, "page", 1), Size: intQ(c, "size", 10),
	}))
}

// IPDetail GET /api/ips/:ip
func (h *Handler) IPDetail(c *gin.Context) {
	ip, err := netip.ParseAddr(c.Param("ip"))
	if err != nil {
		fail(c, http.StatusBadRequest, "IP 不合法")
		return
	}
	c.JSON(http.StatusOK, h.Engine.IPDetail(ip.Unmap()))
}

// GeoLookup GET /api/geo/lookup?ip= ：手动封禁弹窗里输入地址时提示归属地。
func (h *Handler) GeoLookup(c *gin.Context) {
	ip, err := netip.ParseAddr(c.Query("ip"))
	if err != nil {
		fail(c, http.StatusBadRequest, "IP 不合法")
		return
	}
	p := h.GeoDB.Lookup(ip)
	c.JSON(http.StatusOK, gin.H{"geo": p.Label(), "cc": p.CC, "banned": h.BanSvc.Banned(ip)})
}

// Geo GET /api/geo?range=
func (h *Handler) Geo(c *gin.Context) {
	c.JSON(http.StatusOK, h.Engine.Geo(c.Query("range")))
}

// Bans GET /api/bans
func (h *Handler) Bans(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	c.JSON(http.StatusOK, h.BanSvc.List(ctx, service.ListQuery{
		Source: c.Query("source"), Q: c.Query("q"), Country: c.Query("country"), Sort: c.Query("sort"),
		Dir: c.Query("dir"), Page: intQ(c, "page", 1), Size: intQ(c, "size", 10),
	}))
}

func actor(c *gin.Context) service.Actor {
	u, _ := c.Get(middleware.UserKey)
	s, _ := u.(string)
	return service.Actor{User: s, ClientIP: c.ClientIP()}
}

type banReq struct {
	Target   string `json:"target"`
	Duration string `json:"duration"`
	Reason   string `json:"reason"`
}

func banStatus(err error) int {
	switch {
	case errors.Is(err, service.ErrInvalidTarget), errors.Is(err, service.ErrPrivateTarget),
		errors.Is(err, service.ErrTooWide), errors.Is(err, service.ErrBadDuration):
		return http.StatusBadRequest
	case errors.Is(err, service.ErrAlreadyBanned):
		return http.StatusConflict
	case errors.Is(err, service.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, service.ErrNotConfigured):
		return http.StatusServiceUnavailable
	default:
		return http.StatusBadGateway
	}
}

// Ban POST /api/bans
func (h *Handler) Ban(c *gin.Context) {
	var in banReq
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, "请求格式错误")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	res, err := h.BanSvc.Ban(ctx, actor(c), service.BanInput{Target: in.Target, Duration: in.Duration, Reason: in.Reason})
	if err != nil {
		fail(c, banStatus(err), service.ErrorText(err))
		return
	}
	c.JSON(http.StatusOK, res)
}

type unbanReq struct {
	ID     int64  `json:"id"`
	Target string `json:"target"`
}

// Unban DELETE /api/bans  body: {id} 或 {target}
func (h *Handler) Unban(c *gin.Context) {
	var in unbanReq
	if err := c.ShouldBindJSON(&in); err != nil || (in.ID <= 0 && in.Target == "") {
		fail(c, http.StatusBadRequest, "请指定要解封的记录")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	done, err := h.BanSvc.Unban(ctx, actor(c), in.ID, in.Target)
	if err != nil {
		fail(c, banStatus(err), service.ErrorText(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"removed": done})
}

// Audit GET /api/audit?q=&page=&size=
func (h *Handler) AuditList(c *gin.Context) {
	rows, total, err := h.Audit.List(c.Query("q"), intQ(c, "page", 1), intQ(c, "size", 20))
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取审计日志失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"rows": rows, "total": total})
}
