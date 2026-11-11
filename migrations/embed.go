// Package migrations 嵌入 SQLite 迁移脚本。文件名格式 NNNNNN_name.up.sql / .down.sql。
package migrations

import "embed"

//go:embed sqlite/*.sql
var SQLite embed.FS
