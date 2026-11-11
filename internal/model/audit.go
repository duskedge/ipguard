// Package model 定义持久化模型。
package model

import "time"

// AuditLog 记录一次封禁/解封操作。
type AuditLog struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Operator  string    `json:"operator"`
	ClientIP  string    `json:"client_ip"`
	Action    string    `json:"action"`
	Target    string    `json:"target"`
	Duration  string    `json:"duration"`
	Reason    string    `json:"reason"`
	Source    string    `json:"source"`
	Result    string    `json:"result"`
	Error     string    `json:"error"`
}

// TableName 固定表名。
func (AuditLog) TableName() string { return "audit_logs" }
