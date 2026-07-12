package migration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
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

func TestFR2017RealV020UpgradePreservesDataAndIsIdempotent(t *testing.T) {
	gdb, dbPath := openFR2017Fixture(t)
	before := captureLegacySnapshot(t, gdb)

	runner := newFR2017DefaultRunner(t, gdb, dbPath)
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("执行真实 v0.20 升级失败: %v", err)
	}

	assertLegacySnapshot(t, gdb, before)
	assertCompleteSpaceOwnership(t, gdb)
	assertLegacyTaskMapping(t, gdb)
	assertFR2007QuerySmoke(t, gdb)

	for attempt := 0; attempt < 2; attempt++ {
		if err := migrateTasksCenter(context.Background(), gdb); err != nil {
			t.Fatalf("旧任务映射第 %d 次重复执行失败: %v", attempt+1, err)
		}
	}
	assertLegacyTaskMapping(t, gdb)

	second, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("完整迁移第二次执行失败: %v", err)
	}
	if len(second.Applied) != 0 || len(second.Skipped) != 0 || second.Backup.Path != "" {
		t.Fatalf("无待迁移步骤时不应重复备份或应用: %+v", second)
	}
	assertLegacySnapshot(t, gdb, before)
	assertLegacyTaskMapping(t, gdb)
}

func TestFR2017V020AlbumCompatIsEstimatedValidatedAndRepeatable(t *testing.T) {
	gdb, _ := openFR2017Fixture(t)
	before := captureLegacySnapshot(t, gdb)
	plan, err := estimateV020AlbumCompat(context.Background(), gdb)
	if err != nil {
		t.Fatalf("估算 v0.20 相册兼容迁移失败: %v", err)
	}
	if plan.EstimatedRows != 5 {
		t.Fatalf("相册兼容迁移估算行数不正确: got=%d want=5", plan.EstimatedRows)
	}

	for attempt := 0; attempt < 2; attempt++ {
		if err := migrateV020AlbumCompat(context.Background(), gdb); err != nil {
			t.Fatalf("v0.20 相册兼容迁移第 %d 次执行失败: %v", attempt+1, err)
		}
	}
	if _, err := validateV020AlbumCompat(context.Background(), gdb); err != nil {
		t.Fatalf("校验 v0.20 相册兼容迁移失败: %v", err)
	}
	assertLegacySnapshot(t, gdb, before)
	for _, table := range []string{"albums", "album_items"} {
		if got := countWhere(t, gdb, table, "space_id != ?", DefaultSpaceID); got != 0 {
			t.Fatalf("%s 未完整回填默认 Space: %d", table, got)
		}
	}
}

func TestFR2017LegacyTaskMappingIsIdempotent(t *testing.T) {
	gdb, _ := openFR2017Fixture(t)
	before := captureLegacySnapshot(t, gdb)
	for _, statement := range []string{
		"ALTER TABLE scan_tasks ADD COLUMN space_id TEXT NOT NULL DEFAULT '" + DefaultSpaceID + "'",
		"ALTER TABLE transcode_tasks ADD COLUMN space_id TEXT NOT NULL DEFAULT '" + DefaultSpaceID + "'",
	} {
		if err := gdb.Exec(statement).Error; err != nil {
			t.Fatalf("准备 0006 前置 Space 字段失败: %v", err)
		}
	}

	for attempt := 0; attempt < 3; attempt++ {
		if err := migrateTasksCenter(context.Background(), gdb); err != nil {
			t.Fatalf("0006 旧任务映射第 %d 次执行失败: %v", attempt+1, err)
		}
	}
	assertLegacySnapshot(t, gdb, before)
	assertLegacyTaskMapping(t, gdb)
}

func TestFR2017BackupFailureStopsBeforeAnyMigrationWrite(t *testing.T) {
	gdb, dbPath := openFR2017Fixture(t)
	before := captureLegacySnapshot(t, gdb)
	blockedBackupDir := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blockedBackupDir, []byte("占位"), 0o600); err != nil {
		t.Fatalf("创建备份阻塞文件失败: %v", err)
	}

	registry := mustRegistry(t, Migration{
		ID: "20260720_9001_fail_stop_marker", Description: "验证备份失败停止", SafeToRetry: true,
		Up: func(_ context.Context, tx *gorm.DB) error {
			return tx.Exec("CREATE TABLE should_not_exist (id INTEGER PRIMARY KEY)").Error
		},
	})
	runner := NewRunner(gdb, RunnerOptions{
		DBPath: dbPath, BackupDir: blockedBackupDir, Registry: registry, Now: fr2017Now,
	})
	if _, err := runner.Run(context.Background()); err == nil {
		t.Fatal("备份目录不可创建时迁移必须失败")
	}

	assertLegacySnapshot(t, gdb, before)
	if testTableExists(t, gdb, "should_not_exist") {
		t.Fatal("备份失败后不得执行迁移写入")
	}
	if testTableExists(t, gdb, "schema_migrations") || testTableExists(t, gdb, "audit_events") {
		t.Fatal("备份失败后不得创建迁移元数据表")
	}
}

func TestFR2017MigrationFailureKeepsOpenableBackupAndRestoreMatchesLegacyData(t *testing.T) {
	gdb, dbPath := openFR2017Fixture(t)
	before := captureLegacySnapshot(t, gdb)
	registry := mustRegistry(t, Migration{
		ID: "20260720_9002_failure_after_backup", Description: "验证失败后保留备份", SafeToRetry: true,
		Up: func(_ context.Context, tx *gorm.DB) error {
			if err := tx.Exec("CREATE TABLE rolled_back_marker (id INTEGER PRIMARY KEY)").Error; err != nil {
				return err
			}
			return errors.New("模拟迁移失败")
		},
	})
	runner := NewRunner(gdb, RunnerOptions{
		DBPath: dbPath, BackupDir: filepath.Join(t.TempDir(), "backups"), Registry: registry, Now: fr2017Now,
	})

	result, err := runner.Run(context.Background())
	if err == nil {
		t.Fatal("模拟迁移应失败")
	}
	assertFailedMigrationBackup(t, gdb, result)
	assertRestoredLegacyDatabase(t, result.Backup.Path, before)
}

func assertFailedMigrationBackup(t *testing.T, gdb *gorm.DB, result RunResult) {
	t.Helper()
	if result.Backup.Path == "" || !result.Backup.IntegrityOK {
		t.Fatalf("迁移失败后应返回已校验备份: %+v", result.Backup)
	}
	if got := sqliteIntegrityCheck(t, result.Backup.Path); got != "ok" {
		t.Fatalf("失败迁移保留的备份不可打开: %s", got)
	}
	if got := migrationStatus(t, gdb, "20260720_9002_failure_after_backup"); got != MigrationStatusFailed {
		t.Fatalf("失败迁移状态不正确: %s", got)
	}
	if testTableExists(t, gdb, "rolled_back_marker") {
		t.Fatal("失败迁移的事务写入应回滚")
	}
}

func assertRestoredLegacyDatabase(t *testing.T, backupPath string, before legacySnapshot) {
	t.Helper()
	restoredPath := filepath.Join(t.TempDir(), "restored-v020.sqlite")
	copySQLiteFile(t, backupPath, restoredPath)
	restored := openSQLitePath(t, restoredPath)
	assertLegacySnapshot(t, restored, before)
	if testTableExists(t, restored, "schema_migrations") {
		t.Fatal("迁移前备份恢复后不应包含迁移元数据")
	}
}

func TestFR2017UpgradeDoesNotModifyOriginalMediaFiles(t *testing.T) {
	gdb, dbPath := openFR2017Fixture(t)
	mediaDir := t.TempDir()
	paths := []string{
		filepath.Join(mediaDir, "alpha.mp4"),
		filepath.Join(mediaDir, "trip.jpg"),
	}
	writeMediaFixture(t, paths[0], []byte("legacy-video-content"), time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC))
	writeMediaFixture(t, paths[1], []byte("legacy-image-content"), time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC))
	if err := gdb.Exec("UPDATE media_files SET file_path = ? WHERE id = 11", paths[0]).Error; err != nil {
		t.Fatalf("绑定视频文件路径失败: %v", err)
	}
	if err := gdb.Exec("UPDATE media_files SET file_path = ? WHERE id = 31", paths[1]).Error; err != nil {
		t.Fatalf("绑定图片文件路径失败: %v", err)
	}
	before := captureMediaFiles(t, paths)

	if _, err := newFR2017DefaultRunner(t, gdb, dbPath).Run(context.Background()); err != nil {
		t.Fatalf("执行真实升级失败: %v", err)
	}
	if after := captureMediaFiles(t, paths); !reflect.DeepEqual(after, before) {
		t.Fatalf("迁移不得修改原媒体 hash/mtime: before=%v after=%v", before, after)
	}
}

type legacyTaskRow struct {
	IdempotencyKey string
	SpaceID        string
	Type           string
	Status         string
	Progress       int
	Attempts       int
	ResourceType   string
	ResourceID     string
	PayloadJSON    string
}

func assertLegacyTaskMapping(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	var rows []legacyTaskRow
	if err := gdb.Raw(`SELECT idempotency_key, space_id, type, status, progress, attempts,
		resource_type, resource_id, payload_json FROM tasks
		WHERE idempotency_key LIKE 'scan:%' OR idempotency_key LIKE 'transcode:%'
		ORDER BY idempotency_key`).Scan(&rows).Error; err != nil {
		t.Fatalf("读取旧任务映射失败: %v", err)
	}
	if len(rows) != 8 {
		t.Fatalf("每个旧任务应只映射一次: got=%d want=8", len(rows))
	}
	expected := legacyTaskExpectations()
	for _, row := range rows {
		assertLegacyTaskRow(t, row, expected)
	}
	assertNoDuplicateTaskKeys(t, gdb)
}

func legacyTaskExpectations() map[string][]any {
	return map[string][]any{
		"scan:101":      {"library.scan", "succeeded", 100, 0, "library", "1"},
		"scan:102":      {"library.scan", "failed", 30, 1, "library", "2"},
		"scan:103":      {"library.scan", "running", 50, 0, "library", "3"},
		"scan:104":      {"library.scan", "pending", 0, 0, "library", "1"},
		"transcode:201": {"transcode.hls", "succeeded", 100, 0, "media", "11"},
		"transcode:202": {"transcode.hls", "failed", 0, 1, "media", "12"},
		"transcode:203": {"transcode.hls", "running", 0, 0, "media", "21"},
		"transcode:204": {"transcode.hls", "pending", 0, 0, "media", "31"},
	}
}

func assertLegacyTaskRow(t *testing.T, row legacyTaskRow, expected map[string][]any) {
	t.Helper()
	want, ok := expected[row.IdempotencyKey]
	if !ok {
		t.Fatalf("出现未知旧任务映射: %+v", row)
	}
	got := []any{row.Type, row.Status, row.Progress, row.Attempts, row.ResourceType, row.ResourceID}
	if !reflect.DeepEqual(got, want) || row.SpaceID != DefaultSpaceID {
		t.Fatalf("旧任务映射内容不正确 %s: got=%v space=%s want=%v", row.IdempotencyKey, got, row.SpaceID, want)
	}
	assertLegacyPayload(t, row)
}

func assertNoDuplicateTaskKeys(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	var count int64
	if err := gdb.Raw(`SELECT COUNT(*) FROM (
		SELECT idempotency_key FROM tasks WHERE idempotency_key != ''
		GROUP BY idempotency_key HAVING COUNT(*) > 1
	)`).Scan(&count).Error; err != nil {
		t.Fatalf("统计重复任务幂等键失败: %v", err)
	}
	if count != 0 {
		t.Fatalf("任务幂等键出现重复: %d", count)
	}
}

func assertLegacyPayload(t *testing.T, row legacyTaskRow) {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(row.PayloadJSON), &payload); err != nil {
		t.Fatalf("旧任务载荷不是有效 JSON %s: %v", row.IdempotencyKey, err)
	}
	parts := strings.Split(row.IdempotencyKey, ":")
	if len(parts) != 2 || payload["legacy_table"] != parts[0]+"_tasks" {
		t.Fatalf("旧任务载荷来源不正确 %s: %v", row.IdempotencyKey, payload)
	}
}

func assertCompleteSpaceOwnership(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	for _, table := range []string{
		"library_paths", "media_files", "tags", "scan_tasks", "transcode_tasks",
		"albums", "album_items", "shares", "media_health_issues", "tasks",
	} {
		if got := countWhere(t, gdb, table, "space_id IS NULL OR TRIM(space_id) = ''"); got != 0 {
			t.Fatalf("%s 的 Space 归属不完整: %d", table, got)
		}
	}
	if got := countWhere(t, gdb, "spaces", "id = ? AND owner_user_id = 1", DefaultSpaceID); got != 1 {
		t.Fatalf("默认 Space 未完整归属首个旧用户: %d", got)
	}
}

func assertFR2007QuerySmoke(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	queries := []struct {
		name string
		sql  string
		args []any
	}{
		{"Space 活跃媒体", "SELECT id FROM media_files WHERE space_id = ? AND deleted_at IS NULL AND file_state = 'available' ORDER BY added_at DESC, id DESC LIMIT 20", []any{DefaultSpaceID}},
		{"媒体库活跃媒体", "SELECT id FROM media_files WHERE space_id = ? AND library_id = ? AND deleted_at IS NULL AND file_state = 'available' ORDER BY added_at DESC, id DESC LIMIT 20", []any{DefaultSpaceID, 1}},
		{"格式筛选", "SELECT id FROM media_files WHERE space_id = ? AND LOWER(format) IN (?, ?) AND deleted_at IS NULL AND file_state = 'available' ORDER BY added_at DESC, id DESC LIMIT 20", []any{DefaultSpaceID, "mp4", "mkv"}},
		{"回收站", "SELECT id FROM media_files WHERE space_id = ? AND deleted_at IS NOT NULL ORDER BY deleted_at DESC LIMIT 20", []any{DefaultSpaceID}},
	}
	for _, query := range queries {
		var ids []int64
		if err := gdb.Raw(query.sql, query.args...).Scan(&ids).Error; err != nil {
			t.Fatalf("FR2-007 %s 查询失败: %v", query.name, err)
		}
	}
}

type legacySnapshot map[string]string

func captureLegacySnapshot(t *testing.T, gdb *gorm.DB) legacySnapshot {
	t.Helper()
	queries := map[string]string{
		"users":       "SELECT id, username, password_hash, created_at FROM users ORDER BY id",
		"libraries":   "SELECT id, path, type, label, enabled, created_at FROM library_paths ORDER BY id",
		"media":       "SELECT id, library_id, file_path, file_name, file_size, format, video_codec, audio_codec, duration, width, height, bitrate, subtitle_tracks, added_at, modified_at FROM media_files ORDER BY id",
		"settings":    "SELECT key, value, updated_at FROM settings ORDER BY key",
		"albums":      "SELECT id, name, description, cover_media_id, created_at, updated_at FROM albums ORDER BY id",
		"album_items": "SELECT id, album_id, media_id, added_at FROM album_items ORDER BY id",
		"shares":      "SELECT token, resource_type, resource_id, expires_at, password_hash, max_uses, used_count, created_at FROM shares ORDER BY token",
		"health":      "SELECT id, media_id, issue_type, detail, checked_at FROM media_health_issues ORDER BY id",
		"scan_tasks":  "SELECT id, library_id, scan_type, status, scanned_files, total_files, error, created_at, started_at, completed_at FROM scan_tasks ORDER BY id",
		"transcodes":  "SELECT id, media_id, preset_id, codec, width, height, status, error, created_at, started_at, completed_at FROM transcode_tasks ORDER BY id",
	}
	snapshot := make(legacySnapshot, len(queries))
	for name, query := range queries {
		snapshot[name] = queryRowsAsJSON(t, gdb, query)
	}
	return snapshot
}

func assertLegacySnapshot(t *testing.T, gdb *gorm.DB, want legacySnapshot) {
	t.Helper()
	got := captureLegacySnapshot(t, gdb)
	for name, expected := range want {
		if got[name] != expected {
			t.Fatalf("迁移改变了旧数据 %s:\n迁移前=%s\n迁移后=%s", name, expected, got[name])
		}
	}
}

func queryRowsAsJSON(t *testing.T, gdb *gorm.DB, query string) string {
	t.Helper()
	rows, err := gdb.Raw(query).Rows()
	if err != nil {
		t.Fatalf("读取旧数据快照失败: %v", err)
	}
	defer func() { _ = rows.Close() }()
	result := scanSnapshotRows(t, rows)
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("序列化旧数据快照失败: %v", err)
	}
	return string(raw)
}

func scanSnapshotRows(t *testing.T, rows *sql.Rows) []map[string]any {
	t.Helper()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatalf("读取快照列失败: %v", err)
	}
	var result []map[string]any
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err := rows.Scan(pointers...); err != nil {
			t.Fatalf("扫描旧数据快照失败: %v", err)
		}
		row := make(map[string]any, len(columns))
		for index, column := range columns {
			row[column] = normalizeSQLValue(values[index])
		}
		result = append(result, row)
	}
	return result
}

func normalizeSQLValue(value any) any {
	if bytes, ok := value.([]byte); ok {
		return string(bytes)
	}
	return value
}

func openFR2017Fixture(t *testing.T) (*gorm.DB, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "jianvideo-v020-realistic.sqlite")
	gdb := openSQLitePath(t, dbPath)
	raw, err := os.ReadFile(filepath.Join("testdata", "v020_realistic.sql"))
	if err != nil {
		t.Fatalf("读取真实 v0.20 fixture 失败: %v", err)
	}
	for _, statement := range strings.Split(string(raw), ";") {
		if statement = strings.TrimSpace(statement); statement != "" {
			if err := gdb.Exec(statement).Error; err != nil {
				t.Fatalf("初始化真实 v0.20 fixture 失败: %v\nSQL: %s", err, statement)
			}
		}
	}
	return gdb, dbPath
}

func openSQLitePath(t *testing.T, dbPath string) *gorm.DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开 SQLite 测试库失败: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("读取 SQLite 连接失败: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return gdb
}

func newFR2017DefaultRunner(t *testing.T, gdb *gorm.DB, dbPath string) *Runner {
	t.Helper()
	return NewRunner(gdb, RunnerOptions{
		DBPath: dbPath, BackupDir: filepath.Join(t.TempDir(), "backups"),
		Registry: mustRegistry(t, DefaultMigrations()...), Now: fr2017Now,
	})
}

func mustRegistry(t *testing.T, migrations ...Migration) *Registry {
	t.Helper()
	registry, err := NewRegistry(migrations...)
	if err != nil {
		t.Fatalf("创建迁移 registry 失败: %v", err)
	}
	return registry
}

func copySQLiteFile(t *testing.T, source, target string) {
	t.Helper()
	raw, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("读取迁移备份失败: %v", err)
	}
	if err := os.WriteFile(target, raw, 0o600); err != nil {
		t.Fatalf("恢复迁移备份失败: %v", err)
	}
}

type mediaFileState struct {
	Hash  string
	MTime time.Time
}

func writeMediaFixture(t *testing.T, path string, content []byte, modified time.Time) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("写入原媒体 fixture 失败: %v", err)
	}
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatalf("设置原媒体 mtime 失败: %v", err)
	}
}

func captureMediaFiles(t *testing.T, paths []string) map[string]mediaFileState {
	t.Helper()
	result := make(map[string]mediaFileState, len(paths))
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读取原媒体失败: %v", err)
		}
		stat, err := os.Stat(path)
		if err != nil {
			t.Fatalf("读取原媒体 mtime 失败: %v", err)
		}
		sum := sha256.Sum256(raw)
		result[path] = mediaFileState{Hash: hex.EncodeToString(sum[:]), MTime: stat.ModTime()}
	}
	return result
}

func fr2017Now() time.Time {
	return time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
}
