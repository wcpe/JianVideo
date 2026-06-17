package db

import (
	"log"
	"os"
	"path/filepath"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"jianvideo/internal/db/models"
)

// DB 数据库连接。
type DB struct {
	*gorm.DB
}

// Init 初始化数据库连接并执行迁移。
func Init(dbPath string) (*DB, error) {
	// 确保目录存在
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := ensureDir(dir); err != nil {
			return nil, err
		}
	}

	dsn := "file:" + dbPath + "?cache=shared&_journal_mode=WAL&_busy_timeout=5000"
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	// 自动迁移
	if err := gdb.AutoMigrate(&models.LibraryPath{}, &models.MediaFile{}); err != nil {
		return nil, err
	}

	log.Println("[INFO] 数据库初始化完成，路径:", dbPath)
	return &DB{gdb}, nil
}

func ensureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}
