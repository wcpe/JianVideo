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
