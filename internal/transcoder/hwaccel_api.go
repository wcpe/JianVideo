package transcoder

import (
	"encoding/json"
	"net/http"
)

// HWAccelHandler 处理 GET /api/transcode/hwaccel 请求。
// 返回当前环境的硬件加速能力信息，包括可用列表、优先选择编码器等。
// 注意：BuildHWAccelInfo 内部通过 sync.Once 缓存检测结果，首次调用后均为缓存读取，响应极快。
// 若未来改为每次请求实时检测，需在此处添加 context.WithTimeout 超时保护。
func HWAccelHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"code":"METHOD_NOT_ALLOWED","message":"仅支持 GET 方法"}`, http.StatusMethodNotAllowed)
		return
	}

	info := BuildHWAccelInfo()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(info); err != nil {
		http.Error(w, `{"code":"INTERNAL_ERROR","message":"响应序列化失败"}`, http.StatusInternalServerError)
	}
}
