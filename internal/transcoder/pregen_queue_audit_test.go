package transcoder

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/db/models"
)

func TestPregenQueueRecordsTaskAuditEvents(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.TranscodeTask{}, &models.AuditEvent{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	q := NewPregenQueue(db, func(int64, string) error { return nil }).WithAudit(audit.NewRecorder(db))

	id, err := q.EnqueueInSpace(models.DefaultSpaceID, 42, 1, "h264", 0, 0)
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	task, ok := q.nextPending()
	if !ok || task.ID != id {
		t.Fatal("应取到刚入队任务")
	}
	q.runTask(task)

	assertPregenAuditCount(t, db, "task.created", 1)
	assertPregenAuditCount(t, db, "task.succeeded", 1)
}

func assertPregenAuditCount(t *testing.T, db *gorm.DB, action string, want int64) {
	t.Helper()
	var count int64
	if err := db.Model(&models.AuditEvent{}).Where("action = ?", action).Count(&count).Error; err != nil {
		t.Fatalf("统计审计事件失败: %v", err)
	}
	if count != want {
		t.Fatalf("%s 审计事件数量 got=%d want=%d", action, count, want)
	}
}
