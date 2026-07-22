package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wcpe/JianVideo/internal/migration"
)

func TestMigrationDryRunEntryOutputsJSONAndNeverStartsServiceOrWrites(t *testing.T) {
	tempDir := t.TempDir()
	binaryPath := buildJianVideoBinary(t, tempDir)
	tests := []struct {
		name        string
		settingKey  string
		wantSuccess bool
		wantWarning bool
		wantBlocker bool
	}{
		{name: "仅警告时成功退出", settingKey: "legacy_theme", wantSuccess: true, wantWarning: true},
		{name: "存在阻断项时非零退出", settingKey: "legacy_master_password", wantBlocker: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "jianvideo-v020.sqlite")
			initializeV020Database(t, dbPath)
			insertDryRunSetting(t, dbPath, test.settingKey)
			prepareDryRunDeleteJournal(t, dbPath)
			beforeBusiness := captureDryRunBusinessCounts(t, dbPath)
			beforeStorage := captureDryRunStorageState(t, dbPath)
			port := reserveTCPPort(t)

			plan, stderr, err := runMigrationDryRunBinary(t, binaryPath, dbPath, port)
			if test.wantSuccess && err != nil {
				t.Fatalf("仅警告的 dry-run 应成功退出: %v\n%s", err, stderr)
			}
			if !test.wantSuccess && dryRunExitCode(err) == 0 {
				t.Fatalf("存在阻断项的 dry-run 应非零退出\n%s", stderr)
			}
			if test.wantWarning && len(plan.Warnings) == 0 {
				t.Fatalf("dry-run JSON 应包含警告: %+v", plan)
			}
			if test.wantBlocker && len(plan.Blockers) == 0 {
				t.Fatalf("dry-run JSON 应包含阻断项: %+v", plan)
			}
			if strings.Contains(stderr, "JianVideo 启动于") {
				t.Fatalf("dry-run 不得启动 HTTP 服务: %s", stderr)
			}
			assertTCPPortAvailable(t, port)
			assertDryRunStorageUnchanged(t, dbPath, beforeStorage)
			assertDryRunDatabaseUnchanged(t, dbPath, beforeBusiness)
		})
	}
}

func TestMigrationDryRunMissingDatabaseFailsWithoutCreatingFiles(t *testing.T) {
	tempDir := t.TempDir()
	binaryPath := buildJianVideoBinary(t, tempDir)
	dbPath := filepath.Join(tempDir, "不存在 # 100% 数据库.sqlite")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binaryPath, "-migration-dry-run")
	command.Env = append(os.Environ(),
		"DB_PATH="+dbPath,
		"JWT_SECRET=fr2-017-dry-run-secret",
	)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err == nil {
		t.Fatalf("不存在数据库的 dry-run 应非零退出: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("不存在数据库的 dry-run 未及时退出: %s", stderr.String())
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("dry-run 不得创建不存在的数据库: %v", err)
	}
	assertNoDryRunWALArtifacts(t, dbPath)
}

func runMigrationDryRunBinary(t *testing.T, binaryPath, dbPath string, port int) (migration.Plan, string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binaryPath, "-migration-dry-run")
	command.Env = append(os.Environ(),
		"DB_PATH="+dbPath,
		"SERVER_PORT="+strconv.Itoa(port),
		"JWT_SECRET=fr2-017-dry-run-secret",
	)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("dry-run 未及时退出，可能启动了常驻服务\n%s", stderr.String())
	}
	var plan migration.Plan
	if decodeErr := json.Unmarshal(stdout.Bytes(), &plan); decodeErr != nil {
		t.Fatalf("dry-run stdout 不是有效 JSON: %v\nstdout=%s\nstderr=%s", decodeErr, stdout.String(), stderr.String())
	}
	return plan, stderr.String(), err
}

func insertDryRunSetting(t *testing.T, dbPath, key string) {
	t.Helper()
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("打开 dry-run fixture 失败: %v", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(
		"INSERT INTO settings(key, value, updated_at) VALUES (?, ?, ?)",
		key, "测试值", "2026-07-20T12:00:00Z",
	); err != nil {
		t.Fatalf("写入 dry-run 预检设置失败: %v", err)
	}
}

type dryRunStorageState struct {
	hash        [sha256.Size]byte
	modifiedAt  time.Time
	journalMode string
}

func prepareDryRunDeleteJournal(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("打开 DELETE journal fixture 失败: %v", err)
	}
	defer func() { _ = db.Close() }()
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode=DELETE").Scan(&mode); err != nil {
		t.Fatalf("设置 DELETE journal 失败: %v", err)
	}
	if !strings.EqualFold(mode, "delete") {
		t.Fatalf("fixture journal_mode 不正确: %s", mode)
	}
}

func captureDryRunStorageState(t *testing.T, dbPath string) dryRunStorageState {
	t.Helper()
	raw, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("读取 dry-run 数据库文件失败: %v", err)
	}
	stat, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("读取 dry-run 数据库状态失败: %v", err)
	}
	assertNoDryRunWALArtifacts(t, dbPath)
	return dryRunStorageState{
		hash:        sha256.Sum256(raw),
		modifiedAt:  stat.ModTime(),
		journalMode: readDryRunJournalMode(t, dbPath),
	}
}

func readDryRunJournalMode(t *testing.T, dbPath string) string {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(dbPath)+"?mode=ro")
	if err != nil {
		t.Fatalf("只读打开 dry-run 数据库失败: %v", err)
	}
	defer func() { _ = db.Close() }()
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("读取 dry-run journal_mode 失败: %v", err)
	}
	return strings.ToLower(mode)
}

func assertNoDryRunWALArtifacts(t *testing.T, dbPath string) {
	t.Helper()
	for _, suffix := range []string{"-wal", "-shm"} {
		path := dbPath + suffix
		if _, err := os.Stat(path); err == nil {
			t.Fatalf("dry-run 不得创建 SQLite 文件: %s", path)
		} else if !os.IsNotExist(err) {
			t.Fatalf("检查 SQLite 文件失败 %s: %v", path, err)
		}
	}
}

func assertDryRunStorageUnchanged(t *testing.T, dbPath string, before dryRunStorageState) {
	t.Helper()
	after := captureDryRunStorageState(t, dbPath)
	if after.journalMode != before.journalMode || after.journalMode != "delete" {
		t.Fatalf("dry-run 不得切换 journal_mode: before=%s after=%s", before.journalMode, after.journalMode)
	}
	if after.hash != before.hash {
		t.Fatal("dry-run 不得修改数据库文件内容")
	}
	if !after.modifiedAt.Equal(before.modifiedAt) {
		t.Fatalf("dry-run 不得修改数据库文件时间: before=%s after=%s", before.modifiedAt, after.modifiedAt)
	}
}

func captureDryRunBusinessCounts(t *testing.T, dbPath string) map[string]int {
	t.Helper()
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("打开 dry-run 计数数据库失败: %v", err)
	}
	defer func() { _ = db.Close() }()
	counts := make(map[string]int)
	for _, table := range []string{"users", "settings", "library_paths", "media_files", "albums", "shares"} {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Fatalf("读取 dry-run 前业务表 %s 失败: %v", table, err)
		}
		counts[table] = count
	}
	return counts
}

func assertDryRunDatabaseUnchanged(t *testing.T, dbPath string, before map[string]int) {
	t.Helper()
	after := captureDryRunBusinessCounts(t, dbPath)
	for table, want := range before {
		if after[table] != want {
			t.Fatalf("dry-run 不得写业务表 %s: got=%d want=%d", table, after[table], want)
		}
	}
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("打开 dry-run 结果数据库失败: %v", err)
	}
	defer func() { _ = db.Close() }()
	for _, table := range []string{"schema_migrations", "audit_events"} {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&count); err != nil {
			t.Fatalf("检查 dry-run 元数据表失败: %v", err)
		}
		if count != 0 {
			t.Fatalf("dry-run 不得创建 %s", table)
		}
	}
	backups, err := filepath.Glob(filepath.Join(filepath.Dir(dbPath), "backups", "*.sqlite"))
	if err != nil {
		t.Fatalf("检查 dry-run 备份失败: %v", err)
	}
	if len(backups) != 0 {
		t.Fatalf("dry-run 不得创建迁移备份: %v", backups)
	}
}

func assertTCPPortAvailable(t *testing.T, port int) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatalf("dry-run 退出后端口仍被占用: %v", err)
	}
	_ = listener.Close()
}

func dryRunExitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return -1
}
