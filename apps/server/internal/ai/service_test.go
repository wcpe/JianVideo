package ai_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/wcpe/JianVideo/internal/ai"
	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/settings"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

func openAITestDB(t *testing.T) *gorm.DB {
	t.Helper()
	// 每测独立内存库，避免 cache=shared 串数据
	dsn := "file:ai-" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.AIModel{}, &models.AIInferenceNode{}, &models.AIResult{}, &models.AIEmbedding{}, &models.Task{}, &models.Setting{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestEnsureReady_DefaultDisabled(t *testing.T) {
	db := openAITestDB(t)
	settingsSvc := settings.NewService(db)
	svc := ai.NewService(ai.NewGormRepository(db), settingsSvc)
	if err := svc.EnsureReady(context.Background()); err != ai.ErrAIDisabled {
		t.Fatalf("期望 AI_DISABLED，got %v", err)
	}
}

func TestEnqueueAndHandleInfer_WithStub(t *testing.T) {
	db := openAITestDB(t)
	settingsSvc := settings.NewService(db)
	if err := settingsSvc.Set(settings.KeyAIEnabled, "1"); err != nil {
		// Set 可能需要注册表校验；直接写 repo
		_ = db.Exec(`INSERT INTO settings(key, value) VALUES(?, ?)`, settings.KeyAIEnabled, "1").Error
	}
	// 确保设置为 1
	_ = db.Exec(`INSERT OR REPLACE INTO settings(key, value) VALUES(?, ?)`, settings.KeyAIEnabled, "1").Error

	repo := ai.NewGormRepository(db)
	taskSvc := tasksvc.NewService(db)
	svc := ai.NewService(repo, settingsSvc).WithTasks(taskSvc)
	if err := svc.SeedStubFixture(context.Background()); err != nil {
		t.Fatalf("seed: %v", err)
	}

	task, err := svc.EnqueueInfer(context.Background(), "space-default", 42, models.AITaskTypeOCR, "", "", "tester")
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if task.ID == 0 || task.Type != ai.TaskTypeAIInfer {
		t.Fatalf("task 异常: %+v", task)
	}

	// 领取并执行
	claimed, err := taskSvc.ClaimNext(context.Background(), tasksvc.ClaimQuery{Type: ai.TaskTypeAIInfer})
	if err != nil || claimed == nil {
		t.Fatalf("claim: %v %+v", err, claimed)
	}
	if err := svc.HandleInferTask(context.Background(), *claimed); err != nil {
		t.Fatalf("handle: %v", err)
	}
	_ = taskSvc.MarkSucceeded(context.Background(), claimed.ID)

	results, err := svc.ListResults(context.Background(), "space-default", 42)
	if err != nil || len(results) != 1 {
		t.Fatalf("results: %v len=%d", err, len(results))
	}
	if results[0].Manual {
		t.Fatal("stub 结果不应 manual")
	}

	// 跨 Space 不可见
	other, err := svc.ListResults(context.Background(), "other-space", 42)
	if err != nil || len(other) != 0 {
		t.Fatalf("跨 Space 应为空: %v %d", err, len(other))
	}

	// manual 保护
	results[0].Manual = true
	results[0].UpdatedAt = time.Now().UTC()
	if err := db.Save(&results[0]).Error; err != nil {
		t.Fatalf("mark manual: %v", err)
	}
	// 再写一条非 manual
	now := time.Now().UTC()
	if err := repo.CreateResult(context.Background(), &models.AIResult{
		SpaceID: "space-default", MediaID: 42, TaskType: models.AITaskTypeOCR,
		ModelID: "stub-ocr-v1", ModelVersion: "1.0.0", NodeID: "stub-local", BatchID: "b2",
		PayloadJSON: `{"x":1}`, Manual: false, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	n, err := svc.RebuildResults(context.Background(), "space-default", 42, "", "", "tester")
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if n != 1 {
		t.Fatalf("应只删非 manual，got %d", n)
	}
	left, _ := svc.ListResults(context.Background(), "space-default", 42)
	if len(left) != 1 || !left[0].Manual {
		t.Fatalf("manual 应保留: %+v", left)
	}
}

func TestEnqueue_DisabledWithoutModels(t *testing.T) {
	db := openAITestDB(t)
	_ = db.Exec(`INSERT OR REPLACE INTO settings(key, value) VALUES(?, ?)`, settings.KeyAIEnabled, "1").Error
	settingsSvc := settings.NewService(db)
	taskSvc := tasksvc.NewService(db)
	svc := ai.NewService(ai.NewGormRepository(db), settingsSvc).WithTasks(taskSvc)
	// 启用但无模型
	_, err := svc.EnqueueInfer(context.Background(), "space-default", 1, models.AITaskTypeOCR, "", "", "t")
	if err != ai.ErrAIDisabled {
		t.Fatalf("期望 disabled，got %v", err)
	}
}

func TestSemanticSearch_StubHitAndSpaceIsolation(t *testing.T) {
	db := openAITestDB(t)
	_ = db.Exec(`INSERT OR REPLACE INTO settings(key, value) VALUES(?, ?)`, settings.KeyAIEnabled, "1").Error
	settingsSvc := settings.NewService(db)
	taskSvc := tasksvc.NewService(db)
	svc := ai.NewService(ai.NewGormRepository(db), settingsSvc).WithTasks(taskSvc)
	if err := svc.SeedStubFixture(context.Background()); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// 直接 upsert 与 query 同文本的向量，保证必中
	text := "hello-search"
	vec := ai.StubEmbedText(text, 8)
	now := time.Now().UTC()
	if err := ai.NewGormRepository(db).UpsertEmbedding(context.Background(), &models.AIEmbedding{
		SpaceID: "space-a", MediaID: 7, ModelID: "stub-embed-v1", ModelVersion: "1.0.0",
		Dim: 8, BatchID: "b1", Vector: ai.EncodeVector(vec), CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("upsert emb: %v", err)
	}
	// 另一 Space 同 media 不应命中
	if err := ai.NewGormRepository(db).UpsertEmbedding(context.Background(), &models.AIEmbedding{
		SpaceID: "space-b", MediaID: 7, ModelID: "stub-embed-v1", ModelVersion: "1.0.0",
		Dim: 8, BatchID: "b1", Vector: ai.EncodeVector(vec), CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("upsert emb b: %v", err)
	}

	hits, err := svc.SemanticSearch(context.Background(), "space-a", text, 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) == 0 || hits[0].MediaID != 7 {
		t.Fatalf("应命中 media 7: %+v", hits)
	}
	if hits[0].Score <= 0.99 {
		t.Fatalf("同向量 score 应接近 1，got %v", hits[0].Score)
	}

	// 关闭门
	_ = db.Exec(`INSERT OR REPLACE INTO settings(key, value) VALUES(?, ?)`, settings.KeyAIEnabled, "0").Error
	if _, err := svc.SemanticSearch(context.Background(), "space-a", text, 5); err != ai.ErrAIDisabled {
		t.Fatalf("关闭后应 disabled，got %v", err)
	}
}

func TestEmbeddingInfer_WritesVector(t *testing.T) {
	db := openAITestDB(t)
	_ = db.Exec(`INSERT OR REPLACE INTO settings(key, value) VALUES(?, ?)`, settings.KeyAIEnabled, "1").Error
	settingsSvc := settings.NewService(db)
	taskSvc := tasksvc.NewService(db)
	svc := ai.NewService(ai.NewGormRepository(db), settingsSvc).WithTasks(taskSvc)
	if err := svc.SeedStubFixture(context.Background()); err != nil {
		t.Fatalf("seed: %v", err)
	}
	task, err := svc.EnqueueInfer(context.Background(), "space-default", 9, models.AITaskTypeEmbedding, "stub-embed-v1", "", "t")
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	claimed, err := taskSvc.ClaimNext(context.Background(), tasksvc.ClaimQuery{Type: ai.TaskTypeAIInfer})
	if err != nil || claimed == nil {
		t.Fatalf("claim: %v", err)
	}
	if err := svc.HandleInferTask(context.Background(), *claimed); err != nil {
		t.Fatalf("handle: %v", err)
	}
	rows, err := ai.NewGormRepository(db).ListEmbeddingsBySpaceModel(context.Background(), "space-default", "stub-embed-v1")
	if err != nil || len(rows) != 1 || rows[0].MediaID != 9 {
		t.Fatalf("embedding 未写入: %v %+v", err, rows)
	}
	// rebuild 应删向量
	if _, err := svc.RebuildResults(context.Background(), "space-default", 9, models.AITaskTypeEmbedding, "", "t"); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	rows, _ = ai.NewGormRepository(db).ListEmbeddingsBySpaceModel(context.Background(), "space-default", "stub-embed-v1")
	if len(rows) != 0 {
		t.Fatalf("rebuild 后向量应清空: %d", len(rows))
	}
	_ = task
}

func TestInferAllStubTaskTypes(t *testing.T) {
	tests := []struct {
		name     string
		taskType string
		check    func(t *testing.T, payload string)
	}{
		{"object_scene", models.AITaskTypeObjectScene, func(t *testing.T, p string) {
			if !strings.Contains(p, `"objects"`) || !strings.Contains(p, `"scene"`) {
				t.Fatalf("object_scene payload 缺 objects/scene: %s", p)
			}
		}},
		{"face", models.AITaskTypeFace, func(t *testing.T, p string) {
			if !strings.Contains(p, `"faces"`) || !strings.Contains(p, `"bbox"`) {
				t.Fatalf("face payload 缺 faces/bbox: %s", p)
			}
		}},
		{"video_understanding", models.AITaskTypeVideoUnderstanding, func(t *testing.T, p string) {
			if !strings.Contains(p, `"segments"`) || !strings.Contains(p, `"summary"`) {
				t.Fatalf("video_understanding payload 缺 segments/summary: %s", p)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openAITestDB(t)
			_ = db.Exec(`INSERT OR REPLACE INTO settings(key, value) VALUES(?, ?)`, settings.KeyAIEnabled, "1").Error
			settingsSvc := settings.NewService(db)
			repo := ai.NewGormRepository(db)
			taskSvc := tasksvc.NewService(db)
			svc := ai.NewService(repo, settingsSvc).WithTasks(taskSvc)
			if err := svc.SeedStubFixture(context.Background()); err != nil {
				t.Fatalf("seed: %v", err)
			}
			task, err := svc.EnqueueInfer(context.Background(), "s1", 7, tt.taskType, "", "", "u")
			if err != nil {
				t.Fatalf("enqueue %s: %v", tt.name, err)
			}
			_ = task
			claimed, err := taskSvc.ClaimNext(context.Background(), tasksvc.ClaimQuery{Type: ai.TaskTypeAIInfer})
			if err != nil || claimed == nil {
				t.Fatalf("claim: %v", err)
			}
			if err := svc.HandleInferTask(context.Background(), *claimed); err != nil {
				t.Fatalf("handle: %v", err)
			}
			_ = taskSvc.MarkSucceeded(context.Background(), claimed.ID)
			results, err := svc.ListResults(context.Background(), "s1", 7)
			if err != nil || len(results) != 1 {
				t.Fatalf("results: %v len=%d", err, len(results))
			}
			tt.check(t, results[0].PayloadJSON)
		})
	}
}

func TestListResultsBySpaceAndBatch(t *testing.T) {
	db := openAITestDB(t)
	_ = db.Exec(`INSERT OR REPLACE INTO settings(key, value) VALUES(?, ?)`, settings.KeyAIEnabled, "1").Error
	settingsSvc := settings.NewService(db)
	repo := ai.NewGormRepository(db)
	svc := ai.NewService(repo, settingsSvc)
	if err := svc.SeedStubFixture(context.Background()); err != nil {
		t.Fatalf("seed: %v", err)
	}
	now := time.Now().UTC()
	for i, tt := range []string{models.AITaskTypeOCR, models.AITaskTypeFace, models.AITaskTypeOCR} {
		manual := i == 2
		if err := repo.CreateResult(context.Background(), &models.AIResult{
			SpaceID: "s1", MediaID: int64(i + 1), TaskType: tt, ModelID: "stub", ModelVersion: "1",
			NodeID: "n", BatchID: "b", PayloadJSON: `{}`, Manual: manual, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
	all, err := svc.ListResultsBySpace(context.Background(), "s1", "", nil)
	if err != nil || len(all) != 3 {
		t.Fatalf("ListResultsBySpace all: %v len=%d", err, len(all))
	}
	ocr, err := svc.ListResultsBySpace(context.Background(), "s1", models.AITaskTypeOCR, nil)
	if err != nil || len(ocr) != 2 {
		t.Fatalf("filter OCR: %v len=%d", err, len(ocr))
	}
	pending := false
	pendingRows, err := svc.ListResultsBySpace(context.Background(), "s1", "", &pending)
	if err != nil || len(pendingRows) != 2 {
		t.Fatalf("filter pending: %v len=%d", err, len(pendingRows))
	}
	// 批量确认
	n, err := svc.BatchConfirmResults(context.Background(), "s1", []int64{all[0].ID, all[1].ID}, "u")
	if err != nil || n != 2 {
		t.Fatalf("batch confirm: %v n=%d", err, n)
	}
	// 批量驳回：已确认项应跳过
	n2, err := svc.BatchRejectResults(context.Background(), "s1", []int64{all[0].ID}, "u")
	if err != nil || n2 != 0 {
		t.Fatalf("batch reject manual: %v n=%d", err, n2)
	}
	// 其他 Space 不可见
	other, err := svc.ListResultsBySpace(context.Background(), "s2", "", nil)
	if err != nil || len(other) != 0 {
		t.Fatalf("跨 Space 应空: %v len=%d", err, len(other))
	}
}

func TestConfirmRejectAndDuplicates(t *testing.T) {
	db := openAITestDB(t)
	_ = db.Exec(`INSERT OR REPLACE INTO settings(key, value) VALUES(?, ?)`, settings.KeyAIEnabled, "1").Error
	settingsSvc := settings.NewService(db)
	svc := ai.NewService(ai.NewGormRepository(db), settingsSvc)
	if err := svc.SeedStubFixture(context.Background()); err != nil {
		t.Fatalf("seed: %v", err)
	}
	now := time.Now().UTC()
	repo := ai.NewGormRepository(db)
	r1 := &models.AIResult{
		SpaceID: "s1", MediaID: 1, TaskType: models.AITaskTypeOCR, ModelID: "stub-ocr-v1", ModelVersion: "1.0.0",
		NodeID: "stub-local", BatchID: "b", PayloadJSON: `{}`, Manual: false, CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.CreateResult(context.Background(), r1); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.ConfirmResult(context.Background(), "s1", r1.ID, "u"); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	got, _ := repo.GetResult(context.Background(), "s1", r1.ID)
	if got == nil || !got.Manual {
		t.Fatal("应为 manual")
	}
	if err := svc.RejectResult(context.Background(), "s1", r1.ID, "u"); err == nil {
		t.Fatal("manual 不可 reject")
	}
	r2 := &models.AIResult{
		SpaceID: "s1", MediaID: 2, TaskType: models.AITaskTypeOCR, ModelID: "stub-ocr-v1", ModelVersion: "1.0.0",
		NodeID: "stub-local", BatchID: "b", PayloadJSON: `{}`, Manual: false, CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.CreateResult(context.Background(), r2); err != nil {
		t.Fatalf("create2: %v", err)
	}
	if err := svc.RejectResult(context.Background(), "s1", r2.ID, "u"); err != nil {
		t.Fatalf("reject: %v", err)
	}

	// 重复候选：两 media 相同向量
	vec := ai.StubEmbedText("same", 8)
	for _, mid := range []int64{10, 11} {
		if err := repo.UpsertEmbedding(context.Background(), &models.AIEmbedding{
			SpaceID: "s1", MediaID: mid, ModelID: "stub-embed-v1", ModelVersion: "1.0.0",
			Dim: 8, BatchID: "b", Vector: ai.EncodeVector(vec), CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("emb: %v", err)
		}
	}
	groups, err := svc.FindDuplicateCandidates(context.Background(), "s1", 0.9)
	if err != nil || len(groups) == 0 {
		t.Fatalf("duplicates: %v %+v", err, groups)
	}
}
