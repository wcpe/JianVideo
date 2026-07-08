package settings

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const (
	// LayerStartup 表示启动期固定配置，运行后不可通过设置接口修改。
	LayerStartup = "startup"
	// LayerRuntime 表示运行期可写配置，可通过设置接口保存并热应用。
	LayerRuntime = "runtime"
	// LayerReadonly 表示派生只读配置，只能由系统状态接口展示。
	LayerReadonly = "readonly"

	// ValueString 表示普通字符串设置。
	ValueString = "string"
	// ValueInt 表示整数字符串设置。
	ValueInt = "int"
	// ValueBool 表示布尔字符串设置。
	ValueBool = "bool"
	// ValueJSON 表示 JSON 字符串设置。
	ValueJSON = "json"
	// ValueURL 表示 URL 字符串设置。
	ValueURL = "url"
	// ValuePath 表示路径字符串设置。
	ValuePath = "path"
	// ValueEnum 表示枚举字符串设置。
	ValueEnum = "enum"
)

const sensitiveDisplayValue = "已设置"

// SettingOption 描述枚举型设置的可选值。
type SettingOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Definition 描述一个已登记配置项，是运行期设置的唯一写入契约。
type Definition struct {
	Key          string             `json:"key"`
	Label        string             `json:"label"`
	Description  string             `json:"description"`
	Layer        string             `json:"layer"`
	ValueType    string             `json:"value_type"`
	DefaultValue string             `json:"default_value"`
	Sensitive    bool               `json:"sensitive"`
	HotApply     bool               `json:"hot_apply"`
	Consumer     string             `json:"consumer"`
	Options      []SettingOption    `json:"options,omitempty"`
	Validate     func(string) error `json:"-"`
}

// ValidationError 表示配置契约校验失败，可由 API 映射为 400。
type ValidationError struct {
	Key     string
	Message string
}

func (e ValidationError) Error() string {
	if e.Key == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Key, e.Message)
}

// IsValidationError 判断错误是否为配置校验错误。
func IsValidationError(err error) bool {
	_, ok := err.(ValidationError)
	return ok
}

var registry = []Definition{
	{
		Key: KeyScanInterval, Label: "扫描周期", Description: "定时扫描的间隔秒数，0 或留空表示关闭定时扫描。",
		Layer: LayerRuntime, ValueType: ValueInt, DefaultValue: "0", HotApply: true, Consumer: "library.scheduler",
		Validate: validateNonNegativeInt,
	},
	{
		Key: KeyRecycleBinPaths, Label: "回收站路径", Description: "各盘符对应的回收站目录，保存为 JSON 对象。",
		Layer: LayerRuntime, ValueType: ValueJSON, DefaultValue: "{}", HotApply: true, Consumer: "library.recycle",
		Validate: validateJSONObject,
	},
	{
		Key: KeyUpdateChannel, Label: "更新频道", Description: "自更新检查使用的发布频道。",
		Layer: LayerRuntime, ValueType: ValueEnum, DefaultValue: "stable", HotApply: true, Consumer: "update",
		Options:  []SettingOption{{Value: "stable", Label: "正式版"}, {Value: "prerelease", Label: "测试版"}},
		Validate: validateEnum("", "stable", "prerelease"),
	},
	{
		Key: KeyTranscodeCodecPriority, Label: "转码编码优先级", Description: "按优先顺序排列的目标编码 JSON 数组。",
		Layer: LayerRuntime, ValueType: ValueJSON, DefaultValue: `["h264"]`, HotApply: true, Consumer: "transcoder",
		Validate: validateStringArrayJSON,
	},
	{
		Key: KeyFFmpegPath, Label: "FFmpeg 路径", Description: "ffmpeg 可执行文件路径；留空时按自动发现结果使用。",
		Layer: LayerRuntime, ValueType: ValuePath, DefaultValue: "", HotApply: true, Consumer: "transcoder",
		Validate: validateAny,
	},
	{
		Key: KeyFFprobePath, Label: "FFprobe 路径", Description: "ffprobe 可执行文件路径；留空时按自动发现结果使用。",
		Layer: LayerRuntime, ValueType: ValuePath, DefaultValue: "", HotApply: true, Consumer: "library.transcoder",
		Validate: validateAny,
	},
	{
		Key: KeyMagickPath, Label: "Magick 路径", Description: "ImageMagick magick 可执行文件路径；留空时按自动发现结果使用。",
		Layer: LayerRuntime, ValueType: ValuePath, DefaultValue: "", HotApply: true, Consumer: "library.imageconvert",
		Validate: validateAny,
	},
	{
		Key: KeyNetworkProxy, Label: "网络代理", Description: "后端出站网络代理；支持 http、https、socks5、socks5h，凭据不回显。",
		Layer: LayerRuntime, ValueType: ValueURL, DefaultValue: "", Sensitive: true, HotApply: true, Consumer: "netproxy",
		Validate: validateProxyURL,
	},
	{
		Key: KeyDebugLog, Label: "调试日志", Description: "运行时详细日志开关。",
		Layer: LayerRuntime, ValueType: ValueBool, DefaultValue: "0", HotApply: true, Consumer: "dblog",
		Validate: validateBool,
	},
	{
		Key: KeyUploadTargetDir, Label: "默认上传位置", Description: "Web 上传缺省落盘目录，留空表示上传时必须指定。",
		Layer: LayerRuntime, ValueType: ValuePath, DefaultValue: "", HotApply: true, Consumer: "library.upload",
		Validate: validateAny,
	},
	{
		Key: KeyUploadNamingRule, Label: "上传命名规则", Description: "Web 上传文件的默认归档规则。",
		Layer: LayerRuntime, ValueType: ValueEnum, DefaultValue: "original", HotApply: true, Consumer: "library.upload",
		Options:  []SettingOption{{Value: "original", Label: "保留原样"}, {Value: "date", Label: "按日期归档"}},
		Validate: validateEnum("", "original", "date"),
	},
	{
		Key: KeyOpenTabs, Label: "目录标签", Description: "目录浏览打开标签的持久化快照。",
		Layer: LayerRuntime, ValueType: ValueJSON, DefaultValue: "[]", HotApply: true, Consumer: "browse-tabs",
		Validate: validateJSONArray,
	},
	{
		Key: KeyLastOpenedPath, Label: "上次浏览位置", Description: "目录浏览最后打开的位置。",
		Layer: LayerRuntime, ValueType: ValuePath, DefaultValue: "", HotApply: true, Consumer: "browse-tabs",
		Validate: validateAny,
	},
	{
		Key: KeyTaskWorkerScanConcurrency, Label: "扫描 worker 并发", Description: "通用任务中心扫描类任务并发上限。",
		Layer: LayerRuntime, ValueType: ValueInt, DefaultValue: "1", HotApply: true, Consumer: "tasks.worker",
		Validate: validatePositiveInt,
	},
	{
		Key: KeyTaskWorkerTranscodeConcurrency, Label: "转码 worker 并发", Description: "通用任务中心转码类任务并发上限。",
		Layer: LayerRuntime, ValueType: ValueInt, DefaultValue: "1", HotApply: true, Consumer: "tasks.worker",
		Validate: validatePositiveInt,
	},
	{
		Key: KeyTaskWorkerThumbnailConcurrency, Label: "缩略图 worker 并发", Description: "通用任务中心缩略图类任务并发上限。",
		Layer: LayerRuntime, ValueType: ValueInt, DefaultValue: "4", HotApply: true, Consumer: "tasks.worker",
		Validate: validatePositiveInt,
	},
	{
		Key: KeyTaskWorkerLightConcurrency, Label: "轻任务 worker 并发", Description: "通用任务中心轻量任务并发上限。",
		Layer: LayerRuntime, ValueType: ValueInt, DefaultValue: "2", HotApply: true, Consumer: "tasks.worker",
		Validate: validatePositiveInt,
	},
	{
		Key: "server_port", Label: "监听端口", Description: "服务启动时确定的 HTTP 监听端口，运行期不可修改。",
		Layer: LayerStartup, ValueType: ValueInt, DefaultValue: "", HotApply: false, Consumer: "config",
	},
	{
		Key: "db_path", Label: "数据库路径", Description: "SQLite 数据库文件路径，运行期不可通过设置接口修改。",
		Layer: LayerStartup, ValueType: ValuePath, DefaultValue: "", HotApply: false, Consumer: "config",
	},
	{
		Key: "jwt_secret", Label: "会话密钥", Description: "JWT 签名密钥，只能通过启动环境配置。",
		Layer: LayerStartup, ValueType: ValueString, DefaultValue: "", Sensitive: true, HotApply: false, Consumer: "auth",
	},
}

var registryByKey = buildRegistryMap()

func buildRegistryMap() map[string]Definition {
	result := make(map[string]Definition, len(registry))
	for _, def := range registry {
		result[def.Key] = def
	}
	return result
}

// Definitions 返回配置注册表副本，供 API 与前端渲染使用。
func Definitions() []Definition {
	result := make([]Definition, len(registry))
	copy(result, registry)
	return result
}

func definitionFor(key string) (Definition, bool) {
	def, ok := registryByKey[key]
	return def, ok
}

func validateWritable(key, value string) error {
	def, ok := definitionFor(key)
	if !ok {
		return ValidationError{Key: key, Message: "未知设置项"}
	}
	if def.Layer != LayerRuntime {
		return ValidationError{Key: key, Message: "该设置不能在运行期修改"}
	}
	if def.Validate == nil {
		return nil
	}
	if err := def.Validate(value); err != nil {
		return ValidationError{Key: key, Message: err.Error()}
	}
	return nil
}

func publicValue(def Definition, value string) string {
	if def.Sensitive {
		if strings.TrimSpace(value) == "" {
			return ""
		}
		return sensitiveDisplayValue
	}
	return value
}

func validateAny(string) error {
	return nil
}

func validateNonNegativeInt(value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 0 {
		return fmt.Errorf("必须是非负整数")
	}
	return nil
}

func validatePositiveInt(value string) error {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n <= 0 {
		return fmt.Errorf("必须是正整数")
	}
	return nil
}

func validateBool(value string) error {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", "0", "1", "true", "false":
		return nil
	default:
		return fmt.Errorf("必须是布尔值")
	}
}

func validateEnum(values ...string) func(string) error {
	allowed := make(map[string]bool, len(values))
	for _, value := range values {
		allowed[value] = true
	}
	return func(value string) error {
		if allowed[value] {
			return nil
		}
		return fmt.Errorf("取值不在允许范围内")
	}
}

func validateJSONObject(value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(value), &obj); err != nil {
		return fmt.Errorf("必须是 JSON 对象")
	}
	return nil
}

func validateJSONArray(value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	var arr []any
	if err := json.Unmarshal([]byte(value), &arr); err != nil {
		return fmt.Errorf("必须是 JSON 数组")
	}
	return nil
}

func validateStringArrayJSON(value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	var arr []string
	if err := json.Unmarshal([]byte(value), &arr); err != nil || len(arr) == 0 {
		return fmt.Errorf("必须是非空字符串数组 JSON")
	}
	return nil
}

func validateProxyURL(value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	u, err := url.Parse(value)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("代理地址格式不正确")
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "socks5", "socks5h":
		return nil
	default:
		return fmt.Errorf("代理协议仅支持 http、https、socks5、socks5h")
	}
}
