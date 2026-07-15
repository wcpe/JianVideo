package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/wcpe/JianVideo/internal/storage"
	"github.com/wcpe/JianVideo/internal/transcoder"
)

// negotiateRequest 协商请求体：客户端上报各高级编码的解码能力（来自 FR-52 探测）。
type negotiateRequest struct {
	ClientCaps struct {
		H265 bool `json:"h265"`
		AV1  bool `json:"av1"`
		VP9  bool `json:"vp9"`
	} `json:"client_caps"`
}

// Negotiate POST /api/play/:id/negotiate
// 端到端编码协商（FR-53）：按「首选优先级 ∩ 客户端能力 ∩ 实测可产出」协商实际输出编码与播放路径。
//
//   - h264（含协商不出高级编码的所有情形）→ 现有 mpegts.js + TS 路径（master，分支不动）。
//   - 高级编码（h265/av1/vp9）→ 同步调 PreSliceWithCodec（FR-51）产 fMP4，返回 index.m3u8 描述符；
//     产出失败则降级回 h264/TS（不报错，保证可播）。
//
// 返回播放描述符（编码 + 路径 + 清单 URL + MIME + H.264 回退源），并把实际编码与路径记到会话。
func (h *Handler) Negotiate(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	id, ok := parseMediaID(c)
	if !ok {
		return
	}

	var req negotiateRequest
	// 请求体可缺省（视为无高级编码能力 → 兜底 h264），不强制 JSON
	_ = c.ShouldBindJSON(&req)
	clientCaps := map[string]bool{
		"h265": req.ClientCaps.H265,
		"av1":  req.ClientCaps.AV1,
		"vp9":  req.ClientCaps.VP9,
	}

	mf, err := h.library.GetMediaFileByIDInSpace(spaceID, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
		return
	}

	codec := h.chooseCodec(c.Request.Context(), clientCaps)

	// 非 h264：同步产 fMP4（FR-51）；产出失败降级回 h264（不报错，保证可播）
	if transcoder.IsAdvancedCodec(codec) {
		if !h.produceFMP4(c.Request.Context(), mf.ID, mf.SpaceID, mf.LibraryID, mf.FilePath, mf.Width, mf.Height, codec) {
			log.Printf("[WARN] fMP4 产出失败，降级回 h264: mediaID=%d, codec=%s", mf.ID, codec)
			codec = transcoder.DefaultTargetCodec
		}
	}

	descriptor := transcoder.BuildNegotiationDescriptor(mf.ID, codec)
	if !transcoder.IsAdvancedCodec(codec) {
		presentation := detectFramePresentation(c.Request.Context(), mf.FilePath)
		if presentation != nil {
			descriptor.Path = "mp4"
			descriptor.URL = fmt.Sprintf("/api/play/%d/stream", mf.ID)
			descriptor.FramePresentation = presentation
		}
	}

	// 记录会话实际编码与路径（FR-53）
	if h.playback != nil {
		h.playback.RecordNegotiation(mf.ID, descriptor.Codec, descriptor.Path)
	}

	c.JSON(http.StatusOK, descriptor)
}

// chooseCodec 读 FR-50 优先级 + FR-49 可产出并集 + 客户端能力，协商实际编码。
// 缺 settings / capability 服务时回退默认 h264（无服务环境可用）。
func (h *Handler) chooseCodec(ctx context.Context, clientCaps map[string]bool) string {
	if h.settings == nil || h.capability == nil {
		return transcoder.DefaultTargetCodec
	}
	priority := h.settings.TranscodeCodecPriority()
	producible := make(map[string]bool)
	for _, codec := range h.capability.Capabilities(ctx).Codecs {
		producible[codec] = true
	}
	return transcoder.ChosenCodec(priority, clientCaps, producible)
}

// produceFMP4 为高级编码同步产出 fMP4 分片（FR-51）。
// 缺 hlsDir / hlsMgr、ffmpeg 不可用、SMB 路径或源文件缺失、产出失败均返回 false（调用方降级）。
func (h *Handler) produceFMP4(ctx context.Context, mediaID int64, spaceID string, libraryID int64, filePath string, width, height int, codec string) bool {
	if h.hlsDir == "" || h.hlsMgr == nil || !transcoder.IsFFmpegAvailable() {
		return false
	}
	h.refreshHWAccelSnapshot(ctx)
	if strings.HasPrefix(filePath, "smb://") {
		return false
	}
	if _, err := os.Stat(filePath); err != nil {
		return false
	}
	result, err := transcoder.PreSliceWithCodecAndPolicy(ctx, mediaID, filePath, width, height, codec, h.hardwarePolicy(), h.hlsMgr, h.hlsDir)
	if err != nil {
		log.Printf("[WARN] fMP4 预切片失败: mediaID=%d, codec=%s, err=%v", mediaID, codec, err)
		return false
	}
	if h.cache != nil && result != nil {
		if _, err := h.cache.RegisterDirectory(ctx, storage.RegisterInput{
			SpaceID: spaceID, LibraryID: libraryID, MediaID: mediaID,
			Kind: storage.CacheKindHLS, ProfileID: codec, Path: result.OutputDir,
		}); err != nil {
			log.Printf("[WARN] fMP4 缓存登记失败: mediaID=%d, codec=%s, err=%v", mediaID, codec, err)
		}
	}
	return true
}
