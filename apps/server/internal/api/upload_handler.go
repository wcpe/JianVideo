package api

import (
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/settings"
)

// uploadMaxBytes 单文件上传大小上限（2 GiB）：防止超大文件耗尽磁盘 / 内存。
const uploadMaxBytes = 2 << 30

// UploadMedia POST /api/library/upload
// 以 multipart/form-data 接收单个图片 / 视频文件，落盘到「上传目标位置」后触发增量扫描入库（FR-149，见 ADR-0051）。
// 表单字段：
//   - file：上传文件（必填）
//   - target_dir：临时指定的落盘目录（可选，缺省取设置默认 upload_target_dir）
//   - naming_rule：临时指定命名规则 original/date（可选，缺省取设置默认 upload_naming_rule）
//
// 落盘目标须在某个已注册启用的本地库目录内（防越权写库外）；仅接受图片 / 视频；处理重名冲突。
// 落盘成功后入队该库增量扫描，由扫描填充 media_files（保持磁盘文件为入库真源）。
func (h *Handler) UploadMedia(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	if h.settings == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "SETTINGS_UNAVAILABLE", "message": "设置服务未启用"})
		return
	}

	// 限制请求体大小，超限读取时报错而非耗尽磁盘
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, uploadMaxBytes)

	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "NO_FILE", "message": "缺少上传文件"})
		return
	}

	// 目标目录与命名规则：表单临时值优先，缺省回退设置默认
	targetDir := strings.TrimSpace(c.PostForm("target_dir"))
	if targetDir == "" {
		targetDir, err = h.settings.Get(settings.KeyUploadTargetDir)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "读取上传目录配置失败"})
			return
		}
	}
	if strings.TrimSpace(targetDir) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "NO_TARGET_DIR", "message": "未指定上传目标位置，且未配置默认上传位置"})
		return
	}

	rule := strings.TrimSpace(c.PostForm("naming_rule"))
	if rule == "" {
		if rule, err = h.settings.Get(settings.KeyUploadNamingRule); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "读取命名规则配置失败"})
			return
		}
	}

	// 解析目标目录归属的本地库（防越权写库外）
	paths, err := h.library.ListLibraryPathsInSpace(spaceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询媒体库失败"})
		return
	}
	lp, baseDir, err := library.ResolveUploadLibrary(paths, targetDir)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "TARGET_NOT_IN_LIBRARY", "message": err.Error()})
		return
	}

	// 仅接受图片 / 视频（按目标库的扩展名策略，含自定义后缀）
	if _, ok := h.library.MediaTypeByPathInSpace(spaceID, lp.ID, fileHeader.Filename); !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": "UNSUPPORTED_TYPE", "message": library.ErrUploadUnsupportedType.Error()})
		return
	}

	// 计算落盘路径并避让重名冲突
	destPath, err := library.BuildUploadPath(baseDir, fileHeader.Filename, rule, time.Now())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_NAME", "message": err.Error()})
		return
	}
	destPath, err = library.ResolveUploadConflict(destPath, fileExists)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "CONFLICT_UNRESOLVED", "message": "无法解决重名冲突"})
		return
	}

	// 流式写盘
	src, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "OPEN_FAILED", "message": "读取上传内容失败"})
		return
	}
	defer func() { _ = src.Close() }()

	if err := library.SaveUploadFile(destPath, src); err != nil {
		log.Printf("[ERROR] 上传落盘失败: dest=%s, err=%v", destPath, err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "SAVE_FAILED", "message": "保存上传文件失败"})
		return
	}
	log.Printf("[INFO] 上传文件已落盘: library=%d, dest=%s", lp.ID, destPath)

	// 落盘成功后触发该库增量扫描入库（磁盘为真源，由扫描填充 media_files）
	taskID := h.triggerLibraryScan(spaceID, lp)

	c.JSON(http.StatusOK, gin.H{
		"status":     "uploaded",
		"library_id": lp.ID,
		"file_path":  destPath,
		"scan_task":  taskID,
	})
}

// triggerLibraryScan 上传落盘后触发目标库增量扫描（FR-149）。
// 注入扫描队列时入队、返回 task_id；未注入时回退直接异步扫描、返回 0。
func (h *Handler) triggerLibraryScan(spaceID string, lp *models.LibraryPath) int64 {
	if h.scanQueue != nil {
		taskID, err := h.scanQueue.EnqueueInSpace(spaceID, lp.ID, lp.Path, lp.Type, library.ScanModeIncremental)
		if err != nil {
			log.Printf("[WARN] 上传后扫描入队失败: library=%d, err=%v", lp.ID, err)
			return 0
		}
		return taskID
	}
	h.library.StartAsyncScanInSpace(spaceID, lp.ID, lp.Path, lp.Type, library.ScanModeIncremental)
	return 0
}

// fileExists 判断磁盘路径是否已存在（供重名冲突避让的占用谓词）。
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil || !errors.Is(err, os.ErrNotExist)
}
