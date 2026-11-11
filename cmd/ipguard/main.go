// Command ipguard 是 ipguard 的服务端入口：解析 npm-guard 访问日志、统计归属地与频次，
// 并通过 CrowdSec LAPI 封禁/解封 IP。
//
//	ipguard              运行 HTTP 服务
//	ipguard version      打印版本
//	ipguard hash-password <口令>   生成 bcrypt 口令哈希（填 auth.passwordHash）
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/duskedge/ipguard/internal/autoban"
	"github.com/duskedge/ipguard/internal/config"
	"github.com/duskedge/ipguard/internal/crowdsec"
	"github.com/duskedge/ipguard/internal/geoip"
	"github.com/duskedge/ipguard/internal/handler"
	"github.com/duskedge/ipguard/internal/hostacl"
	"github.com/duskedge/ipguard/internal/iptag"
	"github.com/duskedge/ipguard/internal/logparser"
	"github.com/duskedge/ipguard/internal/notification"
	"github.com/duskedge/ipguard/internal/npmconf"
	"github.com/duskedge/ipguard/internal/repository"
	"github.com/duskedge/ipguard/internal/router"
	"github.com/duskedge/ipguard/internal/service"
	"github.com/duskedge/ipguard/internal/stats"
)

var (
	version   = "dev"
	buildTime = "unknown"
	gitCommit = "unknown"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "-v", "--version":
			fmt.Printf("ipguard %s (build %s, commit %s)\n", version, buildTime, gitCommit)
			return
		case "hash-password":
			if len(os.Args) != 3 {
				fmt.Fprintln(os.Stderr, "用法: ipguard hash-password <口令>")
				os.Exit(2)
			}
			h, err := bcrypt.GenerateFromPassword([]byte(os.Args[2]), bcrypt.DefaultCost)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			fmt.Println(string(h))
			return
		}
	}
	os.Exit(run())
}

func run() int {
	cfg, err := config.Load(os.Getenv("IPGUARD_CONFIG"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "配置加载失败:", err)
		return 1
	}
	setupLogger(cfg.Log.Level)
	debug := cfg.Server.Mode == "debug"
	slog.Info("ipguard starting", "version", version, "commit", gitCommit, "mode", cfg.Server.Mode)

	db, err := repository.Open(cfg.Storage.DataDir, debug)
	if err != nil {
		slog.Error("数据库初始化失败", "error", err)
		return 1
	}
	auditRepo := repository.NewAuditRepo(db)

	secret, err := service.ResolveSecret(cfg.Auth.JWTSecret, cfg.Storage.DataDir)
	if err != nil {
		slog.Error("JWT 密钥初始化失败", "error", err)
		return 1
	}
	authSvc := service.NewAuthService(cfg.Auth.Username, cfg.Auth.Password, cfg.Auth.PasswordHash, secret, cfg.Auth.TokenTTL)

	geo := geoip.Open(cfg.GeoIP.CityDB, cfg.GeoIP.FallbackDB).WithRegion(cfg.GeoIP.RegionDB, cfg.GeoIP.RegionFallbackDB)
	defer geo.Close()

	var cs *crowdsec.Client
	if cfg.CrowdSecConfigured() {
		cs = crowdsec.New(cfg.CrowdSec.URL, cfg.CrowdSec.MachineID, cfg.CrowdSec.Password, cfg.CrowdSec.Timeout)
		slog.Info("CrowdSec LAPI", "url", cfg.CrowdSec.URL, "machine", cfg.CrowdSec.MachineID)
	} else {
		slog.Warn("未配置 CrowdSec machine 凭据，封禁功能不可用（crowdsec.machineID / crowdsec.password）")
	}
	bans := service.NewBanService(cs, geo, auditRepo, cfg.CrowdSec.CacheTTL, cfg.CrowdSec.Origin)

	store := stats.NewStore(cfg.Logs.Retention, cfg.Logs.MaxEntries)
	engine := stats.NewEngine(store, geo, bans, stats.Detect{
		SensitivePaths: cfg.Detect.SensitivePaths,
		ScanHits:       cfg.Detect.ScanHits,
		ScanErrRatio:   cfg.Detect.ScanErrRatio,
		WarnMinReq:     cfg.Detect.WarnMinReq,
		WarnErrRatio:   cfg.Detect.WarnErrRatio,
	}, cfg.Location())

	// 访问日志只记反代目标地址、不记端口：读 npm 的反代配置补全成“地址:端口”
	npmResolver := npmconf.New(cfg.Logs.NPMConfDir, npmconf.ParseAliases(cfg.Logs.BackendAliases))
	engine.SetBackendLabeler(npmResolver.Label)
	engine.SetConfiguredHosts(npmResolver.Hosts)

	ipTags, err := iptag.New(cfg.Storage.DataDir)
	if err != nil {
		slog.Error("IP 标识服务初始化失败", "error", err)
		return 1
	}
	engine.SetIPTags(ipTags)

	hostACL, err := hostacl.New(cfg.Storage.DataDir, geo, ipTags)
	if err != nil {
		slog.Error("域名访问控制服务初始化失败", "error", err)
		return 1
	}

	tailer := &logparser.Tailer{
		Dir:      cfg.Logs.Dir,
		Patterns: cfg.Logs.Patterns,
		Exclude:  cfg.Logs.Exclude,
		Interval: cfg.Logs.PollInterval,
		Since:    func() time.Time { return time.Now().Add(-cfg.Logs.Retention) },
		Emit:     store.Add,
	}

	notifications, err := notification.New(cfg.Storage.DataDir)
	if err != nil {
		slog.Error("通知服务初始化失败", "error", err)
		return 1
	}

	autoBan, err := autoban.New(cfg.Storage.DataDir, bans, ipTags, hostACL, notifications)
	if err != nil {
		slog.Error("自动封禁服务初始化失败", "error", err)
		return 1
	}
	// 敏感路径可在设置页修改；未自定义时使用配置文件 / 环境变量里的默认列表
	autoBan.BindSensitivePaths(cfg.Detect.SensitivePaths, engine.SetSensitivePaths)

	guardRepo := repository.NewGuardRepo(db)
	defer guardRepo.Close()
	if err := hostACL.BootstrapStats(guardRepo.HistoricalRuleTotals); err != nil {
		slog.Error("累计拦截统计初始化失败", "error", err)
		return 1
	}
	defer func() {
		if err := hostACL.FlushStats(); err != nil {
			slog.Error("保存累计拦截统计失败", "error", err)
		}
	}()
	h := &handler.Handler{
		LogPreview:    cfg.Logs,
		AutoBan:       autoBan,
		Notifications: notifications,
		IPTags:        ipTags,
		HostACL:       hostACL,
		GuardLogs:     guardRepo,
		Engine:        engine, Store: store, BanSvc: bans, Auth: authSvc, Audit: auditRepo, GeoDB: geo, Tailer: tailer,
		Build: handler.BuildInfo{Version: version, GitCommit: gitCommit, BuildTime: buildTime},
		Site:  cfg.Server.Site, Start: time.Now(),
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go hostACL.RunStats(ctx)
	go geo.RunAutoUpdate(ctx)
	go tailer.Run(ctx)
	go bans.Run(ctx)
	go notifications.Run(ctx, engine)
	go autoBan.Run(ctx, engine)
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				store.Prune()
			}
		}
	}()

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:           router.New(h, debug),
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      60 * time.Second,
	}
	go func() {
		slog.Info("http server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server failed", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", "error", err)
		return 1
	}
	if err := hostACL.FlushStats(); err != nil {
		slog.Error("停服保存累计拦截统计失败", "error", err)
		return 1
	}
	guardRepo.Close()
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
	slog.Info("bye")
	return 0
}

func setupLogger(level string) {
	var lv slog.Level
	switch level {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lv})))
}
