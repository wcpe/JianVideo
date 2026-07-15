package transcoder

import (
	"fmt"
	"strings"
)

const (
	// HWAccelModeAuto 按现有硬件优先级自动选择可用编码器。
	HWAccelModeAuto = "auto"
	// HWAccelModeSoftware 强制使用软件编码器。
	HWAccelModeSoftware = "software"
)

// HardwarePolicy 描述一次转码使用的硬件策略。
type HardwarePolicy struct {
	Mode     string
	Fallback bool
}

// DefaultHardwarePolicy 返回兼容历史行为的默认策略。
func DefaultHardwarePolicy() HardwarePolicy {
	return HardwarePolicy{Mode: HWAccelModeAuto, Fallback: true}
}

// NormalizeHWAccelMode 归一化用户策略；未知值保守回退 auto。
func NormalizeHWAccelMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case HWAccelModeSoftware:
		return HWAccelModeSoftware
	case "nvenc", "qsv", "amf", "vaapi", "videotoolbox":
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return HWAccelModeAuto
	}
}

// SelectCurrentEncoderForCodecWithPolicy 读取当前实测快照并按策略选择编码器。
func SelectCurrentEncoderForCodecWithPolicy(codec string, policy HardwarePolicy) (encoder, deviceType string, hardware bool, err error) {
	var results []EncoderProbeResult
	if snapshot := probeSnapshot.Load(); snapshot != nil {
		results = *snapshot
	}
	return SelectEncoderForCodecWithPolicy(results, codec, policy)
}

// SelectEncoderForCodecWithPolicy 按用户硬件策略选择编码器。
func SelectEncoderForCodecWithPolicy(results []EncoderProbeResult, codec string, policy HardwarePolicy) (encoder, deviceType string, hardware bool, err error) {
	c := normalizePolicyCodec(codec)
	mode := NormalizeHWAccelMode(policy.Mode)
	if mode == HWAccelModeSoftware {
		return softwareEncoderForCodec(c), "", false, nil
	}
	if mode == HWAccelModeAuto {
		return selectAutoEncoderWithPolicy(results, c)
	}
	return selectSpecificEncoderWithPolicy(results, c, mode, policy.Fallback)
}

func normalizePolicyCodec(codec string) string {
	c := normalizeCodec(codec)
	if _, ok := CodecOutputParams(c); !ok {
		return DefaultTargetCodec
	}
	return c
}

func selectAutoEncoderWithPolicy(results []EncoderProbeResult, codec string) (string, string, bool, error) {
	if enc, dev, ok := SelectEncoderForCodec(results, codec); ok {
		return enc, dev, true, nil
	}
	return softwareEncoderForCodec(codec), "", false, nil
}

func selectSpecificEncoderWithPolicy(results []EncoderProbeResult, codec, family string, fallback bool) (string, string, bool, error) {
	if enc, dev, ok := encoderForFamily(results, codec, family); ok {
		return enc, dev, true, nil
	}
	if fallback {
		return softwareEncoderForCodec(codec), "", false, nil
	}
	return "", "", false, unavailableHardwareError(family)
}

func encoderForFamily(results []EncoderProbeResult, codec, family string) (string, string, bool) {
	for _, r := range results {
		if r.Family == family && r.Codec == codec && r.TestedOK {
			return r.Encoder, familyMetaMap[family].deviceType, true
		}
	}
	return "", "", false
}

func unavailableHardwareError(family string) error {
	name := family
	if meta, ok := familyMetaMap[family]; ok && meta.name != "" {
		name = meta.name
	}
	return fmt.Errorf("指定的硬件编码器 %s 不可用，且已关闭软件回退", name)
}
