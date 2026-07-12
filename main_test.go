package main

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestSQLiteDataSourceNameAllowsConcurrentWrites 验证并行请求在 WAL 与 busy timeout 下不会直接报 database locked。
func TestSQLiteDataSourceNameAllowsConcurrentWrites(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "concurrent.db")
	db, err := gorm.Open(sqlite.Open(sqliteDataSourceName(dbPath)), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开并发测试数据库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("获取连接池失败: %v", err)
	}
	sqlDB.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Exec("CREATE TABLE writes (id INTEGER PRIMARY KEY AUTOINCREMENT, value TEXT NOT NULL)").Error; err != nil {
		t.Fatalf("创建测试表失败: %v", err)
	}

	const workers = 16
	const writesPerWorker = 20
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for index := 0; index < writesPerWorker; index++ {
				if err := db.Exec("INSERT INTO writes(value) VALUES (?)", fmt.Sprintf("%d-%d", worker, index)).Error; err != nil {
					errs <- err
					return
				}
			}
		}(worker)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("并发写入不应失败: %v", err)
	}
	var count int64
	if err := db.Table("writes").Count(&count).Error; err != nil {
		t.Fatalf("统计写入数量失败: %v", err)
	}
	if count != workers*writesPerWorker {
		t.Fatalf("写入数量不完整: got=%d want=%d", count, workers*writesPerWorker)
	}
}
