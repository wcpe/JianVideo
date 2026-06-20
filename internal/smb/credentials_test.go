package smb

import (
	"testing"
)

func TestEncryptDecrypt_Roundtrip(t *testing.T) {
	password := []byte("test-master-password")
	plaintext := []byte("hello, 加密世界")

	ciphertext, err := encrypt(password, plaintext)
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}

	decrypted, err := decrypt(password, ciphertext)
	if err != nil {
		t.Fatalf("解密失败: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Fatalf("解密结果不匹配: got %q, want %q", decrypted, plaintext)
	}
}

func TestDecrypt_WrongPassword(t *testing.T) {
	password := []byte("correct-password")
	wrong := []byte("wrong-password")
	plaintext := []byte("secret data")

	ciphertext, err := encrypt(password, plaintext)
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}

	_, err = decrypt(wrong, ciphertext)
	if err == nil {
		t.Fatal("使用错误密码解密应返回错误，但得到了 nil")
	}
}

func TestEncryptDecrypt_EmptyInput(t *testing.T) {
	password := []byte("test-password")
	plaintext := []byte{}

	ciphertext, err := encrypt(password, plaintext)
	if err != nil {
		t.Fatalf("加密空输入失败: %v", err)
	}

	decrypted, err := decrypt(password, ciphertext)
	if err != nil {
		t.Fatalf("解密空输入失败: %v", err)
	}

	if len(decrypted) != 0 {
		t.Fatalf("解密空输入应得到空结果，got %q", decrypted)
	}
}

// 复现 FR-02 凭据无法读回的根因：Save 与 Load 主密码必须一致。
// 修复前各调用点不一致（Save 用 default-master-password / req.MasterPwd，Load 用空串）
// 导致解密必然失败；修复后统一走 MasterPassword()。
func TestCredentials_LoadRequiresSameMasterPassword(t *testing.T) {
	dir := t.TempDir()
	store := NewCredentialStore(dir)
	creds := &Credentials{Host: "nas", Username: "u", Password: "p", Share: "media"}

	// 用统一来源 MasterPassword() 保存（与各调用点修复后一致）
	if err := store.Save(MasterPassword(), creds); err != nil {
		t.Fatalf("保存失败: %v", err)
	}

	// 旧调用点用 Load("") —— 主密码不一致，必然解密失败（复现 bug 根因）
	if _, err := store.Load(""); err == nil {
		t.Fatal("空主密码不应能解密由 MasterPassword() 加密的凭据")
	}

	// 修复后统一用 MasterPassword()，应能正确读回
	got, err := store.Load(MasterPassword())
	if err != nil {
		t.Fatalf("用 MasterPassword() 应能读回凭据: %v", err)
	}
	if got.Host != "nas" || got.Username != "u" || got.Password != "p" {
		t.Fatalf("读回凭据不符: %+v", got)
	}
}
