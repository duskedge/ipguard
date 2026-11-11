package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/duskedge/ipguard/internal/hostacl"
	"github.com/duskedge/ipguard/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const guardMaxRows = 20000
const guardKeysPerMinute = 1000

// GuardRepo 有界异步采样：按分钟合并同类拦截，批量落盘，并限制新增样本速率。
// 队列溢出、超出采样速率、写入失败都累计到 Omitted，不影响鉴权结果。
type GuardRepo struct {
	db      *gorm.DB
	queue   chan model.GuardLog
	done    chan struct{}
	once    sync.Once
	omitted atomic.Uint64
}

func NewGuardRepo(db *gorm.DB) *GuardRepo {
	r := &GuardRepo{db: db, queue: make(chan model.GuardLog, 1024), done: make(chan struct{})}
	go r.run()
	return r
}
func (r *GuardRepo) Record(row model.GuardLog) {
	select {
	case r.queue <- row:
	default:
		r.omitted.Add(1)
	}
}
func (r *GuardRepo) Omitted() uint64 { return r.omitted.Load() }

// Close 必须在 HTTP 服务停止后、数据库关闭前调用。
func (r *GuardRepo) Close() { r.once.Do(func() { close(r.queue) }); <-r.done }

func guardKey(row model.GuardLog) string {
	// 全部判定快照参与键；规则发生变化或拒绝原因改变时不能合并。
	row.TS /= 60000
	row.ID, row.LastTS, row.Hits = 0, 0, 0
	row.EventKey = ""
	b, _ := json.Marshal(row)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func (r *GuardRepo) run() {
	defer close(r.done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	if err := r.prune(r.db); err != nil {
		slog.Error("清理域名拦截记录失败", "error", err)
	}
	pending := make(map[string]model.GuardLog)
	seen := make(map[string]bool)
	minute := time.Now().Unix() / 60
	var reported uint64
	flush := func() {
		if len(pending) > 0 {
			rows := make([]model.GuardLog, 0, len(pending))
			var hits uint64
			for _, row := range pending {
				rows = append(rows, row)
				hits += uint64(row.Hits)
			}
			if err := r.writeBatch(rows); err != nil {
				r.omitted.Add(hits)
				slog.Error("保存域名拦截记录失败", "error", err)
			}
			clear(pending)
		}
		if n := r.omitted.Load(); n > reported {
			slog.Warn("域名拦截日志已省略部分请求，拦截仍正常执行", "omitted", n-reported, "total_omitted", n)
			reported = n
		}
	}
	for {
		select {
		case row, ok := <-r.queue:
			if !ok {
				flush()
				return
			}
			if m := time.Now().Unix() / 60; m != minute {
				clear(seen)
				minute = m
			}
			key := guardKey(row)
			if !seen[key] {
				if len(seen) >= guardKeysPerMinute {
					r.omitted.Add(1)
					continue
				}
				seen[key] = true
			}
			if old, ok := pending[key]; ok {
				old.Hits++
				if row.TS < old.TS {
					old.TS = row.TS
				}
				if row.TS > old.LastTS {
					old.LastTS = row.TS
				}
				pending[key] = old
			} else {
				row.EventKey = key
				row.LastTS = row.TS
				row.Hits = 1
				pending[key] = row
			}
			if len(pending) >= 128 {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}
func (r *GuardRepo) writeBatch(rows []model.GuardLog) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "event_key"}}, DoUpdates: clause.Assignments(map[string]interface{}{
			"hits":    gorm.Expr("guard_logs.hits + excluded.hits"),
			"ts":      gorm.Expr("MIN(guard_logs.ts, excluded.ts)"),
			"last_ts": gorm.Expr("MAX(guard_logs.last_ts, excluded.last_ts)"),
		})}).Create(&rows).Error
		if err != nil {
			return err
		}
		// 同一事务内执行清理，提交后条数始终不超过上限。
		return r.prune(tx)
	})
}
func (r *GuardRepo) prune(tx *gorm.DB) error {
	return tx.Exec(`DELETE FROM guard_logs WHERE ts < ? OR id <= COALESCE((SELECT id FROM guard_logs ORDER BY id DESC LIMIT 1 OFFSET ?), 0)`, time.Now().Add(-7*24*time.Hour).UnixMilli(), guardMaxRows).Error
}
func (r *GuardRepo) List(ctx context.Context, since int64, ip, host, rule, path string, page, size int) ([]model.GuardLog, int64, error) {
	base := func() *gorm.DB {
		tx := r.db.WithContext(ctx).Model(&model.GuardLog{}).Where("last_ts >= ?", since)
		if ip != "" {
			tx = tx.Where("ip = ?", ip)
		}
		if host != "" {
			tx = tx.Where("host = ?", host)
		}
		if rule != "" {
			parts := strings.FieldsFunc(rule, func(r rune) bool { return r == ',' || r == '，' || r == ';' || r == '；' })
			rules := make([]string, 0, len(parts))
			for _, p := range parts {
				if p = strings.TrimSpace(p); p != "" {
					rules = append(rules, p)
				}
			}
			if len(rules) == 1 {
				tx = tx.Where("rule_host = ?", rules[0])
			} else if len(rules) > 1 {
				tx = tx.Where("rule_host IN ?", rules)
			}
		}
		if path != "" {
			tx = tx.Where("instr(path, ?) > 0", path)
		}
		return tx
	}
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 20
	}
	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := []model.GuardLog{}
	err := base().Order("last_ts DESC, id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error
	return rows, total, err
}

// HistoricalRuleTotals 只供首次升级引导计数，之后累计值不依赖采样明细。
func (r *GuardRepo) HistoricalRuleTotals() ([]hostacl.RuleTotal, error) {
	var rows []struct {
		RuleHost string
		Count    uint64
		LastAt   int64
		LastIP   string
	}
	err := r.db.Raw(`SELECT rule_host, SUM(hits) AS count, MAX(last_ts) AS last_at,
 (SELECT ip FROM guard_logs b WHERE b.rule_host = a.rule_host ORDER BY b.last_ts DESC, b.id DESC LIMIT 1) AS last_ip
 FROM guard_logs a GROUP BY rule_host`).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]hostacl.RuleTotal, 0, len(rows))
	for _, row := range rows {
		out = append(out, hostacl.RuleTotal{Host: row.RuleHost, Count: row.Count, LastAt: row.LastAt, LastIP: row.LastIP})
	}
	return out, nil
}
