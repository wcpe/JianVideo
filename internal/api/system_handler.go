package api

import (
	"context"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/wcpe/JianVideo/internal/transcoder"
)

// SystemInfo GET /api/system/info
// 返回系统信息（OS/架构/CPU 数/主机名/Go 版本/应用版本）与 ffmpeg 状态及硬件加速检测结果。
func (h *Handler) SystemInfo(c *gin.Context) {
	hostname, _ := os.Hostname()
	ctx, cancel := context.WithTimeout(c.Request.Context(), 6*time.Second)
	defer cancel()

	c.JSON(http.StatusOK, gin.H{
		"app_version": h.versionOrDefault(),
		"os":          runtime.GOOS,
		"arch":        runtime.GOARCH,
		"num_cpu":     runtime.NumCPU(),
		"hostname":    hostname,
		"go_version":  runtime.Version(),
		"ffmpeg": gin.H{
			"available": transcoder.IsFFmpegAvailable(),
			"path":      transcoder.GetFFmpegPath(),
			"version":   transcoder.FFmpegVersion(ctx),
		},
		"hwaccel": transcoder.BuildHWAccelInfo(),
	})
}

// CodecTest POST /api/system/codec-test
// 对候选编码器逐个用外部 ffmpeg 跑试编码，返回每个编码器是否编入及试编码是否成功。
func (h *Handler) CodecTest(c *gin.Context) {
	if !transcoder.IsFFmpegAvailable() {
		c.JSON(http.StatusOK, gin.H{"ffmpeg_available": false, "results": []transcoder.EncoderProbeResult{}})
		return
	}

	// 整体超时兜底：候选较多且逐个试编码，留足时间但避免无限阻塞
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Minute)
	defer cancel()

	results := transcoder.ProbeEncoders(ctx)
	c.JSON(http.StatusOK, gin.H{"ffmpeg_available": true, "results": results})
}
