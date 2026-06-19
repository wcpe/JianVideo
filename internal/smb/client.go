package smb

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	smb2 "github.com/cloudsoda/go-smb2"
)

const (
	// defaultTimeout SMB 连接超时时间。
	defaultTimeout = 15 * time.Second
	// maxReconnectAttempts 最大重连次数。
	maxReconnectAttempts = 3
	// reconnectDelay 重连间隔。
	reconnectDelay = 2 * time.Second
)

// Client 封装 SMB 会话，提供连接池和重连能力。
type Client struct {
	creds    Credentials
	dialer   *smb2.Dialer
	mu       sync.Mutex
	session  *smb2.Session
	share    *smb2.Share
	lastUsed time.Time
}

// NewClient 创建 SMB 客户端。
func NewClient(creds Credentials) *Client {
	dialer := &smb2.Dialer{
		Initiator: &smb2.NTLMInitiator{
			User:     creds.Username,
			Password: creds.Password,
		},
	}
	return &Client{
		creds:  creds,
		dialer: dialer,
	}
}

// Connect 建立 SMB 连接并挂载共享。
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.session != nil && c.share != nil {
		return nil
	}

	addr := c.creds.Host
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	var err error
	for attempt := 0; attempt < maxReconnectAttempts; attempt++ {
		c.session, err = c.dialer.Dial(ctx, addr)
		if err == nil {
			break
		}
		log.Printf("[WARN] SMB 连接失败 (第 %d 次): %v", attempt+1, err)
		if attempt < maxReconnectAttempts-1 {
			select {
			case <-time.After(reconnectDelay):
			case <-ctx.Done():
				return fmt.Errorf("SMB 连接被取消: %w", ctx.Err())
			}
		}
	}
	if err != nil {
		return fmt.Errorf("SMB 连接失败（已重试 %d 次）: %w", maxReconnectAttempts, err)
	}

	c.share, err = c.session.Mount(c.creds.Share)
	if err != nil {
		_ = c.session.Logoff()
		c.session = nil
		return fmt.Errorf("挂载共享 %q 失败: %w", c.creds.Share, err)
	}

	c.lastUsed = time.Now()
	log.Printf("[INFO] SMB 连接成功: %s/%s", c.creds.Host, c.creds.Share)
	return nil
}

// Share 返回已挂载的 SMB 共享（实现了 fs.FS）。
func (c *Client) Share() (*smb2.Share, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.share == nil {
		return nil, fmt.Errorf("SMB 尚未连接")
	}
	c.lastUsed = time.Now()
	return c.share, nil
}

// Disconnect 断开 SMB 连接。
func (c *Client) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var err error
	if c.share != nil {
		err = c.share.Umount()
		c.share = nil
	}
	if c.session != nil {
		e := c.session.Logoff()
		if err == nil {
			err = e
		}
		c.session = nil
	}
	return err
}

// EnsureConnected 确保连接可用，必要时重新连接。
func (c *Client) EnsureConnected(ctx context.Context) (*smb2.Share, error) {
	c.mu.Lock()
	needsConnect := c.share == nil
	c.mu.Unlock()

	if needsConnect {
		if err := c.Connect(ctx); err != nil {
			return nil, err
		}
	}
	return c.Share()
}
