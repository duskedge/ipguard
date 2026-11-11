package handler

import (
	"context"
	"github.com/duskedge/ipguard/internal/geoip"
	"github.com/duskedge/ipguard/internal/hostacl"
	"github.com/duskedge/ipguard/internal/repository"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestGuardCheckRecordsOnlyActualDenials(t *testing.T) {
	dir := t.TempDir()
	db, err := repository.Open(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { d, _ := db.DB(); d.Close() }()
	repo := repository.NewGuardRepo(db)
	defer repo.Close()
	store, err := hostacl.New(dir, geoip.Open(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Save([]hostacl.Rule{{Host: "hub.example.com", Enabled: true, Mode: hostacl.ModeAllowlist, AllowIPs: []string{"8.8.8.8"}}}); err != nil {
		t.Fatal(err)
	}
	h := &Handler{HostACL: store, GuardLogs: repo}
	for _, tc := range []struct {
		ip, remote string
		status     int
	}{{"9.9.9.9", "192.168.1.45:1234", 403}, {"8.8.8.8", "192.168.1.45:1234", 200}, {"invalid", "192.168.1.45:1234", 200}, {"9.9.9.9", "8.8.8.8:1234", 403}} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/api/guard/check", nil)
		c.Request.RemoteAddr = tc.remote
		c.Request.Header.Set("X-Client-IP", tc.ip)
		c.Request.Header.Set("X-Original-Host", "hub.example.com")
		c.Request.Header.Set("X-Original-URI", "/private?token=secret")
		c.Request.Header.Set("X-Original-Method", "GET")
		c.Request.Header.Set("X-Original-Request", "POST /private?token=secret HTTP/1.1")
		h.GuardCheck(c)
		c.Writer.WriteHeaderNow()
		if w.Code != tc.status {
			t.Fatalf("%s: status %d", tc.ip, w.Code)
		}
	}
	// 修改配置后，已有事件仍保留原来的模式与允许列表。
	if _, err = store.Save(nil); err != nil {
		t.Fatal(err)
	}
	if ok, _ := store.Check("hub.example.com", netip.MustParseAddr("9.9.9.9")); !ok {
		t.Fatal("rule should be removed")
	}
	repo.Close()
	rows, total, err := repo.List(context.Background(), time.Now().Add(-time.Hour).UnixMilli(), "", "", "", "", 1, 20)
	if err != nil || total != 1 {
		t.Fatalf("count %d err %v", total, err)
	}
	row := rows[0]
	if row.Path != "/private" || strings.Contains(row.Path, "secret") || row.Method != "POST" || row.AllowIPs != "8.8.8.8" || row.Mode != hostacl.ModeAllowlist {
		t.Fatalf("incorrect snapshot: %+v", row)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/logs/guard?size=999&page=-1&rule=hub.example.com", nil)
	h.GuardLogList(c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"page":1`) {
		t.Fatalf("list: %s", w.Body.String())
	}
}

func TestGuardRoutingRulesPrivateAndMinimal(t *testing.T) {
	store, err := hostacl.New(t.TempDir(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Save([]hostacl.Rule{
		{Host: "*.example.com", Enabled: true, Mode: hostacl.ModeAllowlist, AllowIPs: []string{"8.8.8.8"}, Note: "不应泄露"},
		{Host: "public.example.com", Enabled: false, Mode: hostacl.ModeDomestic},
	}); err != nil {
		t.Fatal(err)
	}
	h := &Handler{HostACL: store}
	for _, tc := range []struct {
		remote string
		status int
	}{{"192.168.1.45:1234", 200}, {"[::1]:1234", 200}, {"8.8.8.8:1234", 403}, {"invalid", 403}} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/api/guard/rules", nil)
		c.Request.RemoteAddr = tc.remote
		c.Request.Header.Set("X-Real-IP", "192.168.1.45")
		h.GuardRoutingRules(c)
		c.Writer.WriteHeaderNow()
		if w.Code != tc.status {
			t.Fatalf("%s: status %d", tc.remote, w.Code)
		}
		if tc.status == 200 {
			body := w.Body.String()
			if !strings.Contains(body, `"schema":1`) || !strings.Contains(body, `"enabled":false`) || strings.Contains(body, "8.8.8.8") || strings.Contains(body, "不应泄露") {
				t.Fatalf("snapshot: %s", body)
			}
		}
	}
}
