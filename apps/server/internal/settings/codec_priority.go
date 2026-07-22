package settings

import (
	"encoding/json"
	"fmt"
	"log"
)

// defaultCodecPriority 未配置 / 配置非法时的默认目标编码优先级。
// 默认仅 H.264，保证运行时默认行为不变（FR-50 红线）。
var defaultCodecPriority = []string{"h264"}

// knownTargetCodecs 管道可参数化输出的目标编码集合（与 transcoder.CodecOutputParams 的已知编码一致）。
// 在此独立定义以避免 settings 反向依赖 transcoder；二者均以 FR-49 codecOrder 为准。
var knownTargetCodecs = map[string]bool{
	"h264": true,
	"h265": true,
	"av1":  true,
	"vp9":  true,
}

// TranscodeCodecPriority 读取「首选目标编码优先级」（FR-50）。
// 未配置 / 空 / 解析失败一律回落默认 ["h264"]，保证默认行为不变。
func (s *Service) TranscodeCodecPriority() []string {
	raw, err := s.Get(KeyTranscodeCodecPriority)
	if err != nil || raw == "" {
		return cloneCodecPriority(defaultCodecPriority)
	}
	var priority []string
	if err := json.Unmarshal([]byte(raw), &priority); err != nil || len(priority) == 0 {
		log.Printf("[WARN] 目标编码优先级配置非法，回落默认 [h264]: raw=%q, err=%v", raw, err)
		return cloneCodecPriority(defaultCodecPriority)
	}
	return priority
}

// SetTranscodeCodecPriority 校验并写入「首选目标编码优先级」（FR-50）。
// allowed 为系统实测可输出编码并集（来自 FR-49 能力 codecs），priority 每项须：
// 非空、为已知目标编码、在 allowed 集内、且不重复；任一不满足整体拒绝不落库。
func (s *Service) SetTranscodeCodecPriority(priority, allowed []string) error {
	if err := validateCodecPriority(priority, allowed); err != nil {
		return err
	}
	raw, err := json.Marshal(priority)
	if err != nil {
		return fmt.Errorf("序列化目标编码优先级失败: %w", err)
	}
	return s.Set(KeyTranscodeCodecPriority, string(raw))
}

// validateCodecPriority 校验目标编码优先级（纯逻辑，无副作用，便于穷举单测）。
func validateCodecPriority(priority, allowed []string) error {
	if len(priority) == 0 {
		return fmt.Errorf("目标编码优先级不能为空")
	}
	allowedSet := make(map[string]bool, len(allowed))
	for _, c := range allowed {
		allowedSet[c] = true
	}
	seen := make(map[string]bool, len(priority))
	for _, codec := range priority {
		if !knownTargetCodecs[codec] {
			return fmt.Errorf("不支持的目标编码: %q", codec)
		}
		if !allowedSet[codec] {
			return fmt.Errorf("目标编码 %q 不在系统实测可输出集内", codec)
		}
		if seen[codec] {
			return fmt.Errorf("目标编码 %q 重复", codec)
		}
		seen[codec] = true
	}
	return nil
}

// cloneCodecPriority 返回切片副本，避免调用方修改默认值。
func cloneCodecPriority(src []string) []string {
	dst := make([]string, len(src))
	copy(dst, src)
	return dst
}
