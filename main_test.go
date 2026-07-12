package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSQLiteDataSourceNamesSeparateNormalWALAndDryRunReadOnlyModes(t *testing.T) {
	const normalPath = "jianvideo.db"
	normal := sqliteDataSourceName(normalPath)
	wantNormal := normalPath + "?_busy_timeout=10000&_journal_mode=WAL&_foreign_keys=on"
	if normal != wantNormal {
		t.Fatalf("普通启动 DSN 应保持原 WAL 行为: got=%s want=%s", normal, wantNormal)
	}

	dbPath := filepath.Join(t.TempDir(), "资料 # 100%", "jian video.sqlite")
	dryRun := sqliteReadOnlyDataSourceName(dbPath)
	parsed, err := url.Parse(dryRun)
	if err != nil {
		t.Fatalf("dry-run DSN 应为有效 file URI: %v", err)
	}
	if parsed.Scheme != "file" || parsed.Query().Get("mode") != "ro" {
		t.Fatalf("dry-run DSN 应使用 file URI 只读模式: %s", dryRun)
	}
	if strings.Contains(dryRun, "_journal_mode") {
		t.Fatalf("dry-run DSN 不得设置 journal_mode: %s", dryRun)
	}
	if gotPath := sqliteURIPath(parsed); filepath.Clean(gotPath) != filepath.Clean(dbPath) {
		t.Fatalf("dry-run file URI 未保留特殊字符路径: got=%s want=%s", gotPath, dbPath)
	}
}

func TestSQLiteReadOnlyDataSourceNameUsesAuthorityFreeUNCURI(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("UNC URI 驱动验证仅适用于 Windows")
	}
	const uncPath = `\\127.0.0.1\jianvideo-missing-share\资料 # 100%\missing.sqlite`
	dsn := sqliteReadOnlyDataSourceName(uncPath)
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("UNC dry-run DSN 应为有效 file URI: %v", err)
	}
	if parsed.Host != "" {
		t.Fatalf("UNC file URI 不得包含 authority: host=%s dsn=%s", parsed.Host, dsn)
	}
	if !strings.HasPrefix(parsed.Path, "//127.0.0.1/jianvideo-missing-share/") {
		t.Fatalf("UNC file URI 路径不正确: path=%s dsn=%s", parsed.Path, dsn)
	}
	if parsed.Query().Get("mode") != "ro" {
		t.Fatalf("UNC dry-run DSN 应保持只读模式: %s", dsn)
	}

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("创建 UNC SQLite 连接失败: %v", err)
	}
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = db.PingContext(ctx)
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatal("UNC SQLite 连接验证超时")
	}
	if err == nil {
		t.Fatal("测试用不存在 UNC 数据库不应成功打开")
	}
	if strings.Contains(strings.ToLower(err.Error()), "invalid uri authority") {
		t.Fatalf("go-sqlite3 拒绝了 UNC URI authority: %v", err)
	}
	t.Logf("go-sqlite3 已解析无 authority UNC URI，预期路径打开失败: %v", err)
}

func TestSQLiteReadOnlyDataSourceNameRejectsWrites(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "资料 # 100%")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("创建特殊字符数据库目录失败: %v", err)
	}
	dbPath := filepath.Join(dir, "jian video.sqlite")
	writable, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("创建只读连接测试数据库失败: %v", err)
	}
	if err := writable.Exec("CREATE TABLE write_probe (id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatalf("创建写入探针表失败: %v", err)
	}
	writableSQL, err := writable.DB()
	if err != nil {
		t.Fatalf("获取可写测试连接失败: %v", err)
	}
	if err := writableSQL.Close(); err != nil {
		t.Fatalf("关闭可写测试连接失败: %v", err)
	}

	readOnly, err := gorm.Open(sqlite.Open(sqliteReadOnlyDataSourceName(dbPath)), &gorm.Config{})
	if err != nil {
		t.Fatalf("使用只读 file URI 打开数据库失败: %v", err)
	}
	readOnlySQL, err := readOnly.DB()
	if err != nil {
		t.Fatalf("获取只读测试连接失败: %v", err)
	}
	defer func() { _ = readOnlySQL.Close() }()
	if err := readOnly.Exec("INSERT INTO write_probe(id) VALUES (1)").Error; err == nil {
		t.Fatal("通过 dry-run 只读连接写入必须失败")
	}
}

func sqliteURIPath(uri *url.URL) string {
	path := filepath.FromSlash(uri.Path)
	if runtime.GOOS == "windows" && len(path) >= 3 && path[0] == '\\' && path[2] == ':' {
		return path[1:]
	}
	return path
}

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
