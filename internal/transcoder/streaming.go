package transcoder

import (
	"context"
	"io"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"jianvideo/internal/library"
)

// Transcoder 定义转码器接口。
type Transcoder interface {
	Run(ctx context.Context, inputPath string, dst io.Writer) error
}

// StreamHandler 处理流式播放的 HTTP 请求。
type StreamHandler struct {
	library         *library.Service
	pipelineFactory func() Transcoder
}

// NewStreamHandler 创建流式播放处理器。
func NewStreamHandler(lib *library.Service) *StreamHandler {
	return &StreamHandler{
		library: lib,
		pipelineFactory: func() Transcoder {
			return NewPipeline()
		},
	}
}

// NewStreamHandlerWithFactory 创建处理器（注入管道工厂，用于测试）。
func NewStreamHandlerWithFactory(lib *library.Service, factory func() Transcoder) *StreamHandler {
	return &StreamHandler{
		library:         lib,
		pipelineFactory: factory,
	}
}

// StreamMedia GET /api/play/stream/:id
// 将指定媒体文件通过 ffmpeg 实时转码为 MPEG-TS 裸流输出。
func (h *StreamHandler) StreamMedia(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
		return
	}

	// 查询媒体文件
	mf, err := h.library.GetMediaFileByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
		return
	}

	// 设置流式响应头
	c.Header("Content-Type", "video/MP2T")
	c.Header("Transfer-Encoding", "chunked")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	// 创建带超时的 context（防止 ffmpeg 僵死导致连接永不关闭）
	ctx, cancel := context.WithTimeout(c.Request.Context(), 4*time.Hour)
	defer cancel()

	// 创建 pipe：ffmpeg stdout → pipe reader → ResponseWriter
	pipeReader, pipeWriter := io.Pipe()

	// 启动转码 goroutine
	var runErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		tc := h.pipelineFactory()
		runErr = tc.Run(ctx, mf.FilePath, pipeWriter)
		pipeWriter.Close()
	}()

	// 将 pipe 数据流式写入响应（使用 CopyBuffer 批量传输，提升吞吐量）
	buf := make([]byte, 32*1024)
	if _, copyErr := io.CopyBuffer(c.Writer, pipeReader, buf); copyErr != nil {
		// 客户端断开，取消 context 以通知转码 goroutine 退出
		cancel()
	}
	// 确保退出写循环后 context 已取消，防止转码 goroutine 泄露
	cancel()

	// 等待转码 goroutine 结束，检查是否有错误
	wg.Wait()
	if runErr != nil && runErr != context.Canceled && runErr != io.EOF {
		log.Printf("[WARN] 转码异常: media_id=%d, err=%v", id, runErr)
	}
}
