package main

import (
	"database/sql"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestSingleBinaryStartsAfterUpgradingRealV020Database(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "jianvideo-v020.sqlite")
	initializeV020Database(t, dbPath)
	binaryPath := buildJianVideoBinary(t, tempDir)
	port := reserveTCPPort(t)
	command, logFile, logPath := newAcceptanceCommand(t, binaryPath, dbPath, tempDir, port)
	waitDone := startAcceptanceCommand(t, command, logFile)

	waitForBinaryStartup(t, port, waitDone, logPath)
	assertSingleBinaryMigration(t, dbPath)
	assertSingleBinaryBackup(t, tempDir)
}

func newAcceptanceCommand(t *testing.T, binaryPath, dbPath, tempDir string, port int) (*exec.Cmd, *os.File, string) {
	t.Helper()
	logPath := filepath.Join(tempDir, "startup.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("创建启动日志文件失败: %v", err)
	}
	command := exec.Command(binaryPath)
	command.Env = append(os.Environ(),
		"DB_PATH="+dbPath, "SERVER_PORT="+strconv.Itoa(port),
		"JWT_SECRET=fr2-017-acceptance-secret",
		"JIANVIDEO_FFMPEG_PATH="+filepath.Join(tempDir, "missing-ffmpeg"),
		"JIANVIDEO_FFPROBE_PATH="+filepath.Join(tempDir, "missing-ffprobe"),
		"JIANVIDEO_MAGICK_PATH="+filepath.Join(tempDir, "missing-magick"),
	)
	command.Stdout, command.Stderr = logFile, logFile
	return command, logFile, logPath
}

func startAcceptanceCommand(t *testing.T, command *exec.Cmd, logFile *os.File) <-chan error {
	t.Helper()
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		t.Fatalf("启动单二进制失败: %v", err)
	}
	waitDone := make(chan error, 1)
	go func() { waitDone <- command.Wait() }()
	t.Cleanup(func() {
		if command.ProcessState == nil && command.Process != nil {
			_ = command.Process.Kill()
			select {
			case <-waitDone:
			case <-time.After(5 * time.Second):
			}
		}
		_ = logFile.Close()
	})
	return waitDone
}

func assertSingleBinaryBackup(t *testing.T, tempDir string) {
	t.Helper()
	backups, err := filepath.Glob(filepath.Join(tempDir, "backups", "*-before-v2-*.sqlite"))
	if err != nil {
		t.Fatalf("查找单二进制迁移备份失败: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("单二进制启动应创建一个迁移前备份: %v", backups)
	}
	assertSQLiteIntegrity(t, backups[0])
}

func initializeV020Database(t *testing.T, dbPath string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("internal", "migration", "testdata", "v020_realistic.sql"))
	if err != nil {
		t.Fatalf("读取真实 v0.20 fixture 失败: %v", err)
	}
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("创建真实 v0.20 SQLite 失败: %v", err)
	}
	defer func() { _ = db.Close() }()
	for _, statement := range strings.Split(string(raw), ";") {
		if statement = strings.TrimSpace(statement); statement != "" {
			if _, err := db.Exec(statement); err != nil {
				t.Fatalf("初始化真实 v0.20 SQLite 失败: %v\nSQL: %s", err, statement)
			}
		}
	}
}

func buildJianVideoBinary(t *testing.T, tempDir string) string {
	t.Helper()
	name := "jianvideo-fr2-017"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binaryPath := filepath.Join(tempDir, name)
	command := exec.Command("go", "build", "-o", binaryPath, ".")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("构建单二进制失败: %v\n%s", err, output)
	}
	return binaryPath
}

func reserveTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("申请启动测试端口失败: %v", err)
	}
	defer func() { _ = listener.Close() }()
	return listener.Addr().(*net.TCPAddr).Port
}

func waitForBinaryStartup(t *testing.T, port int, waitDone <-chan error, logPath string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	address := fmt.Sprintf("127.0.0.1:%d", port)
	for time.Now().Before(deadline) {
		select {
		case err := <-waitDone:
			t.Fatalf("单二进制在监听前退出: %v\n%s", err, readStartupLog(t, logPath))
		default:
		}
		connection, err := net.DialTimeout("tcp", address, 200*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("单二进制未在期限内监听 %s\n%s", address, readStartupLog(t, logPath))
}

func assertSingleBinaryMigration(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("打开单二进制迁移库失败: %v", err)
	}
	defer func() { _ = db.Close() }()
	assertSQLCount(t, db, "SELECT COUNT(*) FROM users", 2)
	assertSQLCount(t, db, "SELECT COUNT(*) FROM library_paths", 3)
	assertSQLCount(t, db, "SELECT COUNT(*) FROM media_files", 4)
	assertSQLCount(t, db, "SELECT COUNT(*) FROM settings", 4)
	assertSQLCount(t, db, "SELECT COUNT(*) FROM tasks WHERE idempotency_key LIKE 'scan:%' OR idempotency_key LIKE 'transcode:%'", 8)
	assertSQLCount(t, db, "SELECT COUNT(*) FROM schema_migrations WHERE status != 'succeeded'", 0)
	assertSQLCount(t, db, "SELECT COUNT(*) FROM albums WHERE space_id IS NULL OR space_id = ''", 0)
	assertSQLCount(t, db, "SELECT COUNT(*) FROM shares WHERE space_id IS NULL OR space_id = ''", 0)
	assertSQLCount(t, db, "SELECT COUNT(*) FROM media_health_issues WHERE space_id IS NULL OR space_id = ''", 0)
	var value string
	if err := db.QueryRow("SELECT value FROM settings WHERE key = 'scan_interval'").Scan(&value); err != nil {
		t.Fatalf("读取迁移后旧设置失败: %v", err)
	}
	if value != "7200" {
		t.Fatalf("单二进制迁移改变了旧设置: %s", value)
	}
}

func assertSQLCount(t *testing.T, db *sql.DB, query string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(query).Scan(&got); err != nil {
		t.Fatalf("执行单二进制验收查询失败: %v\nSQL: %s", err, query)
	}
	if got != want {
		t.Fatalf("单二进制验收计数不正确: got=%d want=%d\nSQL: %s", got, want, query)
	}
}

func assertSQLiteIntegrity(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("打开单二进制备份失败: %v", err)
	}
	defer func() { _ = db.Close() }()
	var result string
	if err := db.QueryRow("PRAGMA integrity_check").Scan(&result); err != nil {
		t.Fatalf("校验单二进制备份失败: %v", err)
	}
	if result != "ok" {
		t.Fatalf("单二进制备份完整性异常: %s", result)
	}
}

func readStartupLog(t *testing.T, logPath string) string {
	t.Helper()
	raw, err := os.ReadFile(logPath)
	if err != nil {
		return fmt.Sprintf("读取启动日志失败: %v", err)
	}
	return string(raw)
}
