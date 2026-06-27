package auth

import (
	"database/sql"
	"errors"
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

func TestChangePassword_Success(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	svc := NewService(d, "test-secret")
	if err := svc.CreateDefaultUser(); err != nil {
		t.Fatalf("播种用户失败: %v", err)
	}

	if err := svc.ChangePassword("admin", "admin", "new-secret"); err != nil {
		t.Fatalf("改密应成功: %v", err)
	}
	// 旧密码登录失败、新密码登录成功
	if _, err := svc.Login("admin", "admin"); err == nil {
		t.Error("改密后旧密码登录应失败")
	}
	if _, err := svc.Login("admin", "new-secret"); err != nil {
		t.Errorf("改密后新密码登录应成功: %v", err)
	}
}

func TestChangePassword_WrongCurrent(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	svc := NewService(d, "test-secret")
	if err := svc.CreateDefaultUser(); err != nil {
		t.Fatalf("播种用户失败: %v", err)
	}

	err := svc.ChangePassword("admin", "wrong-current", "new-secret")
	if !errors.Is(err, ErrCurrentPasswordWrong) {
		t.Errorf("当前密码错误应返回 ErrCurrentPasswordWrong, 得到 %v", err)
	}
	// 密码未被更改：原密码仍可登录
	if _, err := svc.Login("admin", "admin"); err != nil {
		t.Errorf("改密失败后原密码应仍可登录: %v", err)
	}
}

func TestChangePassword_EmptyNew(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	svc := NewService(d, "test-secret")
	if err := svc.CreateDefaultUser(); err != nil {
		t.Fatalf("播种用户失败: %v", err)
	}
	if err := svc.ChangePassword("admin", "admin", ""); err == nil {
		t.Error("新密码为空应被拒绝")
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
