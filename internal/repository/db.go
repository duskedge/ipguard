// Package repository 负责 SQLite 持久化（目前只有审计日志）。
package repository

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/duskedge/ipguard/migrations"
)

// Open 打开（必要时创建）SQLite 库并执行迁移。迁移失败即返回错误、拒绝启动。
func Open(dataDir string, debug bool) (*gorm.DB, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建数据目录: %w", err)
	}
	path := filepath.Join(dataDir, "ipguard.db")
	lv := logger.Warn
	if debug {
		lv = logger.Info
	}
	// WAL + busy_timeout：审计写入与列表读取并发时不报 database is locked
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(lv)})
	if err != nil {
		return nil, fmt.Errorf("打开 SQLite: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1) // SQLite 单写者，串行化最省心
	if err := migrate(db, migrations.SQLite); err != nil {
		return nil, err
	}
	return db, nil
}

// migrate 是一个极简的顺序迁移器：只执行 up，按编号递增，每个文件一个事务。
// 不引入 golang-migrate：只有一张表，带上它反而要多一个 CGO-free 的驱动适配层。
func migrate(db *gorm.DB, fsys fs.FS) error {
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at DATETIME NOT NULL)`).Error; err != nil {
		return fmt.Errorf("初始化迁移表: %w", err)
	}
	var current int
	if err := db.Raw(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current).Error; err != nil {
		return err
	}
	files, err := fs.Glob(fsys, "sqlite/*.up.sql")
	if err != nil {
		return err
	}
	type mig struct {
		ver  int
		path string
	}
	var list []mig
	for _, f := range files {
		base := filepath.Base(f)
		n, err := strconv.Atoi(strings.SplitN(base, "_", 2)[0])
		if err != nil {
			return fmt.Errorf("迁移文件名不合法: %s", base)
		}
		list = append(list, mig{n, f})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ver < list[j].ver })
	for _, m := range list {
		if m.ver <= current {
			continue
		}
		sqlText, err := fs.ReadFile(fsys, m.path)
		if err != nil {
			return err
		}
		err = db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec(string(sqlText)).Error; err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, m.ver, time.Now().UTC()).Error
		})
		if err != nil {
			return fmt.Errorf("执行迁移 %s: %w", m.path, err)
		}
		slog.Info("已执行迁移", "version", m.ver, "file", filepath.Base(m.path))
	}
	return nil
}
