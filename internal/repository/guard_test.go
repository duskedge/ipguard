package repository

import (
	"context"
	"fmt"
	"github.com/duskedge/ipguard/internal/model"
	"testing"
	"time"
)

func TestGuardLogsPersistFilterAndExpire(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	r := NewGuardRepo(db)
	now := time.Now().UnixMilli()
	for _, row := range []model.GuardLog{
		{TS: now, IP: "8.8.8.8", Host: "hub.example.com", RuleHost: "*.example.com", Mode: "domestic", Reason: "历史原因", Path: "/a%"},
		{TS: now + 1, IP: "9.9.9.9", Host: "other.example.com", RuleHost: "other.example.com", Mode: "allowlist"},
		{TS: time.Now().Add(-8 * 24 * time.Hour).UnixMilli(), IP: "8.8.8.8", Host: "old.example.com"},
	} {
		r.Record(row)
	}
	r.Close()
	sqlDB, _ := db.DB()
	sqlDB.Close()
	db, err = Open(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { d, _ := db.DB(); d.Close() }()
	r = NewGuardRepo(db)
	r.Close() // 等待启动清理完成
	rows, total, err := r.List(context.Background(), 0, "8.8.8.8", "hub.example.com", "*.example.com", "%", 1, 20)
	if err != nil || total != 1 || len(rows) != 1 || rows[0].Reason != "历史原因" {
		t.Fatalf("snapshot/filter failed: %+v %d %v", rows, total, err)
	}
	rows, total, err = r.List(context.Background(), 0, "", "", "", "", 1, 1)
	if err != nil || total != 2 || len(rows) != 1 || rows[0].IP != "9.9.9.9" {
		t.Fatalf("retention/pagination failed: %+v %d %v", rows, total, err)
	}
}

func TestGuardLogsMergeAndRateLimit(t *testing.T) {
	db, err := Open(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { d, _ := db.DB(); d.Close() }()
	r := NewGuardRepo(db)
	defer r.Close()
	now := time.Now().Truncate(time.Minute).UnixMilli() + 1000
	row := model.GuardLog{TS: now, IP: "8.8.8.8", Host: "hub.example.com", Path: "/login", Mode: "domestic", Reason: "拒绝"}
	for i := 0; i < 500; i++ {
		r.Record(row)
	}
	// 模拟攻击者随机改路径；直接写队列以隔离测试采样速率，而非队列容量。
	for i := 0; i < guardKeysPerMinute+20; i++ {
		v := row
		v.Path = fmt.Sprintf("/random/%d", i)
		r.queue <- v
	}
	r.Close()
	rows, total, err := r.List(context.Background(), 0, "8.8.8.8", "hub.example.com", "", "/login", 1, 20)
	if err != nil || total != 1 || rows[0].Hits != 500 {
		t.Fatalf("merge failed: %+v %d %v", rows, total, err)
	}
	if r.Omitted() != 21 {
		t.Fatalf("rate limit expected 21, got %d", r.Omitted())
	}
	// 跨批次合并，同一条记录累加次数；规则快照变化时分开。
	row.EventKey = guardKey(row)
	row.Hits = 3
	row.LastTS = now + 2000
	if err := r.writeBatch([]model.GuardLog{row}); err != nil {
		t.Fatal(err)
	}
	rows, _, err = r.List(context.Background(), 0, "8.8.8.8", "hub.example.com", "", "/login", 1, 20)
	if err != nil || rows[0].Hits != 503 || rows[0].LastTS != now+2000 {
		t.Fatalf("upsert failed: %+v %v", rows, err)
	}
	changed := row
	changed.Mode = "allowlist"
	changed.EventKey = guardKey(changed)
	if err := r.writeBatch([]model.GuardLog{changed}); err != nil {
		t.Fatal(err)
	}
	_, total, err = r.List(context.Background(), 0, "", "", "", "/login", 1, 20)
	if err != nil || total != 2 {
		t.Fatalf("different snapshots merged: %d %v", total, err)
	}
}

func TestGuardLogHardRowCap(t *testing.T) {
	db, err := Open(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { d, _ := db.DB(); d.Close() }()
	r := NewGuardRepo(db)
	r.Close()
	rows := make([]model.GuardLog, guardMaxRows+5)
	for i := range rows {
		rows[i] = model.GuardLog{TS: time.Now().UnixMilli(), LastTS: time.Now().UnixMilli(), Hits: 1, EventKey: fmt.Sprintf("key-%d", i)}
	}
	if err := db.CreateInBatches(rows, 256).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.writeBatch([]model.GuardLog{{TS: time.Now().UnixMilli(), LastTS: time.Now().UnixMilli(), EventKey: "newest", Hits: 1}}); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&model.GuardLog{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != guardMaxRows {
		t.Fatalf("row cap exceeded: %d", count)
	}
}

func TestHistoricalTotalsUseHitsNotSampleRows(t *testing.T) {
	db, err := Open(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { d, _ := db.DB(); d.Close() }()
	r := NewGuardRepo(db)
	r.Close()
	now := time.Now().UnixMilli()
	if err := r.writeBatch([]model.GuardLog{
		{TS: now, LastTS: now, Hits: 3, RuleHost: "hub.example.com", IP: "8.8.8.8", EventKey: "seed-one"},
		{TS: now, LastTS: now + 1, Hits: 5, RuleHost: "hub.example.com", IP: "9.9.9.9", EventKey: "seed-two"},
	}); err != nil {
		t.Fatal(err)
	}
	totals, err := r.HistoricalRuleTotals()
	if err != nil {
		t.Fatal(err)
	}
	if len(totals) != 1 || totals[0].Count != 8 || totals[0].LastIP != "9.9.9.9" || totals[0].LastAt != now+1 {
		t.Fatalf("totals: %+v", totals)
	}
}
