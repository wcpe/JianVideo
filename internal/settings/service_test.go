package settings

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"jianvideo/internal/db/models"
)

// setupTestDB 创建测试用的内存数据库并迁移 settings 表。
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.Setting{}); err != nil {
		t.Fatalf("迁移 settings 表失败: %v", err)
	}
	return db
}

func TestSetAndGet(t *testing.T) {
	svc := NewService(setupTestDB(t))

	if err := svc.Set(KeyScanInterval, "3600"); err != nil {
		t.Fatalf("写入设置失败: %v", err)
	}

	v, err := svc.Get(KeyScanInterval)
	if err != nil {
		t.Fatalf("读取设置失败: %v", err)
	}
	if v != "3600" {
		t.Fatalf("期望值 3600, 实际 %q", v)
	}
}

func TestGetMissingReturnsEmpty(t *testing.T) {
	svc := NewService(setupTestDB(t))

	v, err := svc.Get("not_exist")
	if err != nil {
		t.Fatalf("读取缺失键不应报错, 实际: %v", err)
	}
	if v != "" {
		t.Fatalf("缺失键应返回空串, 实际 %q", v)
	}
}

// TestSetUpsert 同一 key 重复写入应覆盖旧值，不报主键冲突。
func TestSetUpsert(t *testing.T) {
	svc := NewService(setupTestDB(t))

	if err := svc.Set(KeyScanInterval, "60"); err != nil {
		t.Fatalf("首次写入失败: %v", err)
	}
	if err := svc.Set(KeyScanInterval, "120"); err != nil {
		t.Fatalf("覆盖写入失败（疑似主键冲突）: %v", err)
	}

	v, _ := svc.Get(KeyScanInterval)
	if v != "120" {
		t.Fatalf("期望覆盖为 120, 实际 %q", v)
	}
}

func TestGetAll(t *testing.T) {
	svc := NewService(setupTestDB(t))
	_ = svc.Set(KeyScanInterval, "300")
	_ = svc.Set(KeyRecycleBinPaths, `{"D":"D:/.recycle"}`)

	all, err := svc.GetAll()
	if err != nil {
		t.Fatalf("读取全部设置失败: %v", err)
	}
	if all[KeyScanInterval] != "300" || all[KeyRecycleBinPaths] != `{"D":"D:/.recycle"}` {
		t.Fatalf("GetAll 返回不正确: %v", all)
	}
}

func TestSetManyAtomicAndPersist(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db)

	err := svc.SetMany(map[string]string{
		KeyScanInterval:    "900",
		KeyRecycleBinPaths: `{"E":"E:/trash"}`,
	})
	if err != nil {
		t.Fatalf("批量写入失败: %v", err)
	}

	// 用同一底层数据库新建服务实例，验证持久化（非内存态残留）
	svc2 := NewService(db)
	all, err := svc2.GetAll()
	if err != nil {
		t.Fatalf("二次读取失败: %v", err)
	}
	if all[KeyScanInterval] != "900" {
		t.Fatalf("扫描周期未持久化, 实际 %q", all[KeyScanInterval])
	}
	if all[KeyRecycleBinPaths] != `{"E":"E:/trash"}` {
		t.Fatalf("回收站路径未持久化, 实际 %q", all[KeyRecycleBinPaths])
	}
}
