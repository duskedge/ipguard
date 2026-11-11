package repository

import (
	"gorm.io/gorm"

	"github.com/duskedge/ipguard/internal/model"
)

// AuditRepo 读写审计日志。
type AuditRepo struct{ db *gorm.DB }

// NewAuditRepo 创建仓库。
func NewAuditRepo(db *gorm.DB) *AuditRepo { return &AuditRepo{db: db} }

// Add 写一条审计记录。
func (r *AuditRepo) Add(a *model.AuditLog) error { return r.db.Create(a).Error }

// List 按时间倒序分页；q 非空时按目标模糊匹配。
func (r *AuditRepo) List(q string, page, size int) ([]model.AuditLog, int64, error) {
	if size <= 0 || size > 200 {
		size = 20
	}
	if page < 1 {
		page = 1
	}
	base := func() *gorm.DB {
		tx := r.db.Model(&model.AuditLog{})
		if q != "" {
			tx = tx.Where("target LIKE ?", "%"+q+"%")
		}
		return tx
	}
	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := []model.AuditLog{}
	err := base().Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error
	return rows, total, err
}
