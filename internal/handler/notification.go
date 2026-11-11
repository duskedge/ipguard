package handler

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/duskedge/ipguard/internal/autoban"
	"github.com/duskedge/ipguard/internal/geoip"
	"github.com/duskedge/ipguard/internal/hostacl"
	"github.com/duskedge/ipguard/internal/iptag"
	"github.com/duskedge/ipguard/internal/model"
	"github.com/duskedge/ipguard/internal/notification"
	"github.com/gin-gonic/gin"
)

func (h *Handler) NotificationSettings(c *gin.Context) {
	c.JSON(http.StatusOK, h.Notifications.View())
}
func (h *Handler) SaveNotificationSettings(c *gin.Context) {
	var in notification.Input
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024)
	if c.ShouldBindJSON(&in) != nil {
		fail(c, http.StatusBadRequest, "通知配置格式错误或内容过大")
		return
	}
	view, err := h.Notifications.Save(in)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	c.JSON(http.StatusOK, view)
}
func (h *Handler) TestNotification(c *gin.Context) {
	var in notification.Input
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024)
	if c.ShouldBindJSON(&in) != nil {
		fail(c, http.StatusBadRequest, "通知配置格式错误或内容过大")
		return
	}
	if err := h.Notifications.Test(c.Request.Context(), in); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "测试通知已发送"})
}

// AutoBanSettings GET /api/settings/autoban
func (h *Handler) AutoBanSettings(c *gin.Context) {
	c.JSON(http.StatusOK, h.AutoBan.View())
}

// SaveAutoBanSettings PUT /api/settings/autoban
func (h *Handler) SaveAutoBanSettings(c *gin.Context) {
	if h.AutoBan == nil {
		fail(c, http.StatusServiceUnavailable, "自动封禁服务未初始化")
		return
	}
	var in autoban.Config
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024)
	if c.ShouldBindJSON(&in) != nil {
		fail(c, http.StatusBadRequest, "自动封禁配置格式错误")
		return
	}
	view, err := h.AutoBan.Save(in)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	c.JSON(http.StatusOK, view)
}

type ipTagsReq struct {
	Items []iptag.Item `json:"items"`
}

func (h *Handler) IPTagSettings(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"items": h.IPTags.List()})
}

func (h *Handler) SaveIPTagSettings(c *gin.Context) {
	var in ipTagsReq
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128*1024)
	if c.ShouldBindJSON(&in) != nil {
		fail(c, http.StatusBadRequest, "IP 标识配置格式错误或内容过大")
		return
	}
	items, err := h.IPTags.Save(in.Items)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

type hostRulesReq struct {
	Rules []hostacl.Rule `json:"rules"`
}

// HostRuleSettings GET /api/settings/host-rules
func (h *Handler) HostRuleSettings(c *gin.Context) {
	var rules []hostacl.RuleView
	if h.HostACL != nil {
		rules = h.HostACL.List()
	} else {
		rules = []hostacl.RuleView{}
	}
	c.JSON(http.StatusOK, gin.H{
		"rules": rules,
		"hosts": h.Engine.Hosts(),
		"tags":  h.Engine.Tags(),
	})
}

// SaveHostRuleSettings PUT /api/settings/host-rules
func (h *Handler) SaveHostRuleSettings(c *gin.Context) {
	if h.HostACL == nil {
		fail(c, http.StatusServiceUnavailable, "域名访问控制未初始化")
		return
	}
	var in hostRulesReq
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128*1024)
	if c.ShouldBindJSON(&in) != nil {
		fail(c, http.StatusBadRequest, "域名访问控制配置格式错误或内容过大")
		return
	}
	rules, err := h.HostACL.Save(in.Rules)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"rules": rules})
}

// GuardCheck GET /api/guard/check：供 npm-guard (Nginx auth_request) 调用的超轻量鉴权接口。
// 命中放行返回 200，命中拦截返回 403。
func (h *Handler) GuardCheck(c *gin.Context) {
	if remoteHost, _, err := net.SplitHostPort(c.Request.RemoteAddr); err == nil {
		if remoteIP, err := netip.ParseAddr(remoteHost); err == nil && !geoip.IsPrivate(remoteIP) {
			c.Status(http.StatusForbidden)
			return
		}
	}
	if h.HostACL == nil {
		c.Status(http.StatusOK)
		return
	}
	host := strings.TrimSpace(c.GetHeader("X-Original-Host"))
	if host == "" {
		host = strings.TrimSpace(c.Query("host"))
	}
	if host == "" {
		host = c.Request.Host
	}
	ipStr := strings.TrimSpace(c.GetHeader("X-Client-IP"))
	if ipStr == "" {
		ipStr = strings.TrimSpace(c.GetHeader("X-Real-IP"))
	}
	if ipStr == "" {
		ipStr = strings.TrimSpace(c.Query("ip"))
	}
	if ipStr == "" {
		ipStr = c.ClientIP()
	}
	addr, err := netip.ParseAddr(ipStr)
	if err != nil {
		// 无法解析客户端 IP 时默认放行，避免异常头导致服务不可用
		c.Status(http.StatusOK)
		return
	}
	decision := h.HostACL.Decide(host, addr)
	if decision.Allowed {
		c.Status(http.StatusOK)
		return
	} else {
		c.Header("X-IPGuard-Reason", "denied:"+decision.Mode)
		path := strings.SplitN(c.GetHeader("X-Original-URI"), "?", 2)[0]
		if h.GuardLogs != nil {
			// 限制不可信请求元数据长度；只保留路径，不存储可能包含凭据的查询串。
			method := c.GetHeader("X-Original-Method")
			if original := c.GetHeader("X-Original-Request"); original != "" {
				method, _, _ = strings.Cut(original, " ")
			}
			h.GuardLogs.Record(model.GuardLog{
				TS: time.Now().UnixMilli(), IP: addr.Unmap().String(), Host: clipGuardText(host, 253),
				Path: clipGuardText(path, 2048), Method: clipGuardText(method, 16), UA: clipGuardText(c.GetHeader("User-Agent"), 512),
				RuleHost: decision.RuleHost, Mode: decision.Mode, Reason: decision.Reason, Geo: decision.Geo, CC: decision.CC, Tag: decision.Tag,
				AllowIPs: clipGuardText(strings.Join(decision.AllowIPs, ", "), 2048), AllowTags: clipGuardText(strings.Join(decision.AllowTags, ", "), 1024),
			})
		}
		if h.AutoBan != nil {
			h.AutoBan.RecordGuardBlock(addr, clipGuardText(host, 253), clipGuardText(path, 512), decision.Geo)
		}
	}
	c.Status(http.StatusForbidden)
}

func clipGuardText(s string, limit int) string {
	r := []rune(s)
	if len(r) > limit {
		return string(r[:limit]) + "…"
	}
	return s
}

// GuardRoutingRules 是仅供内网 Nginx 后台同步的最小域名快照。
func (h *Handler) GuardRoutingRules(c *gin.Context) {
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		c.Status(http.StatusForbidden)
		return
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !geoip.IsPrivate(ip) {
		c.Status(http.StatusForbidden)
		return
	}
	if h.HostACL == nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	c.JSON(http.StatusOK, gin.H{"schema": 1, "rules": h.HostACL.RoutingRules()})
}

type geoipUpdateReq struct {
	Mode string `json:"mode"` // "download" | "reload"
}

// GeoIPSettings GET /api/settings/geoip
func (h *Handler) GeoIPSettings(c *gin.Context) {
	c.JSON(http.StatusOK, h.GeoDB.Status())
}

// UpdateGeoIP POST /api/settings/geoip/update
func (h *Handler) UpdateGeoIP(c *gin.Context) {
	var in geoipUpdateReq
	_ = c.ShouldBindJSON(&in)
	if in.Mode == "reload" {
		if err := h.GeoDB.Reload(); err != nil {
			fail(c, http.StatusBadRequest, err.Error())
			return
		}
		c.JSON(http.StatusOK, h.GeoDB.Status())
		return
	}
	if err := h.GeoDB.UpdateOnline(c.Request.Context()); err != nil {
		fail(c, http.StatusBadGateway, err.Error())
		return
	}
	c.JSON(http.StatusOK, h.GeoDB.Status())
}
