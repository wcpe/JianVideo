package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
)

func TestChaptersAPI只读Space隔离与软删不可见(t *testing.T) {
	router, db, media := setupChaptersBookmarksRouter(t)
	chapter := models.MediaChapter{ID: "chapter-a", SpaceID: media.SpaceID, MediaID: media.ID, Source: "embedded", SourceIndex: 0, StartMS: 0, EndMS: 5000, Title: "开场", SourceFingerprint: "fingerprint", ParsedAt: time.Now()}
	if err := db.Create(&chapter).Error; err != nil {
		t.Fatalf("创建章节失败: %v", err)
	}

	response := performChaptersBookmarksRequest(router, http.MethodGet, "/api/library/media/"+jsonNumber(media.ID)+"/chapters", media.SpaceID, "")
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"title":"开场"`)) {
		t.Fatalf("章节查询失败: status=%d body=%s", response.Code, response.Body.String())
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		result := performChaptersBookmarksRequest(router, method, "/api/library/media/"+jsonNumber(media.ID)+"/chapters", media.SpaceID, `{}`)
		if result.Code != http.StatusNotFound {
			t.Fatalf("章节写路由不得注册: method=%s status=%d", method, result.Code)
		}
	}
	cross := performChaptersBookmarksRequest(router, http.MethodGet, "/api/library/media/"+jsonNumber(media.ID)+"/chapters", "space-b", "")
	if cross.Code != http.StatusNotFound {
		t.Fatalf("跨 Space 章节应不可见: status=%d body=%s", cross.Code, cross.Body.String())
	}
	now := time.Now()
	if err := db.Model(&models.MediaFile{}).Where("id = ?", media.ID).Update("deleted_at", &now).Error; err != nil {
		t.Fatalf("软删媒体失败: %v", err)
	}
	deleted := performChaptersBookmarksRequest(router, http.MethodGet, "/api/library/media/"+jsonNumber(media.ID)+"/chapters", media.SpaceID, "")
	if deleted.Code != http.StatusNotFound {
		t.Fatalf("软删媒体章节应不可见: status=%d body=%s", deleted.Code, deleted.Body.String())
	}
}

func TestBookmarksAPICRUDRevision冲突返回Current(t *testing.T) {
	router, _, media := setupChaptersBookmarksRouter(t)
	path := "/api/library/media/" + jsonNumber(media.ID) + "/bookmarks"
	createdResponse := performChaptersBookmarksRequest(router, http.MethodPost, path, media.SpaceID, `{"position_ms":1000,"title":"关键论点","note":"稍后复看"}`)
	if createdResponse.Code != http.StatusCreated {
		t.Fatalf("创建书签失败: status=%d body=%s", createdResponse.Code, createdResponse.Body.String())
	}
	var created models.MediaBookmark
	if err := json.Unmarshal(createdResponse.Body.Bytes(), &created); err != nil {
		t.Fatalf("解析创建响应失败: %v", err)
	}
	if created.Revision != 1 || created.ID == "" {
		t.Fatalf("创建响应错误: %+v", created)
	}

	itemPath := path + "/" + created.ID
	updatedResponse := performChaptersBookmarksRequest(router, http.MethodPut, itemPath, media.SpaceID, `{"position_ms":1500,"title":"修正","note":null,"revision":1}`)
	if updatedResponse.Code != http.StatusOK || !bytes.Contains(updatedResponse.Body.Bytes(), []byte(`"revision":2`)) {
		t.Fatalf("更新书签失败: status=%d body=%s", updatedResponse.Code, updatedResponse.Body.String())
	}
	conflict := performChaptersBookmarksRequest(router, http.MethodPut, itemPath, media.SpaceID, `{"position_ms":2000,"title":"旧端覆盖","revision":1}`)
	assertBookmarkConflict(t, conflict, created.ID, 2, false)
	deleted := performChaptersBookmarksRequest(router, http.MethodDelete, itemPath+"?revision=2", media.SpaceID, "")
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("删除书签失败: status=%d body=%s", deleted.Code, deleted.Body.String())
	}
	deletedConflict := performChaptersBookmarksRequest(router, http.MethodDelete, itemPath+"?revision=2", media.SpaceID, "")
	assertBookmarkConflict(t, deletedConflict, "", 0, true)
}

func TestBookmarksAPISpace隔离与软删媒体不可见(t *testing.T) {
	router, db, media := setupChaptersBookmarksRouter(t)
	bookmark := createBookmarkThroughAPI(t, router, media)
	path := "/api/library/media/" + jsonNumber(media.ID) + "/bookmarks"
	itemPath := path + "/" + bookmark.ID

	assertMediaNotFound(t, performChaptersBookmarksRequest(router, http.MethodGet, path, "space-b", ""))
	assertMediaNotFound(t, performChaptersBookmarksRequest(router, http.MethodPut, itemPath, "space-b", `{"position_ms":2000,"title":"越权更新","revision":1}`))
	assertMediaNotFound(t, performChaptersBookmarksRequest(router, http.MethodDelete, itemPath+"?revision=1", "space-b", ""))

	now := time.Now()
	if err := db.Model(&models.MediaFile{}).Where("id = ?", media.ID).Update("deleted_at", &now).Error; err != nil {
		t.Fatalf("软删媒体失败: %v", err)
	}
	assertMediaNotFound(t, performChaptersBookmarksRequest(router, http.MethodGet, path, media.SpaceID, ""))
	assertMediaNotFound(t, performChaptersBookmarksRequest(router, http.MethodPost, path, media.SpaceID, `{"position_ms":2000,"title":"软删后创建"}`))
	assertMediaNotFound(t, performChaptersBookmarksRequest(router, http.MethodPut, itemPath, media.SpaceID, `{"position_ms":2000,"title":"软删后更新","revision":1}`))
	assertMediaNotFound(t, performChaptersBookmarksRequest(router, http.MethodDelete, itemPath+"?revision=1", media.SpaceID, ""))

	var count int64
	if err := db.Model(&models.MediaBookmark{}).Where("id = ?", bookmark.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("媒体软删后书签业务行必须保留: count=%d err=%v", count, err)
	}
}

func setupChaptersBookmarksRouter(t *testing.T) (*gin.Engine, *gorm.DB, models.MediaFile) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.Space{}, &models.LibraryPath{}, &models.MediaFile{}, &models.MediaMetadata{}, &models.MediaChapter{}, &models.MediaBookmark{}, &models.AuditEvent{}); err != nil {
		t.Fatalf("迁移测试表失败: %v", err)
	}
	for _, spaceID := range []string{"space-a", "space-b"} {
		if err := db.Create(&models.Space{ID: spaceID, Name: spaceID, CreatedAt: time.Now(), UpdatedAt: time.Now()}).Error; err != nil {
			t.Fatalf("创建 Space 失败: %v", err)
		}
	}
	media := models.MediaFile{SpaceID: "space-a", LibraryID: 1, FilePath: "chapter.mkv", FileName: "chapter.mkv", Format: "mkv", Duration: 10, AddedAt: time.Now(), ModifiedAt: time.Now()}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("创建媒体失败: %v", err)
	}
	recorder := audit.NewRecorder(db)
	handler := NewHandler(library.NewService(db).WithAudit(recorder)).WithAudit(recorder)
	router := gin.New()
	RegisterRoutes(router, handler)
	return router, db, media
}

func createBookmarkThroughAPI(t *testing.T, router *gin.Engine, media models.MediaFile) models.MediaBookmark {
	t.Helper()
	path := "/api/library/media/" + jsonNumber(media.ID) + "/bookmarks"
	response := performChaptersBookmarksRequest(router, http.MethodPost, path, media.SpaceID, `{"position_ms":1000,"title":"隔离测试"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("创建书签失败: status=%d body=%s", response.Code, response.Body.String())
	}
	var bookmark models.MediaBookmark
	if err := json.Unmarshal(response.Body.Bytes(), &bookmark); err != nil {
		t.Fatalf("解析书签失败: %v", err)
	}
	return bookmark
}

func assertBookmarkConflict(t *testing.T, response *httptest.ResponseRecorder, currentID string, revision int64, deleted bool) {
	t.Helper()
	var payload struct {
		Code    string                `json:"code"`
		Current *models.MediaBookmark `json:"current"`
		Deleted bool                  `json:"deleted"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("解析冲突响应失败: %v", err)
	}
	if response.Code != http.StatusConflict || payload.Code != "BOOKMARK_CONFLICT" || payload.Deleted != deleted {
		t.Fatalf("冲突响应错误: status=%d payload=%+v", response.Code, payload)
	}
	if deleted && payload.Current != nil {
		t.Fatalf("已删除冲突不得返回 current: %+v", payload.Current)
	}
	if !deleted && (payload.Current == nil || payload.Current.ID != currentID || payload.Current.Revision != revision) {
		t.Fatalf("冲突响应缺少服务端 current: %+v", payload.Current)
	}
}

func assertMediaNotFound(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	var payload struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("解析错误响应失败: %v", err)
	}
	if response.Code != http.StatusNotFound || payload.Code != "MEDIA_NOT_FOUND" {
		t.Fatalf("应返回媒体不存在: status=%d body=%s", response.Code, response.Body.String())
	}
}

func performChaptersBookmarksRequest(router *gin.Engine, method, path, spaceID, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(spaceHeader, spaceID)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func jsonNumber(value int64) string {
	data, _ := json.Marshal(value)
	return string(data)
}
