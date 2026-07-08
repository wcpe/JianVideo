package library

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/db/models"
)

func TestTaskQueueRecordsCreatedAndSucceededAudit(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.ScanTask{}, &models.AuditEvent{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	q := NewTaskQueue(db, func(int64, string, string, string) (int, error) { return 3, nil }).WithAudit(audit.NewRecorder(db))

	id, err := q.EnqueueInSpace(models.DefaultSpaceID, 7, "D:/media", "local", models.ScanTypeFull)
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	task, ok := q.nextPending()
	if !ok || task.ID != id {
		t.Fatal("应取到刚入队任务")
	}
	q.runTask(task)

	assertAuditCount(t, db, "task.created", 1)
	assertAuditCount(t, db, "task.succeeded", 1)
}

func TestTaskQueueCancelAndRetryRecordAuditEvents(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.LibraryPath{}, &models.ScanTask{}, &models.AuditEvent{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	lp := models.LibraryPath{SpaceID: models.DefaultSpaceID, Path: t.TempDir(), Type: "local", Enabled: 1}
	if err := db.Create(&lp).Error; err != nil {
		t.Fatalf("创建媒体库失败: %v", err)
	}
	q := NewTaskQueue(db, func(int64, string, string, string) (int, error) {
		t.Fatal("取消后的 pending 任务不应被执行")
		return 0, nil
	}).WithAudit(audit.NewRecorder(db))

	taskID, err := q.EnqueueInSpace(models.DefaultSpaceID, lp.ID, lp.Path, lp.Type, models.ScanTypeFull)
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	if err := q.CancelTaskInSpace(models.DefaultSpaceID, taskID); err != nil {
		t.Fatalf("取消任务失败: %v", err)
	}
	if err := q.RetryTaskInSpace(models.DefaultSpaceID, taskID); err != nil {
		t.Fatalf("重试任务失败: %v", err)
	}

	assertAuditCount(t, db, "task.created", 1)
	assertAuditCount(t, db, "task.canceled", 1)
	assertAuditCount(t, db, "task.retried", 1)
	var task models.ScanTask
	if err := db.First(&task, taskID).Error; err != nil {
		t.Fatalf("读取任务失败: %v", err)
	}
	if task.Status != models.ScanTaskStatusPending || task.Error != "" {
		t.Fatalf("重试后任务状态应恢复 pending 且清空错误，实际 %+v", task)
	}
}
