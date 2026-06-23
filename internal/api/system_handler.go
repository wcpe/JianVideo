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
		"runtime": h.runtimeInfo(),
	})
}

// runtimeInfo 汇总进程与 Go 运行时信息（FR-60），供系统诊断「运行环境」展示。
// 全部来自标准库（os/runtime），不引外部依赖；系统级总内存/磁盘可用不在此列（需第三方库，范围外）。
func (h *Handler) runtimeInfo() gin.H {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	// 工作目录与可执行文件路径读取失败不致命，退化为空串（不阻塞诊断展示）。
	workDir, _ := os.Getwd()
	executable, _ := os.Executable()

	// 运行时长：未注入启动时刻（零值）时退化为 0，避免返回荒谬的超大值。
	var uptimeSeconds int64
	if !h.startTime.IsZero() {
		uptimeSeconds = int64(time.Since(h.startTime).Seconds())
	}

	return gin.H{
		"pid":            os.Getpid(),
		"work_dir":       workDir,
		"executable":     executable,
		"db_path":        h.dbPath,
		"uptime_seconds": uptimeSeconds,
		"mem_alloc":      mem.Alloc, // 当前堆上已分配且仍在用的字节数
		"mem_sys":        mem.Sys,   // 进程向 OS 申请的总字节数
		"num_gc":         mem.NumGC, // 已完成的 GC 次数
		"gomaxprocs":     runtime.GOMAXPROCS(0),
	}
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

// envMaskSet / envMaskUnset 敏感环境变量的固定掩码（FR-56）：绝不回显明文，仅暴露是否已设置。
const (
	envMaskSet   = "****（已设置）"
	envMaskUnset = "（未设置）"
)

// envMeta 描述一个项目已知环境变量的元数据（键 + 中文用途 + 是否敏感）。
type envMeta struct {
	key         string
	description string
	sensitive   bool
}

// knownEnvKeys 项目已知环境变量清单（FR-56）。涵盖根 config（SERVER_PORT/DB_PATH/JWT_SECRET）
// 与 internal/config（JIANVIDEO_*）两套来源，运行时均生效；测试用的 JIANVIDEO_REALMACHINE 不在此列。
// 敏感项（JWT_SECRET / SMB_MASTER_PASSWORD）只暴露是否已设置，绝不回显明文。
var knownEnvKeys = []envMeta{
	{"JIANVIDEO_SERVER_PORT", "HTTP 服务监听端口，默认 8080", false},
	{"JIANVIDEO_DB_PATH", "SQLite 数据库文件路径，默认 data/jianvideo.db", false},
	{"JIANVIDEO_FFMPEG_PATH", "ffmpeg 可执行文件路径，未设置时回退同目录捆绑版或 PATH", false},
	{"JIANVIDEO_FFPROBE_PATH", "ffprobe 可执行文件路径，未设置时回退同目录捆绑版或 PATH", false},
	{"JIANVIDEO_MAGICK_PATH", "ImageMagick magick 可执行文件路径，用于 HEIC/RAW 转换", false},
	{"JIANVIDEO_DEBUG", "设为 1/true 时启用 gin debug 模式（输出调试日志）", false},
	{"SERVER_PORT", "认证模块读取的 HTTP 端口（根 config），默认 8080", false},
	{"DB_PATH", "认证模块读取的数据库路径（根 config），默认 jianvideo.db", false},
	{"JWT_SECRET", "JWT 签名密钥，未设置时启动随机生成（重启后需重新登录）", true},
	{"SMB_MASTER_PASSWORD", "SMB 凭据加解密主密码，未设置则 SMB 凭据功能不可用", true},
}

// envValue 按敏感标志返回脱敏后的展示值（FR-56）：敏感项只返回掩码，非敏感项返回明文。
func envValue(m envMeta, set bool) string {
	if m.sensitive {
		if set {
			return envMaskSet
		}
		return envMaskUnset
	}
	return os.Getenv(m.key)
}

// SystemEnv GET /api/system/env
// 返回项目已知环境变量清单（FR-56），每项含 key/用途/是否敏感/是否已设置/展示值。
// 敏感项（JWT_SECRET / SMB_MASTER_PASSWORD）只暴露是否已设置，value 为固定掩码，绝不回显明文（安全红线）。
func (h *Handler) SystemEnv(c *gin.Context) {
	type envEntry struct {
		Key         string `json:"key"`
		Description string `json:"description"`
		Sensitive   bool   `json:"sensitive"`
		Set         bool   `json:"set"`
		Value       string `json:"value"`
	}

	entries := make([]envEntry, 0, len(knownEnvKeys))
	for _, m := range knownEnvKeys {
		_, set := os.LookupEnv(m.key)
		entries = append(entries, envEntry{
			Key:         m.key,
			Description: m.description,
			Sensitive:   m.sensitive,
			Set:         set,
			Value:       envValue(m, set),
		})
	}
	c.JSON(http.StatusOK, gin.H{"env": entries})
}

// DetectFFmpeg POST /api/system/ffmpeg/detect
// 检测指定 ffmpeg 路径是否可用（FR-56），请求体 {"path":"..."}（path 可空 = 测当前已配置路径）。
// 仅探测、不改写运行期全局路径，供用户保存前先验路径；返回 {ffmpeg_available, ffmpeg_version}。
func (h *Handler) DetectFFmpeg(c *gin.Context) {
	var req struct {
		Path string `json:"path"`
	}
	// 允许空 body / 缺省 path：绑定失败不视为错误，按测当前路径处理
	_ = c.ShouldBindJSON(&req)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 6*time.Second)
	defer cancel()

	available, version := transcoder.CheckFFmpegPath(ctx, req.Path)
	c.JSON(http.StatusOK, gin.H{
		"ffmpeg_available": available,
		"ffmpeg_version":   version,
	})
}
