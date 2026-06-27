package auth

import (
	"database/sql"
	"testing"

	"github.com/wcpe/JianVideo/internal/db"
	"github.com/wcpe/JianVideo/internal/db/models"

	_ "github.com/mattn/go-sqlite3"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := db.InitSchema(d); err != nil {
		t.Fatalf("初始化表结构失败: %v", err)
	}
	return d
}

func TestCreateDefaultUser(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	svc := NewService(d, "test-secret")

	err := svc.CreateDefaultUser()
	if err != nil {
		t.Fatalf("创建默认用户失败: %v", err)
	}

	// 验证用户存在
	exists, err := models.UserExists(d)
	if err != nil {
		t.Fatalf("检查用户存在失败: %v", err)
	}
	if !exists {
		t.Fatal("默认用户应已创建")
	}

	// 验证用户名
	user, err := models.FindUserByUsername(d, "admin")
	if err != nil {
		t.Fatalf("查找用户失败: %v", err)
	}
	if user == nil {
		t.Fatal("admin 用户应存在")
	}

	// 再次调用应幂等
	err = svc.CreateDefaultUser()
	if err != nil {
		t.Fatalf("重复创建默认用户应幂等: %v", err)
	}

	var count int
	d.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if count != 1 {
		t.Errorf("应只有 1 个用户, 得到 %d", count)
	}
}

func TestNeedsSetup(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	svc := NewService(d, "test-secret")

	// 全新库无用户：需要初始化
	need, err := svc.NeedsSetup()
	if err != nil {
		t.Fatalf("NeedsSetup 失败: %v", err)
	}
	if !need {
		t.Fatal("无用户时应需要初始化")
	}

	// 创建用户后：不再需要初始化
	if _, err := svc.Setup("alice", "secret123"); err != nil {
		t.Fatalf("Setup 失败: %v", err)
	}
	need, err = svc.NeedsSetup()
	if err != nil {
		t.Fatalf("NeedsSetup 失败: %v", err)
	}
	if need {
		t.Fatal("已有用户时不应再需要初始化")
	}
}

func TestSetup_CreatesFirstUser(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	svc := NewService(d, "test-secret")
	user, err := svc.Setup("alice", "secret123")
	if err != nil {
		t.Fatalf("Setup 失败: %v", err)
	}
	if user.Username != "alice" {
		t.Errorf("期望用户名 alice, 得到 %q", user.Username)
	}
	// 用初始化设置的凭据可登录
	if _, err := svc.Login("alice", "secret123"); err != nil {
		t.Fatalf("初始化后应能登录: %v", err)
	}
	// 不应存在 admin/admin 默认账户
	admin, _ := models.FindUserByUsername(d, "admin")
	if admin != nil {
		t.Fatal("不应自动创建 admin 默认账户")
	}
}

func TestSetup_RejectsWhenUserExists(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	svc := NewService(d, "test-secret")
	if _, err := svc.Setup("alice", "secret123"); err != nil {
		t.Fatalf("首次 Setup 失败: %v", err)
	}
	// 已有用户后再次初始化应被拒绝（防重复初始化劫持）
	if _, err := svc.Setup("mallory", "evil"); err == nil {
		t.Fatal("已初始化后再次 Setup 应被拒绝")
	}
	var count int
	d.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if count != 1 {
		t.Errorf("应只有 1 个用户, 得到 %d", count)
	}
}

func TestSetup_RejectsEmpty(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	svc := NewService(d, "test-secret")
	if _, err := svc.Setup("", "secret123"); err == nil {
		t.Error("空用户名应被拒绝")
	}
	if _, err := svc.Setup("alice", ""); err == nil {
		t.Error("空密码应被拒绝")
	}
}

func TestLogin_Success(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	svc := NewService(d, "test-secret")
	if err := svc.CreateDefaultUser(); err != nil {
		t.Fatalf("创建默认用户失败: %v", err)
	}

	user, err := svc.Login("admin", "admin")
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	if user.Username != "admin" {
		t.Errorf("期望用户名 admin, 得到 %q", user.Username)
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	svc := NewService(d, "test-secret")
	if err := svc.CreateDefaultUser(); err != nil {
		t.Fatalf("创建默认用户失败: %v", err)
	}

	_, err := svc.Login("admin", "wrong-password")
	if err == nil {
		t.Fatal("错误密码登录应失败")
	}
}

func TestLogin_UserNotFound(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	svc := NewService(d, "test-secret")
	if err := svc.CreateDefaultUser(); err != nil {
		t.Fatalf("创建默认用户失败: %v", err)
	}

	_, err := svc.Login("nonexistent", "admin")
	if err == nil {
		t.Fatal("不存在的用户登录应失败")
	}
}
