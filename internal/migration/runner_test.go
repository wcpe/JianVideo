package migration

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRegistryOrdersMigrationsAndRejectsDuplicates(t *testing.T) {
	registry, err := NewRegistry(
		Migration{ID: "20260708_0002_backfill", Description: "回填"},
		Migration{ID: "20260708_0001_schema", Description: "建表"},
	)
	if err != nil {
		t.Fatalf("创建 registry 失败: %v", err)
	}

	want := []string{"20260708_0001_schema", "20260708_0002_backfill"}
	if got := registry.IDs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("migration 排序不正确: got=%v want=%v", got, want)
	}

	if _, err := NewRegistry(Migration{ID: "dup"}, Migration{ID: "dup"}); err == nil {
		t.Fatal("重复 migration ID 应被拒绝")
	}
}

func TestDryRunDoesNotWriteBusinessSchemaMigrationOrAuditTables(t *testing.T) {
	gdb, dbPath := openLegacyDB(t)
	beforeLibraries := countRows(t, gdb, "library_paths")
	beforeMedia := countRows(t, gdb, "media_files")

	runner := newDefaultRunner(t, gdb, dbPath)
	plan, err := runner.DryRun(context.Background())
	if err != nil {
		t.Fatalf("dry-run 失败: %v", err)
	}
	if len(plan.Steps) == 0 {
		t.Fatal("dry-run 应返回待执行步骤")
	}

	if testTableExists(t, gdb, "schema_migrations") {
		t.Fatal("dry-run 不应创建 schema_migrations 表")
	}
	if testTableExists(t, gdb, "audit_events") {
		t.Fatal("dry-run 不应创建 audit_events 表")
	}
	if testTableExists(t, gdb, "spaces") {
		t.Fatal("dry-run 不应创建业务表 spaces")
	}
	if testColumnExists(t, gdb, "media_files", "space_id") {
		t.Fatal("dry-run 不应修改 media_files schema")
	}
	if got := countRows(t, gdb, "library_paths"); got != beforeLibraries {
		t.Fatalf("dry-run 不应改 library_paths 行数: got=%d want=%d", got, beforeLibraries)
	}
	if got := countRows(t, gdb, "media_files"); got != beforeMedia {
		t.Fatalf("dry-run 不应改 media_files 行数: got=%d want=%d", got, beforeMedia)
	}
}

func TestRunCreatesVerifiedSQLiteBackupBeforeApplyingMigrations(t *testing.T) {
	gdb, dbPath := openLegacyDB(t)

	result, err := newDefaultRunner(t, gdb, dbPath).Run(context.Background())
	if err != nil {
		t.Fatalf("执行迁移失败: %v", err)
	}
	if result.Backup.Path == "" {
		t.Fatal("真实迁移应返回备份路径")
	}
	if !result.Backup.IntegrityOK {
		t.Fatal("备份应通过 PRAGMA integrity_check")
	}

	stat, err := os.Stat(result.Backup.Path)
	if err != nil {
		t.Fatalf("备份文件不存在: %v", err)
	}
	if stat.Size() == 0 {
		t.Fatal("备份文件不应为空")
	}
	if got := sqliteIntegrityCheck(t, result.Backup.Path); got != "ok" {
		t.Fatalf("备份 integrity_check 失败: %s", got)
	}
}

func TestRunnerCanResumeAfterInterruptWithoutReapplyingSucceededMigration(t *testing.T) {
	gdb, dbPath := openLegacyDB(t)
	failSecond := true
	registry, err := NewRegistry(
		Migration{
			ID:          "20260708_9001_marker",
			Description: "写入重入标记",
			SafeToRetry: true,
			Up: func(_ context.Context, tx *gorm.DB) error {
				if err := tx.Exec("CREATE TABLE IF NOT EXISTS reentry_markers (id INTEGER PRIMARY KEY)").Error; err != nil {
					return err
				}
				return tx.Exec("INSERT INTO reentry_markers(id) VALUES (1)").Error
			},
			Validate: func(_ context.Context, db *gorm.DB) (Validation, error) {
				if got := countRows(t, db, "reentry_markers"); got != 1 {
					return Validation{}, errors.New("重入标记数量异常")
				}
				return Validation{Summary: "重入标记已写入"}, nil
			},
		},
		Migration{
			ID:          "20260708_9002_interrupt_once",
			Description: "模拟中断一次",
			SafeToRetry: true,
			Up: func(_ context.Context, tx *gorm.DB) error {
				if failSecond {
					return errors.New("模拟迁移中断")
				}
				return tx.Exec("CREATE TABLE IF NOT EXISTS reentry_done (id INTEGER PRIMARY KEY)").Error
			},
			Validate: func(_ context.Context, db *gorm.DB) (Validation, error) {
				if !testTableExists(t, db, "reentry_done") {
					return Validation{}, errors.New("重入完成表不存在")
				}
				return Validation{Summary: "重入完成"}, nil
			},
		},
	)
	if err != nil {
		t.Fatalf("创建 registry 失败: %v", err)
	}

	runner := NewRunner(gdb, RunnerOptions{
		DBPath:    dbPath,
		BackupDir: filepath.Join(t.TempDir(), "backups"),
		Registry:  registry,
		Now:       fixedNow,
	})
	if _, err := runner.Run(context.Background()); err == nil {
		t.Fatal("第一次迁移应模拟中断失败")
	}
	if got := migrationStatus(t, gdb, "20260708_9001_marker"); got != MigrationStatusSucceeded {
		t.Fatalf("首个 migration 应已成功: %s", got)
	}
	if got := migrationStatus(t, gdb, "20260708_9002_interrupt_once"); got != MigrationStatusFailed {
		t.Fatalf("中断 migration 应记录 failed: %s", got)
	}
	if got := countRows(t, gdb, "reentry_markers"); got != 1 {
		t.Fatalf("第一次执行后标记数量异常: %d", got)
	}
	if got := countAuditEvents(t, gdb, "migration.failed"); got != 1 {
		t.Fatalf("失败迁移应写系统审计事件: %d", got)
	}

	failSecond = false
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("第二次重入迁移失败: %v", err)
	}
	if got := countRows(t, gdb, "reentry_markers"); got != 1 {
		t.Fatalf("已成功 migration 不应重复执行: %d", got)
	}
	if got := migrationStatus(t, gdb, "20260708_9002_interrupt_once"); got != MigrationStatusSucceeded {
		t.Fatalf("中断 migration 重入后应成功: %s", got)
	}
}

func TestDefaultMigrationBackfillsDefaultSpaceAndCreatesSmokeIndexes(t *testing.T) {
	gdb, dbPath := openLegacyDB(t)
	beforeLibraries := countRows(t, gdb, "library_paths")
	beforeMedia := countRows(t, gdb, "media_files")

	result, err := newDefaultRunner(t, gdb, dbPath).Run(context.Background())
	if err != nil {
		t.Fatalf("执行默认迁移失败: %v", err)
	}
	if len(result.Applied) == 0 {
		t.Fatal("默认迁移应至少应用一个步骤")
	}

	if got := countRows(t, gdb, "library_paths"); got != beforeLibraries {
		t.Fatalf("迁移不应改变媒体库数量: got=%d want=%d", got, beforeLibraries)
	}
	if got := countRows(t, gdb, "media_files"); got != beforeMedia {
		t.Fatalf("迁移不应改变媒体数量: got=%d want=%d", got, beforeMedia)
	}
	if got := countWhere(t, gdb, "spaces", "id = ?", DefaultSpaceID); got != 1 {
		t.Fatalf("默认 Space 不存在: %d", got)
	}
	if got := countWhere(t, gdb, "library_paths", "space_id = '' OR space_id IS NULL"); got != 0 {
		t.Fatalf("library_paths 存在未回填 space_id 的记录: %d", got)
	}
	if got := countWhere(t, gdb, "media_files", "space_id = '' OR space_id IS NULL"); got != 0 {
		t.Fatalf("media_files 存在未回填 space_id 的记录: %d", got)
	}

	for _, indexName := range []string{
		"idx_media_files_library_id",
		"idx_library_paths_space_id",
		"idx_media_files_space_id",
		"idx_media_files_space_library_added",
	} {
		if !testIndexExists(t, gdb, indexName) {
			t.Fatalf("关键索引不存在: %s", indexName)
		}
	}

	var ids []int64
	if err := gdb.Raw(
		"SELECT id FROM media_files WHERE space_id = ? ORDER BY added_at DESC, id DESC LIMIT 10",
		DefaultSpaceID,
	).Scan(&ids).Error; err != nil {
		t.Fatalf("FR2-007 查询 smoke 失败: %v", err)
	}
	if len(ids) == 0 {
		t.Fatal("FR2-007 查询 smoke 应返回迁移后的媒体")
	}
	if got := countAuditEvents(t, gdb, "migration.started"); got == 0 {
		t.Fatal("迁移开始应写系统审计事件")
	}
	if got := countAuditEvents(t, gdb, "migration.succeeded"); got == 0 {
		t.Fatal("迁移成功应写系统审计事件")
	}
}

func newDefaultRunner(t *testing.T, gdb *gorm.DB, dbPath string) *Runner {
	t.Helper()
	registry, err := NewRegistry(DefaultMigrations()...)
	if err != nil {
		t.Fatalf("创建默认 registry 失败: %v", err)
	}
	return NewRunner(gdb, RunnerOptions{
		DBPath:    dbPath,
		BackupDir: filepath.Join(t.TempDir(), "backups"),
		Registry:  registry,
		Now:       fixedNow,
	})
}

func openLegacyDB(t *testing.T) (*gorm.DB, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "jianvideo-v020.db")
	gdb, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开 legacy gorm 失败: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, err := gdb.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	raw, err := os.ReadFile(filepath.Join("testdata", "v020_minimal.sql"))
	if err != nil {
		t.Fatalf("读取 v0.20 fixture 失败: %v", err)
	}
	for _, stmt := range strings.Split(string(raw), ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if err := gdb.Exec(stmt).Error; err != nil {
			t.Fatalf("初始化 legacy fixture 失败: %v\nSQL: %s", err, stmt)
		}
	}
	return gdb, dbPath
}

func fixedNow() time.Time {
	return time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
}

func countRows(t *testing.T, gdb *gorm.DB, table string) int64 {
	t.Helper()
	return countWhere(t, gdb, table, "1 = 1")
}

func countWhere(t *testing.T, gdb *gorm.DB, table, where string, args ...any) int64 {
	t.Helper()
	var count int64
	if err := gdb.Table(table).Where(where, args...).Count(&count).Error; err != nil {
		t.Fatalf("统计 %s 失败: %v", table, err)
	}
	return count
}

func testTableExists(t *testing.T, gdb *gorm.DB, table string) bool {
	t.Helper()
	var count int64
	if err := gdb.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).
		Scan(&count).Error; err != nil {
		t.Fatalf("查询表是否存在失败: %v", err)
	}
	return count == 1
}

func testColumnExists(t *testing.T, gdb *gorm.DB, table, column string) bool {
	t.Helper()
	rows, err := gdb.Raw("PRAGMA table_info(" + table + ")").Rows()
	if err != nil {
		t.Fatalf("读取表字段失败: %v", err)
	}
	defer func() {
		_ = rows.Close()
	}()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("解析表字段失败: %v", err)
		}
		if name == column {
			return true
		}
	}
	return false
}

func testIndexExists(t *testing.T, gdb *gorm.DB, indexName string) bool {
	t.Helper()
	var count int64
	if err := gdb.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?", indexName).
		Scan(&count).Error; err != nil {
		t.Fatalf("查询索引失败: %v", err)
	}
	return count == 1
}

func migrationStatus(t *testing.T, gdb *gorm.DB, id string) string {
	t.Helper()
	var status string
	if err := gdb.Raw("SELECT status FROM schema_migrations WHERE id = ?", id).Scan(&status).Error; err != nil {
		t.Fatalf("读取 migration 状态失败: %v", err)
	}
	return status
}

func countAuditEvents(t *testing.T, gdb *gorm.DB, eventType string) int64 {
	t.Helper()
	return countWhere(t, gdb, "audit_events", "scope = 'system' AND space_id IS NULL AND event_type = ?", eventType)
}

func sqliteIntegrityCheck(t *testing.T, dbPath string) string {
	t.Helper()
	sqlDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("打开备份失败: %v", err)
	}
	defer func() {
		_ = sqlDB.Close()
	}()
	var result string
	if err := sqlDB.QueryRow("PRAGMA integrity_check").Scan(&result); err != nil {
		t.Fatalf("执行 integrity_check 失败: %v", err)
	}
	return result
}
