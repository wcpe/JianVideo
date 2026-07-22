package tasks

import (
	"context"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

func TestAsTx_NilAndRoundTrip(t *testing.T) {
	if AsTx(nil) != nil {
		t.Fatal("AsTx(nil) 应返回 nil")
	}
	db, err := gorm.Open(sqlite.Open("file:tx-as?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开内存库失败: %v", err)
	}
	tx := AsTx(db)
	if tx == nil || tx.DB() != db {
		t.Fatal("AsTx 应包装同一 *gorm.DB")
	}
}

func TestEnqueueTx_RequiresTx(t *testing.T) {
	svc, _ := newTaskTestService(t)
	_, err := svc.EnqueueTx(context.Background(), nil, EnqueueInput{
		Scope: models.TaskScopeSystem, Type: "test.tx.nil",
	})
	if err == nil {
		t.Fatal("EnqueueTx(nil) 应失败")
	}
}

func TestEnqueueTx_ViaAsTx(t *testing.T) {
	svc, db := newTaskTestService(t)
	var created *models.Task
	err := db.WithContext(context.Background()).Transaction(func(tx *gorm.DB) error {
		task, e := svc.EnqueueTx(context.Background(), AsTx(tx), EnqueueInput{
			Scope: models.TaskScopeSystem, Type: "test.tx.ok", IdempotencyKey: "tx-ok-1",
		})
		if e != nil {
			return e
		}
		created = task
		return nil
	})
	if err != nil {
		t.Fatalf("事务内 EnqueueTx 失败: %v", err)
	}
	if created == nil || created.ID == 0 {
		t.Fatal("应创建任务")
	}
	var row models.Task
	if err := db.First(&row, created.ID).Error; err != nil {
		t.Fatalf("回读任务失败: %v", err)
	}
	if row.Type != "test.tx.ok" {
		t.Fatalf("任务类型不符: %s", row.Type)
	}
}
