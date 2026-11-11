// Package webui 把构建好的前端产物嵌进二进制，让面板以单容器形态部署。
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// dist 是 web/ 构建产物的落点。仓库里只存一个占位 index.html——真正的产物由
// 构建流程（make web-embed / deploy/Dockerfile）覆盖进来。
//
// 为什么要留占位文件：go:embed 的模式必须至少匹配到一个文件，否则整个包编译不过。
// 没有它，任何一次干净检出上的 `go build ./...`（CI 的第一道校验）都会直接失败。
// 用 all: 前缀是因为 Vite 产物里有 .vite/ 这类点开头的目录，默认模式会跳过它们。
//
//go:embed all:dist
var dist embed.FS

// Built 报告嵌进来的是不是真正的前端产物。占位文件里带了这个标记。
func Built() bool {
	data, err := dist.ReadFile("dist/index.html")
	if err != nil {
		return false
	}
	return !strings.Contains(string(data), placeholderMarker)
}

const placeholderMarker = "ipguard-webui-placeholder"

// FS 返回以 dist 为根的文件系统。
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		// 目录是编译期嵌入的，取子目录不可能失败；真失败了说明构建产物结构不对，
		// 让它在启动时就炸，而不是运行时给用户一堆 404。
		panic("webui: 嵌入产物结构异常: " + err.Error())
	}
	return sub
}

// Handler 返回 SPA 处理器：命中静态文件就直接发，其余一律回 index.html
// 交给前端路由（前端用 hash 路由，但任何未知路径都回 index.html，刷新时必须能出页面）。
func Handler() http.Handler {
	root := FS()
	fileServer := http.FileServer(http.FS(root))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" || name == "." {
			serveIndex(w, r, root)
			return
		}
		f, err := root.Open(name)
		if err != nil {
			// 带 hash 的产物不存在时必须 404，不能回 index.html：
			// 发版后旧页面去拉旧 chunk，拿到 HTML 会静默失败，404 才能让前端感知并刷新。
			if strings.HasPrefix(name, "assets/") {
				http.NotFound(w, r)
				return
			}
			serveIndex(w, r, root)
			return
		}
		defer f.Close()
		if st, err := f.Stat(); err == nil && st.IsDir() {
			serveIndex(w, r, root)
			return
		}
		// 带 hash 的产物（assets/xxx.a1b2c3.js）可以长缓存；index.html 不行，
		// 否则前端发版后用户会一直拿到旧的入口页。
		if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else if strings.HasSuffix(name, ".webmanifest") {
			w.Header().Set("Content-Type", "application/manifest+json; charset=utf-8")
		}
		fileServer.ServeHTTP(w, r)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, root fs.FS) {
	data, err := fs.ReadFile(root, "index.html")
	if err != nil {
		http.Error(w, "前端产物缺失", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
