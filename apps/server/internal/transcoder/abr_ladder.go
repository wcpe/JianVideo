package transcoder

import (
	"fmt"
	"strings"
)

var defaultABRVariantNames = []string{"1080p", "720p", "480p"}

// DefaultABRLadderNames 返回默认 H.264 ABR 档位名称副本。
func DefaultABRLadderNames() []string {
	return append([]string(nil), defaultABRVariantNames...)
}

// ABRLadderForSource 按源分辨率裁剪档位；低于最低档时仅保留原尺寸 source 档。
func ABRLadderForSource(width, height int, requested []string) ([]QualityDefinition, error) {
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("源分辨率无效: %dx%d", width, height)
	}
	if len(requested) == 0 {
		requested = defaultABRVariantNames
	}
	definitions := make(map[string]QualityDefinition, len(qualityLadders))
	for _, definition := range qualityLadders {
		definitions[definition.Name] = definition
	}
	ladder := make([]QualityDefinition, 0, len(requested))
	seen := make(map[string]bool, len(requested))
	for _, raw := range requested {
		name := strings.TrimSpace(raw)
		definition, ok := definitions[name]
		if !ok {
			return nil, fmt.Errorf("未知 ABR 档位: %s", name)
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		if definition.Width <= width && definition.Height <= height {
			ladder = append(ladder, definition)
		}
	}
	if len(ladder) > 0 {
		return ladder, nil
	}
	return []QualityDefinition{{
		Name: "source", Width: width, Height: height, VideoRate: "800k", AudioRate: "96k",
	}}, nil
}
