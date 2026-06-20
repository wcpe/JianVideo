package smb

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/crypto/pbkdf2"
)

// Credentials 存储 SMB 连接凭据。
// Password 需随凭据一起持久化到加密文件（AES-256-GCM + 0600 权限），故参与序列化；
// 本结构仅在 smb 包内加解密使用，从不直接返回给 HTTP 响应，不存在明文泄露风险。
type Credentials struct {
	Host     string `json:"host"`
	Username string `json:"username"`
	Password string `json:"password"`
	Share    string `json:"share"`
	Domain   string `json:"domain"`
}

// CredentialStore 加密存储 SMB 凭据到本地文件。
type CredentialStore struct {
	dataDir string
}

// NewCredentialStore 创建凭据存储器。
func NewCredentialStore(dataDir string) *CredentialStore {
	return &CredentialStore{dataDir: dataDir}
}

// MasterPassword 返回加解密 SMB 凭据用的主密码：优先环境变量 SMB_MASTER_PASSWORD，
// 未设置时回退默认值（仅开发用，生产应设置环境变量）。
// 所有 Save/Load 调用点必须使用此同一来源——否则加密与解密主密码不一致，凭据无法读回。
func MasterPassword() string {
	if p := os.Getenv("SMB_MASTER_PASSWORD"); p != "" {
		return p
	}
	return "default-master-password"
}

// Save 使用 AES-GCM 加密并保存凭据。
func (cs *CredentialStore) Save(masterPassword string, creds *Credentials) error {
	data, err := json.Marshal(creds)
	if err != nil {
		return fmt.Errorf("序列化凭据失败: %w", err)
	}

	encrypted, err := encrypt([]byte(masterPassword), data)
	if err != nil {
		return fmt.Errorf("加密凭据失败: %w", err)
	}

	path := filepath.Join(cs.dataDir, "smb_credentials.enc")
	return os.WriteFile(path, encrypted, 0o600)
}

// Load 读取并解密凭据文件。
func (cs *CredentialStore) Load(masterPassword string) (*Credentials, error) {
	path := filepath.Join(cs.dataDir, "smb_credentials.enc")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取凭据文件失败: %w", err)
	}

	decrypted, err := decrypt([]byte(masterPassword), data)
	if err != nil {
		return nil, fmt.Errorf("解密凭据失败（主密码错误）: %w", err)
	}

	var creds Credentials
	if err := json.Unmarshal(decrypted, &creds); err != nil {
		return nil, fmt.Errorf("解析凭据失败: %w", err)
	}
	return &creds, nil
}

// Exists 检查凭据文件是否存在。
func (cs *CredentialStore) Exists() bool {
	path := filepath.Join(cs.dataDir, "smb_credentials.enc")
	_, err := os.Stat(path)
	return err == nil
}

// deriveKey 使用 PBKDF2 从主密码派生 AES-256 密钥。
func deriveKey(password []byte, salt []byte) []byte {
	return pbkdf2.Key(password, salt, 100_000, 32, sha256.New)
}

// 加密参数常量。
const (
	saltLen  = 16
	nonceLen = 12 // GCM 标准 nonce 长度
)

// encrypt 使用 AES-256-GCM 加密明文。
// 输出格式: salt(16) || nonce(12) || ciphertext+tag。
func encrypt(password, plaintext []byte) ([]byte, error) {
	salt := make([]byte, saltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, err
	}

	key := deriveKey(password, salt)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	// 输出: salt || nonce || ciphertext+tag
	return append(salt, ciphertext...), nil
}

// decrypt 使用 AES-256-GCM 解密密文。
func decrypt(password, data []byte) ([]byte, error) {
	if len(data) < saltLen {
		return nil, fmt.Errorf("密文太短")
	}

	salt := data[:saltLen]
	remaining := data[saltLen:]

	key := deriveKey(password, salt)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	if len(remaining) < nonceLen {
		return nil, fmt.Errorf("密文格式错误")
	}

	nonce := remaining[:nonceLen]
	ciphertext := remaining[nonceLen:]

	return gcm.Open(nil, nonce, ciphertext, nil)
}
