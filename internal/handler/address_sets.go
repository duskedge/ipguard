package handler

import (
	"github.com/duskedge/ipguard/internal/hostacl"
	"github.com/gin-gonic/gin"
	"net/http"
)

func (h *Handler) AddressSetSettings(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"sets": h.HostACL.AddressSets()})
}
func (h *Handler) SaveAddressSetSettings(c *gin.Context) {
	if h.HostACL == nil {
		fail(c, http.StatusServiceUnavailable, "域名访问控制未初始化")
		return
	}
	var in struct {
		Sets []hostacl.AddressSet `json:"sets"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024*1024)
	if c.ShouldBindJSON(&in) != nil {
		fail(c, http.StatusBadRequest, "地址集配置格式错误或内容过大")
		return
	}
	sets, err := h.HostACL.SaveAddressSets(in.Sets)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"sets": sets})
}

// AddAddressSetIP POST /api/settings/address-sets/:id/ips：仅追加单个地址。
func (h *Handler) AddAddressSetIP(c *gin.Context) {
	if h.HostACL == nil {
		fail(c, http.StatusServiceUnavailable, "域名访问控制未初始化")
		return
	}
	var in struct {
		IP string `json:"ip"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
	if c.ShouldBindJSON(&in) != nil {
		fail(c, http.StatusBadRequest, "请填写有效 IP")
		return
	}
	added, err := h.HostACL.AddAddressSetIP(c.Param("id"), in.IP)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"added": added})
}
