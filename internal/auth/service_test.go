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
