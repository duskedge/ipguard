package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPlaceholderIsNotMistakenForARealBuild(t *testing.T) {
	if Built() {
		t.Fatal("仓库里嵌的应该是占位页，Built() 必须为 false；" +
			"若这里失败，说明构建产物被误提交进了 internal/webui/dist")
	}
}

func TestHandlerServesIndexForFrontendRoutes(t *testing.T) {
	h := Handler()
	// 这些都是前端路由，刷新时必须回 index.html 而不是 404
	for _, p := range []string{"/", "/login", "/some/deep/route"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: 期望 200，实际 %d", p, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Fatalf("%s: 期望 HTML，实际 %q", p, ct)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Fatalf("%s: 入口页不能被缓存，实际 Cache-Control=%q", p, cc)
		}
	}
}

func TestHandlerRejectsPathTraversal(t *testing.T) {
	h := Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/../../etc/passwd", nil))
	// 逃逸不出去：要么回 index.html，绝不能吐出 dist 之外的内容
	if body := rec.Body.String(); strings.Contains(body, "root:") {
		t.Fatal("路径穿越读到了 dist 之外的文件")
	}
}

func TestMissingAssetIs404(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/old-chunk-abc123.js", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("缺失的产物应返回 404，实际 %d", rec.Code)
	}
}
