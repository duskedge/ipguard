// Package router 组装路由。
package router

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/duskedge/ipguard/internal/handler"
	"github.com/duskedge/ipguard/internal/middleware"
	"github.com/duskedge/ipguard/internal/webui"
)

// New 创建 gin 引擎。
func New(h *handler.Handler, debug bool) *gin.Engine {
	if !debug {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery(), middleware.Security())
	// 外网访问会经过 npm-guard（内网地址）反代，信任内网段的 X-Forwarded-For，
	// 审计和登录限流才能拿到真实客户端 IP。
	_ = r.SetTrustedProxies([]string{"127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "::1/128", "fc00::/7"})
	if debug {
		r.Use(gin.Logger())
	}

	r.GET("/healthz", h.Healthz)

	api := r.Group("/api", middleware.NoStore())
	api.POST("/auth/login", h.Login)
	api.GET("/guard/check", h.GuardCheck)
	api.GET("/guard/rules", h.GuardRoutingRules)

	authed := api.Group("", middleware.Auth(h.Auth))
	authed.GET("/meta", h.Meta)
	authed.GET("/overview", h.Overview)
	authed.GET("/logs", h.Logs)
	authed.GET("/logs/filters", h.LogFilters)
	authed.GET("/logs/guard", h.GuardLogList)
	authed.GET("/hosts", h.Hosts)
	authed.GET("/ips", h.IPs)
	authed.GET("/ips/:ip", h.IPDetail)
	authed.GET("/geo", h.Geo)
	authed.GET("/geo/lookup", h.GeoLookup)
	authed.GET("/bans", h.Bans)
	authed.POST("/bans", h.Ban)
	authed.DELETE("/bans", h.Unban)
	authed.GET("/audit", h.AuditList)
	authed.GET("/settings/notifications", h.NotificationSettings)
	authed.PUT("/settings/notifications", h.SaveNotificationSettings)
	authed.POST("/settings/notifications/test", h.TestNotification)
	authed.GET("/settings/autoban", h.AutoBanSettings)
	authed.PUT("/settings/autoban", h.SaveAutoBanSettings)
	authed.GET("/settings/ip-tags", h.IPTagSettings)
	authed.PUT("/settings/ip-tags", h.SaveIPTagSettings)
	authed.GET("/settings/address-sets", h.AddressSetSettings)
	authed.PUT("/settings/address-sets", h.SaveAddressSetSettings)
	authed.POST("/settings/address-sets/:id/ips", h.AddAddressSetIP)
	authed.GET("/settings/host-rules", h.HostRuleSettings)
	authed.PUT("/settings/host-rules", h.SaveHostRuleSettings)
	authed.GET("/settings/geoip", h.GeoIPSettings)
	authed.POST("/settings/geoip/update", h.UpdateGeoIP)

	ui := webui.Handler()
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "接口不存在"})
			return
		}
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Status(http.StatusMethodNotAllowed)
			return
		}
		ui.ServeHTTP(c.Writer, c.Request)
	})
	return r
}
