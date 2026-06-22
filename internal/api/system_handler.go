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
		"hwaccel": h.hwaccelInfo(ctx),
	})
}

// hwaccelInfo 返回硬件加速能力：注入能力服务时读缓存派生（冷态返回未测，不阻塞），
// 未注入时回退冷态默认（软件兜底），保证 NewHandler(nil) 测试可用。
func (h *Handler) hwaccelInfo(ctx context.Context) *transcoder.HWAccelInfo {
	if h.capability != nil {
		return h.capability.Capabilities(ctx)
	}
	return transcoder.BuildCapabilities(nil)
}

// CodecTest POST /api/system/codec-test
// 对候选编码器逐个用外部 ffmpeg 跑试编码，返回每个编码器是否编入及试编码是否成功。
// 默认读缓存（命中当前 ffmpeg 版本即返回，from_cache=true）；?force=true 强制重测覆盖缓存。
func (h *Handler) CodecTest(c *gin.Context) {
	if !transcoder.IsFFmpegAvailable() {
		c.JSON(http.StatusOK, gin.H{
			"ffmpeg_available": false,
			"results":          []transcoder.EncoderProbeResult{},
			"from_cache":       false,
			"ffmpeg_version":   "",
			"tested_at":        "",
		})
		return
	}

	// 整体超时兜底：候选较多且逐个试编码，留足时间但避免无限阻塞
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Minute)
	defer cancel()

	force := c.Query("force") == "true"

	// 注入能力服务：走缓存（命中即返回、未命中或 force 实测并持久化）
	if h.capability != nil {
		results, fromCache, version, testedAt, err := h.capability.CodecResults(ctx, force)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "CODEC_TEST_FAILED", "message": "编码器实测失败"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"ffmpeg_available": true,
			"results":          results,
			"from_cache":       fromCache,
			"ffmpeg_version":   version,
			"tested_at":        codecTestedAtString(testedAt),
		})
		return
	}

	// 未注入能力服务：直接实测（无缓存），保证无服务测试可用
	results := transcoder.ProbeEncoders(ctx)
	c.JSON(http.StatusOK, gin.H{
		"ffmpeg_available": true,
		"results":          results,
		"from_cache":       false,
		"ffmpeg_version":   transcoder.FFmpegVersion(ctx),
		"tested_at":        codecTestedAtString(time.Now()),
	})
}

// codecTestedAtString 把实测时间格式化为 RFC3339；零值返回空串。
func codecTestedAtString(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
