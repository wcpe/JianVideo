package api

import (
	"context"

	"github.com/wcpe/JianVideo/internal/transcoder"
)

// refreshHWAccelSnapshot 从能力缓存刷新进程级选码快照，避免任务只依赖异步预热。
func (h *Handler) refreshHWAccelSnapshot(ctx context.Context) {
	if h.capability != nil {
		h.capability.Capabilities(ctx)
	}
}

// hardwarePolicy 读取运行期硬件转码策略；未注入 settings 时保持历史自动策略。
func (h *Handler) hardwarePolicy() transcoder.HardwarePolicy {
	if h.settings == nil {
		return transcoder.DefaultHardwarePolicy()
	}
	return transcoder.HardwarePolicy{
		Mode:     transcoder.NormalizeHWAccelMode(h.settings.TranscodeHWAccelMode()),
		Fallback: h.settings.TranscodeHWAccelFallback(),
	}
}
