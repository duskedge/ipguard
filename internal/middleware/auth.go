// Package middleware 提供 gin 中间件。
package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/duskedge/ipguard/internal/service"
)

// UserKey 是 gin.Context 里保存当前用户名的键。
const UserKey = "user"

// Auth 校验 Authorization: Bearer <token>。
func Auth(a *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		tok, ok := strings.CutPrefix(h, "Bearer ")
		if !ok || tok == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
			return
		}
		user, err := a.Verify(tok)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}
		c.Set(UserKey, user)
		c.Next()
	}
}

// NoStore 禁止缓存 API 响应（含封禁数据，不该留在浏览器/代理缓存里）。
func NoStore() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Next()
	}
}

// Security 设置基础安全响应头。
func Security() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "same-origin")
		c.Next()
	}
}
