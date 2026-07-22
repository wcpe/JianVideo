package update

import (
	"net/http"
	"testing"
	"time"
)

// TestDownloadClientNoOverallTimeout 验证下载二进制用的 client 不设整体 Timeout，
// 改靠调用方 context 控制——避免慢网络下几十 MB 产物被 client 的 30s 整体超时掐断
// （这是 FR-46 真机验暴露的缺陷：检测与下载共用 30s client，下载大文件超时失败）。
func TestDownloadClientNoOverallTimeout(t *testing.T) {
	s := NewService()
	if s.downloadClient == nil {
		t.Fatal("downloadClient 不应为 nil")
	}
	if s.downloadClient.Timeout != 0 {
		t.Errorf("下载 client 不应设整体 Timeout（应靠 context 控制），实际 %v", s.downloadClient.Timeout)
	}
	if s.client.Timeout != 30*time.Second {
		t.Errorf("检测 client 应保留 30s 超时，实际 %v", s.client.Timeout)
	}
}

// TestDLClientFallback 未注入 downloadClient（如测试以字面量构造 Service）时回退到 client。
func TestDLClientFallback(t *testing.T) {
	c := &http.Client{Timeout: time.Second}
	s := &Service{client: c}
	if s.dlClient() != c {
		t.Error("未注入 downloadClient 时应回退到 client")
	}
	dl := &http.Client{}
	s.downloadClient = dl
	if s.dlClient() != dl {
		t.Error("已注入 downloadClient 时应优先用它")
	}
}
