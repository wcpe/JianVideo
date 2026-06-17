package transcoder

import (
	"context"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"jianvideo/internal/library"
)

// StreamHandler 处理流式播放的 HTTP 请求。
type StreamHandler struct {
	library  *library.Service
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
	errCh := make(chan error, 1)
	go func() {
		tc := h.pipelineFactory()
		errCh <- tc.Run(ctx, mf.FilePath, pipeWriter)
		pipeWriter.Close()
	}()

	// 将 pipe 数据流式写入响应（手动 flush 以兼容测试环境）
	buf := make([]byte, 32*1024)
	for {
		n, err := pipeReader.Read(buf)
		if n > 0 {
			c.Writer.Write(buf[:n])
			c.Writer.Flush()
		}
		if err != nil {
			break
		}
	}

	// 检查转码是否有错误
	if err := <-errCh; err != nil && err != context.Canceled && err != io.EOF {
		log.Printf("[WARN] 转码异常: media_id=%d, err=%v", id, err)
	}
}
