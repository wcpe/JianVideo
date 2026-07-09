package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

// 感知哈希去重端点（FR-70）：扫描计算缺失 dHash + 查询重复组。

// ScanDuplicates POST /api/library/duplicates/scan
// 为缺 dHash 的未软删媒体计算并持久化哈希（缩略图缺失则现场生成），返回本次新算条数。
// 同步执行 + 有界并发；单条失败仅记日志跳过，不影响整体（详见 library.ComputeMissingDHashes）。
func (h *Handler) ScanDuplicates(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	computed, err := h.library.ComputeMissingDHashesInSpace(spaceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DEDUP_SCAN_FAILED", "message": "去重扫描失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"computed": computed})
}

// ListDuplicates GET /api/library/duplicates
// 返回按汉明距离阈值聚类的重复组（每组 ≥2 项、排除软删），供「重复项」页展示与批量清理候选。
func (h *Handler) ListDuplicates(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	groups, err := h.library.FindDuplicateGroupsInSpace(spaceID, h.library.DedupThreshold())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询重复组失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"groups": groups})
}

// BackfillFileHashes POST /api/library/file-hashes/backfill
// 将内容 SHA-256 回填任务入队，由 FR2-037 通用任务中心异步执行。
func (h *Handler) BackfillFileHashes(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	if h.tasks == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "TASKS_UNAVAILABLE", "message": "任务中心未启用"})
		return
	}
	task, err := h.tasks.Enqueue(c.Request.Context(), tasksvc.EnqueueInput{
		Scope:          models.TaskScopeSpace,
		SpaceID:        spaceID,
		Type:           library.TaskTypeFileHashBackfill,
		Priority:       0,
		MaxAttempts:    3,
		IdempotencyKey: fmt.Sprintf("file-hash-backfill:%s", spaceID),
		PayloadJSON:    fmt.Sprintf(`{"space_id":%q}`, spaceID),
		ResourceType:   "library",
		ResourceID:     spaceID,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "TASK_ENQUEUE_FAILED", "message": "内容哈希回填入队失败"})
		return
	}
	h.triggerTaskWorkers()
	c.JSON(http.StatusAccepted, gin.H{"status": "queued", "task_id": strconv.FormatInt(task.ID, 10)})
}

// ListExactDuplicates GET /api/library/duplicates/exact
// 返回指定 Space 内按内容 SHA-256 精确分组的重复媒体。
func (h *Handler) ListExactDuplicates(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	groups, err := h.library.FindExactDuplicateGroupsInSpace(spaceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询精确重复组失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"groups": groups})
}

func (h *Handler) triggerTaskWorkers() {
	if h.taskWorkers == nil {
		return
	}
	go func() {
		if err := h.taskWorkers.RunPending(context.Background()); err != nil {
			log.Printf("[WARN] 触发通用任务 worker 失败: %v", err)
		}
	}()
}
