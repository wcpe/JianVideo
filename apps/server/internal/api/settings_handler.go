package api

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/netproxy"
	"github.com/wcpe/JianVideo/internal/settings"
	"github.com/wcpe/JianVideo/internal/transcoder"
)

// GetStorageSettings GET /api/settings/storage
// 返回当前 owner 可查看的 Space、数据目录与索引库路径。
func (h *Handler) GetStorageSettings(c *gin.Context) {
	spaceID := models.DefaultSpaceID
	if value, ok := c.Get("space_id"); ok {
		spaceID, _ = value.(string)
	}
	space, err := h.library.GetSpace(spaceID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"code": "SPACE_NOT_FOUND", "message": "Space 不存在"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询 Space 失败"})
		return
	}
	libraryCount, err := h.library.CountLibraryPathsInSpace(spaceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询媒体库目录失败"})
		return
	}
	dataDir := ""
	if h.dbPath != "" {
		dataDir = filepath.Dir(h.dbPath)
	}
	c.JSON(http.StatusOK, gin.H{
		"space":         space,
		"data_dir":      dataDir,
		"database_path": h.dbPath,
		"library_count": libraryCount,
	})
}

// GetSettings GET /api/settings
// 返回全部运行期设置，形如 {"settings": {"scan_interval": "3600", ...}}。
func (h *Handler) GetSettings(c *gin.Context) {
	if h.settings == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "SETTINGS_UNAVAILABLE", "message": "设置服务未启用"})
		return
	}

	all, err := h.settings.GetAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询设置失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"settings": all})
}

// GetSettingDefinitions GET /api/settings/definitions
// 返回配置注册表，供前端按类型、分层、敏感性和热应用能力渲染设置页。
func (h *Handler) GetSettingDefinitions(c *gin.Context) {
	if h.settings == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "SETTINGS_UNAVAILABLE", "message": "设置服务未启用"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"definitions": settings.Definitions()})
}

// UpdateSettings PUT /api/settings
// 批量写入设置，请求体形如 {"settings": {"scan_interval": "3600", ...}}。
func (h *Handler) UpdateSettings(c *gin.Context) {
	if h.settings == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "SETTINGS_UNAVAILABLE", "message": "设置服务未启用"})
		return
	}

	var req struct {
		Settings map[string]string `json:"settings"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "请求参数错误"})
		return
	}
	if len(req.Settings) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "EMPTY_SETTINGS", "message": "settings 不能为空"})
		return
	}

	wakeInferenceWorker := false
	err := h.settings.SetManyWithHook(c.Request.Context(), req.Settings, func(ctx context.Context, tx settings.TxRepository, before, after map[string]string) error {
		queued, hookErr := h.persistInferenceRefreshForSettings(ctx, tx, before, after)
		wakeInferenceWorker = queued
		return hookErr
	})
	if err != nil {
		if settings.IsValidationError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_SETTING", "message": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": "UPDATE_FAILED", "message": "保存设置失败"})
		return
	}

	// 落库成功后，把 ffmpeg/ffprobe 路径设置应用到转码运行期，保存即生效（FR-56）。
	applyFFmpegPathSettings(req.Settings)

	// 落库成功后，把 magick 路径设置应用到 HEIC/RAW 转换运行期，保存即生效（FR-63）。
	applyMagickPathSettings(req.Settings)

	// 落库成功后，把网络代理设置应用到后端出站 HTTP 运行期，保存即生效（FR-80）。
	if err := applyNetworkProxySettings(req.Settings); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "APPLY_FAILED", "message": "应用网络代理设置失败"})
		return
	}

	// 落库成功后，把调试日志开关应用到 GORM 日志运行期，保存即生效（FR-110）。
	h.applyDebugLogSetting(req.Settings)

	// 通知设置变更，让定时扫描周期等运行期配置即时生效（FR-28）。
	if h.settingsReload != nil {
		h.settingsReload()
	}

	if wakeInferenceWorker {
		h.taskWorkers.Wake()
	}

	// 写入成功后回读返回，便于前端直接刷新状态。
	all, err := h.settings.GetAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询设置失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"settings": all})
}

// persistInferenceRefreshForSettings 在设置事务内处理影视推断开关变更：代次递增 + 缺失回填入队。
// settings 读写经 TxRepository；任务入队经 tasksvc.Tx；space 列表经 library.DBProvider（均由 TxRepository 实现，无 tx.DB() 裸传）。
func (h *Handler) persistInferenceRefreshForSettings(ctx context.Context, tx settings.TxRepository, before, after map[string]string) (bool, error) {
	if !containsInferenceSetting(after) {
		return false, nil
	}
	oldCfg, newCfg, err := inferenceSettingsTransition(ctx, tx, before, after)
	if err != nil || inferenceConfigsEqual(oldCfg, newCfg) {
		return false, err
	}
	newCfg.Generation++
	if err := persistInferenceGeneration(ctx, tx, newCfg.Generation); err != nil {
		return false, err
	}
	libraryIDs := newlyEnabledInferenceLibraries(oldCfg, newCfg)
	if !newCfg.Enabled || (oldCfg.Enabled && len(libraryIDs) == 0) {
		return false, nil
	}
	spaces, err := library.ListSpacesNeedingInferenceBackfill(ctx, tx, newCfg, libraryIDs)
	if err != nil || len(spaces) == 0 {
		return false, err
	}
	if h.tasks == nil || h.taskWorkers == nil {
		return false, errors.New("推断回填任务服务未启用")
	}
	disabled := library.SortedDisabledInferenceLibraries(newCfg.DisabledLibraries)
	for _, spaceID := range spaces {
		payload := inferenceBackfillPayload{
			SpaceID: spaceID, Mode: inferenceBackfillModeMissing, Generation: newCfg.Generation,
			Enabled: true, DisabledLibraries: disabled,
		}
		if _, err := h.enqueueInferenceTask(ctx, tx, payload); err != nil {
			return false, err
		}
	}
	return len(spaces) > 0, nil
}

func containsInferenceSetting(values map[string]string) bool {
	_, enabled := values[settings.KeyMediaInferenceEnabled]
	_, libraries := values[settings.KeyMediaInferenceDisabledLibraries]
	return enabled || libraries
}

func inferenceSettingsTransition(ctx context.Context, tx settings.TxRepository, before, after map[string]string) (library.InferenceConfig, library.InferenceConfig, error) {
	newCfg, err := inferenceConfigTx(ctx, tx)
	if err != nil {
		return library.InferenceConfig{}, library.InferenceConfig{}, err
	}
	oldCfg := newCfg
	if _, ok := after[settings.KeyMediaInferenceEnabled]; ok {
		oldCfg.Enabled = settings.ParseBoolSetting(settingBefore(before, settings.KeyMediaInferenceEnabled, "1"), true)
	}
	if _, ok := after[settings.KeyMediaInferenceDisabledLibraries]; ok {
		oldCfg.DisabledLibraries = library.ParseDisabledInferenceLibraries(settingBefore(before, settings.KeyMediaInferenceDisabledLibraries, "[]"))
	}
	return oldCfg, newCfg, nil
}

func settingBefore(before map[string]string, key, fallback string) string {
	if value, ok := before[key]; ok {
		return value
	}
	return fallback
}

func inferenceConfigTx(ctx context.Context, tx settings.TxRepository) (library.InferenceConfig, error) {
	values, err := tx.GetMany(ctx, []string{
		settings.KeyMediaInferenceEnabled,
		settings.KeyMediaInferenceDisabledLibraries,
		settings.KeyMediaInferenceGeneration,
	})
	if err != nil {
		return library.InferenceConfig{}, err
	}
	return library.InferenceConfig{
		Enabled:           settings.ParseBoolSetting(values[settings.KeyMediaInferenceEnabled], true),
		DisabledLibraries: library.ParseDisabledInferenceLibraries(values[settings.KeyMediaInferenceDisabledLibraries]),
		Generation:        settings.ParseInt64Setting(values[settings.KeyMediaInferenceGeneration]),
	}, nil
}

func inferenceConfigsEqual(a, b library.InferenceConfig) bool {
	if a.Enabled != b.Enabled || len(a.DisabledLibraries) != len(b.DisabledLibraries) {
		return false
	}
	for id := range a.DisabledLibraries {
		if !b.DisabledLibraries[id] {
			return false
		}
	}
	return true
}

func newlyEnabledInferenceLibraries(oldCfg, newCfg library.InferenceConfig) []int64 {
	if !oldCfg.Enabled && newCfg.Enabled {
		return nil
	}
	result := make([]int64, 0)
	for id := range oldCfg.DisabledLibraries {
		if !newCfg.DisabledLibraries[id] {
			result = append(result, id)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func persistInferenceGeneration(ctx context.Context, tx settings.TxRepository, generation int64) error {
	return tx.Upsert(ctx, settings.KeyMediaInferenceGeneration, strconv.FormatInt(generation, 10))
}

func applyFFmpegPathSettings(values map[string]string) {
	if p, ok := values[settings.KeyFFmpegPath]; ok && p != "" {
		transcoder.SetFFmpegPath(p)
		library.SetFFmpegPath(p)
	}
	if p, ok := values[settings.KeyFFprobePath]; ok && p != "" {
		transcoder.SetFFprobePath(p)
		library.SetFFprobePath(p)
	}
}

// applyMagickPathSettings 把本次保存的 magick 路径设置应用到运行期（FR-63）。
// 仿 applyFFmpegPathSettings：仅当 magick_path 出现且非空时覆盖全局路径，与 main.go 启动注入保持一致。
// 空串不覆盖（保留自动发现/捆绑版结果），由 library.SetMagickPath 自身的空值守卫保证。
func applyMagickPathSettings(values map[string]string) {
	if p, ok := values[settings.KeyMagickPath]; ok && p != "" {
		library.SetMagickPath(p)
	}
}

// applyDebugLogSetting 把本次保存的调试日志开关应用到 GORM 日志运行期（FR-110）。
// 仅当 debug_log 键出现时处理：按 "1"/"true"=开、其余=关，调用注入的切级别回调即时生效。
// 未注入回调时仅落库（启动时仍会读取该设置决定初始级别）。
func (h *Handler) applyDebugLogSetting(values map[string]string) {
	if v, ok := values[settings.KeyDebugLog]; ok && h.debugLogApply != nil {
		h.debugLogApply(settings.ParseDebugLog(v))
	}
}

// applyNetworkProxySettings 把本次保存的网络代理设置应用到后端出站 HTTP 运行期（FR-80）。
// 仅当 network_proxy 键出现时处理：空串清空走直连、非空设置代理；
// settings registry 已在写入前拦截非法 URL，这里返回错误作为运行期兜底。
func applyNetworkProxySettings(values map[string]string) error {
	if p, ok := values[settings.KeyNetworkProxy]; ok {
		if err := netproxy.SetProxy(p); err != nil {
			return err
		}
	}
	return nil
}
