package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
)

type watchStateAPIResponse struct {
	Applied bool              `json:"applied"`
	Code    string            `json:"code"`
	Current models.WatchState `json:"current"`
}

func TestWatchStateAPI读取更新幂等与冲突(t *testing.T) {
	router, svc, _ := setupWatchStateAPIRouter(t)
	media := seedWatchStateAPIMedia(t, svc, models.DefaultSpaceID, "state.mp4")
	path := "/api/play/" + strconv.FormatInt(media.ID, 10) + "/watch-state"

	initial := doJSON(t, router, http.MethodGet, path, "")
	if initial.Code != http.StatusOK {
		t.Fatalf("读取初始观看状态应返回 200，实际 %d body=%s", initial.Code, initial.Body.String())
	}
	var initialState models.WatchState
	decodeWatchStateAPI(t, initial, &initialState)
	if initialState.Revision != 0 || initialState.MediaID != media.ID || initialState.PositionSeconds != 0 || initialState.Completed {
		t.Fatalf("初始观看状态错误: %+v", initialState)
	}

	body := `{"position_seconds":25,"expected_revision":0,"session_id":"session-a","event_seq":1,"event_type":"progress","reason":"user"}`
	updated := doJSON(t, router, http.MethodPut, path, body)
	if updated.Code != http.StatusOK {
		t.Fatalf("更新观看状态应返回 200，实际 %d body=%s", updated.Code, updated.Body.String())
	}
	var applied watchStateAPIResponse
	decodeWatchStateAPI(t, updated, &applied)
	if !applied.Applied || applied.Current.Revision != 1 || applied.Current.PositionSeconds != 25 {
		t.Fatalf("更新响应错误: %+v", applied)
	}

	duplicate := doJSON(t, router, http.MethodPut, path, `{"position_seconds":90,"expected_revision":1,"session_id":"session-a","event_seq":1,"event_type":"progress","reason":"user"}`)
	if duplicate.Code != http.StatusOK {
		t.Fatalf("重复事件应返回 200，实际 %d body=%s", duplicate.Code, duplicate.Body.String())
	}
	var ignored watchStateAPIResponse
	decodeWatchStateAPI(t, duplicate, &ignored)
	if ignored.Applied || ignored.Current.Revision != 1 || ignored.Current.PositionSeconds != 25 {
		t.Fatalf("重复事件必须 applied=false 且状态不变: %+v", ignored)
	}

	newer := doJSON(t, router, http.MethodPut, path, `{"position_seconds":40,"expected_revision":1,"session_id":"session-b","event_seq":1,"event_type":"seek","reason":"user"}`)
	if newer.Code != http.StatusOK {
		t.Fatalf("新会话更新应返回 200，实际 %d body=%s", newer.Code, newer.Body.String())
	}

	conflict := doJSON(t, router, http.MethodPut, path, `{"position_seconds":80,"expected_revision":1,"session_id":"session-a","event_seq":2,"event_type":"pause","reason":"system"}`)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("旧 revision 应返回 409，实际 %d body=%s", conflict.Code, conflict.Body.String())
	}
	var conflictBody watchStateAPIResponse
	decodeWatchStateAPI(t, conflict, &conflictBody)
	if conflictBody.Code != "WATCH_STATE_CONFLICT" || conflictBody.Applied || conflictBody.Current.Revision != 2 || conflictBody.Current.PositionSeconds != 40 {
		t.Fatalf("冲突响应必须包含稳定错误码、applied=false 与当前状态: %+v", conflictBody)
	}
}

func TestWatchStateAPI校验Space软删与历史游标(t *testing.T) {
	router, svc, db := setupWatchStateAPIRouter(t)
	first := seedWatchStateAPIMedia(t, svc, models.DefaultSpaceID, "first.mp4")
	second := seedWatchStateAPIMedia(t, svc, models.DefaultSpaceID, "second.mp4")
	other := seedWatchStateAPIMedia(t, svc, "space-alt", "other.mp4")

	applyWatchStateAPIEvent(t, svc, models.DefaultSpaceID, first.ID, 10, "first")
	applyWatchStateAPIEvent(t, svc, models.DefaultSpaceID, second.ID, 20, "second")
	applyWatchStateAPIEvent(t, svc, "space-alt", other.ID, 30, "other")
	fixed := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	if err := db.Model(&models.WatchState{}).Where("media_id IN ?", []int64{first.ID, second.ID}).Update("last_watched_at", fixed).Error; err != nil {
		t.Fatalf("固定历史排序时间失败: %v", err)
	}

	page1 := doJSON(t, router, http.MethodGet, "/api/library/watch-history?limit=1", "")
	if page1.Code != http.StatusOK {
		t.Fatalf("观看历史第一页应返回 200，实际 %d body=%s", page1.Code, page1.Body.String())
	}
	var history struct {
		Items []library.WatchMediaItem `json:"items"`
		Next  string                   `json:"next_cursor"`
	}
	decodeWatchStateAPI(t, page1, &history)
	if len(history.Items) != 1 || history.Items[0].Media.ID != second.ID || history.Items[0].State.PositionSeconds != 20 || history.Next == "" {
		t.Fatalf("观看历史第一页未按真源游标返回: %+v", history)
	}
	page2 := doJSON(t, router, http.MethodGet, "/api/library/watch-history?limit=1&cursor="+history.Next, "")
	var next struct {
		Items []library.WatchMediaItem `json:"items"`
	}
	decodeWatchStateAPI(t, page2, &next)
	if page2.Code != http.StatusOK || len(next.Items) != 1 || next.Items[0].Media.ID != first.ID {
		t.Fatalf("观看历史第二页不稳定: code=%d body=%s", page2.Code, page2.Body.String())
	}

	continued := doJSON(t, router, http.MethodGet, "/api/library/continue-watching?limit=10", "")
	var continuePage struct {
		Items []library.WatchMediaItem `json:"items"`
	}
	decodeWatchStateAPI(t, continued, &continuePage)
	if continued.Code != http.StatusOK || len(continuePage.Items) != 2 || continuePage.Items[0].State.MediaID != second.ID {
		t.Fatalf("继续观看必须返回真源状态: code=%d body=%s", continued.Code, continued.Body.String())
	}

	otherPath := "/api/play/" + strconv.FormatInt(other.ID, 10) + "/watch-state"
	wrongSpace := doJSON(t, router, http.MethodGet, otherPath, "")
	if wrongSpace.Code != http.StatusNotFound {
		t.Fatalf("默认 Space 不得读取其他 Space 状态，实际 %d body=%s", wrongSpace.Code, wrongSpace.Body.String())
	}

	deletedAt := time.Now().UTC()
	if err := db.Model(&models.MediaFile{}).Where("id = ?", second.ID).Update("deleted_at", deletedAt).Error; err != nil {
		t.Fatalf("软删媒体失败: %v", err)
	}
	deleted := doJSON(t, router, http.MethodGet, "/api/play/"+strconv.FormatInt(second.ID, 10)+"/watch-state", "")
	if deleted.Code != http.StatusNotFound {
		t.Fatalf("软删媒体不得读取观看状态，实际 %d body=%s", deleted.Code, deleted.Body.String())
	}
	continued = doJSON(t, router, http.MethodGet, "/api/library/continue-watching?limit=10", "")
	decodeWatchStateAPI(t, continued, &continuePage)
	if len(continuePage.Items) != 1 || continuePage.Items[0].Media.ID != first.ID {
		t.Fatalf("继续观看必须隔离软删媒体: %s", continued.Body.String())
	}
}

func TestWatchStateAPI拒绝无效请求与游标(t *testing.T) {
	router, svc, _ := setupWatchStateAPIRouter(t)
	media := seedWatchStateAPIMedia(t, svc, models.DefaultSpaceID, "invalid.mp4")
	path := "/api/play/" + strconv.FormatInt(media.ID, 10) + "/watch-state"
	invalid := doJSON(t, router, http.MethodPut, path, `{"position_seconds":-1,"expected_revision":0,"session_id":"bad space","event_seq":-1,"event_type":"tick","reason":"retry"}`)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("无效观看事件应返回 400，实际 %d body=%s", invalid.Code, invalid.Body.String())
	}
	missing := doJSON(t, router, http.MethodPut, path, `{"position_seconds":0,"session_id":"session-a","event_type":"progress","reason":"user"}`)
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("缺少 revision 或事件序号应返回 400，实际 %d body=%s", missing.Code, missing.Body.String())
	}
	cursor := doJSON(t, router, http.MethodGet, "/api/library/watch-history?cursor=bad", "")
	if cursor.Code != http.StatusBadRequest {
		t.Fatalf("无效游标应返回 400，实际 %d body=%s", cursor.Code, cursor.Body.String())
	}
}

func setupWatchStateAPIRouter(t *testing.T) (*gin.Engine, *library.Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开观看状态 API 测试库失败: %v", err)
	}
	if sqlDB, sqlErr := db.DB(); sqlErr == nil {
		sqlDB.SetMaxOpenConns(1)
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	if err := db.AutoMigrate(&models.Space{}, &models.LibraryPath{}, &models.MediaFile{}, &models.WatchState{}); err != nil {
		t.Fatalf("迁移观看状态 API 测试表失败: %v", err)
	}
	for _, id := range []string{models.DefaultSpaceID, "space-alt"} {
		if err := db.Create(&models.Space{ID: id, Name: id, OwnerUserID: 1}).Error; err != nil {
			t.Fatalf("创建测试 Space 失败: %v", err)
		}
	}
	svc := library.NewService(db)
	router := gin.New()
	RegisterRoutes(router, NewHandler(svc))
	return router, svc, db
}

func seedWatchStateAPIMedia(t *testing.T, svc *library.Service, spaceID, name string) models.MediaFile {
	t.Helper()
	libraryPath, err := svc.CreateLibraryPathInSpace(spaceID, t.TempDir(), "local", spaceID)
	if err != nil {
		t.Fatalf("创建观看状态 API 测试媒体库失败: %v", err)
	}
	media, err := svc.CreateMediaFileInSpace(spaceID, libraryPath.ID, filepath.ToSlash(filepath.Join("D:/Videos", spaceID, name)), 100)
	if err != nil {
		t.Fatalf("创建观看状态 API 测试媒体失败: %v", err)
	}
	return *media
}

func applyWatchStateAPIEvent(t *testing.T, svc *library.Service, spaceID string, mediaID int64, position float64, sessionID string) {
	t.Helper()
	if _, err := svc.ApplyWatchEventInSpace(spaceID, mediaID, library.WatchEventInput{
		PositionSeconds: position,
		SessionID:       sessionID,
		EventSeq:        1,
		EventType:       library.WatchEventProgress,
		Reason:          library.WatchReasonUser,
	}); err != nil {
		t.Fatalf("写入观看状态 API 测试事件失败: %v", err)
	}
}

func decodeWatchStateAPI(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatalf("解析观看状态 API 响应失败: %v body=%s", err, response.Body.String())
	}
}
