package tasks

import "gorm.io/gorm"

// Tx 是跨域事务句柄抽象（FR2-070 续）。
// api / settings 钩子可传入实现了 DB() 的仓库事务，而不在业务签名上暴露 *gorm.DB。
// domain 内部（如 transcoder）仍可用 AsTx 从 *gorm.DB 适配。
type Tx interface {
	DB() *gorm.DB
}

// gormTx 将 *gorm.DB 适配为 Tx。
type gormTx struct {
	db *gorm.DB
}

func (t gormTx) DB() *gorm.DB { return t.db }

// AsTx 将 *gorm.DB 包装为 Tx；tx 为 nil 时返回 nil。
func AsTx(tx *gorm.DB) Tx {
	if tx == nil {
		return nil
	}
	return gormTx{db: tx}
}
