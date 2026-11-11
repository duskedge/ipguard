package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// GuardLogList 返回实际发生的域名拦截；不通过当前规则推断普通 403 的原因。
func (h *Handler) GuardLogList(c *gin.Context) {
	if h.GuardLogs == nil {
		fail(c, http.StatusServiceUnavailable, "域名拦截日志不可用")
		return
	}
	now := time.Now()
	if h.Engine != nil {
		since, _ := h.Engine.Window(c.Query("range"))
		h.guardLogResponse(c, since)
		return
	}
	since := now.Add(-24 * time.Hour)
	switch c.Query("range") {
	case "1h":
		since = now.Add(-time.Hour)
	case "7d":
		since = now.Add(-7 * 24 * time.Hour)
	case "today":
		y, m, d := now.Date()
		since = time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	}
	h.guardLogResponse(c, since.UnixMilli())
}

func (h *Handler) guardLogResponse(c *gin.Context, since int64) {
	page, size := intQ(c, "page", 1), intQ(c, "size", 20)
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 20
	}
	rows, total, err := h.GuardLogs.List(c.Request.Context(), since, c.Query("ip"), c.Query("host"), c.Query("rule"), c.Query("path"), page, size)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取域名拦截日志失败")
		return
	}
	pages := (total + int64(size) - 1) / int64(size)
	if pages < 1 {
		pages = 1
	}
	c.JSON(http.StatusOK, gin.H{"rows": rows, "total": total, "page": page, "pages": pages, "omitted": h.GuardLogs.Omitted()})
}
