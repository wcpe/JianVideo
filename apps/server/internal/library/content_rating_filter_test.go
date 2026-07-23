package library

import (
	"testing"
	"time"

	"github.com/wcpe/JianVideo/internal/db/models"
)

func TestListMediaFiles_FiltersByMaxContentRating(t *testing.T) {
	svc, db := newTestService(t)
	now := time.Now()
	if err := db.Create(&models.LibraryPath{
		ID: 1, SpaceID: models.DefaultSpaceID, Path: "/tmp/lib", Type: "local", CreatedAt: now,
	}).Error; err != nil {
		t.Fatalf("创建媒体库: %v", err)
	}

	mk := func(name, rating string) int64 {
		mf, err := svc.CreateMediaFile(1, "/tmp/"+name, 100)
		if err != nil {
			t.Fatalf("创建 %s: %v", name, err)
		}
		if rating != "" {
			if err := svc.UpdateMediaContentRatingInSpace(models.DefaultSpaceID, mf.ID, rating); err != nil {
				t.Fatalf("设分级: %v", err)
			}
		}
		return mf.ID
	}
	idG := mk("g.mp4", "G")
	idR := mk("r.mp4", "R")
	idU := mk("u.mp4", "UNRATED")
	idPG13 := mk("pg13.mp4", "PG-13")
	_ = idG

	page, err := svc.ListMediaFilesPage(MediaFilter{
		SpaceID: models.DefaultSpaceID, MaxContentRating: "PG",
	}, MediaPageRequest{Page: 1, PageSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	ids := map[int64]bool{}
	for _, it := range page.Items {
		ids[it.ID] = true
	}
	if ids[idR] {
		t.Fatal("max=PG 列表不应含 R")
	}
	if ids[idPG13] {
		t.Fatal("max=PG 列表不应含 PG-13")
	}
	if !ids[idU] {
		t.Fatal("UNRATED 应可见")
	}
	if !ids[idG] {
		t.Fatal("G 应可见")
	}

	_, err = svc.GetMediaFileByIDInSpaceForViewer(models.DefaultSpaceID, idR, "PG")
	if err == nil {
		t.Fatal("直链 R 对 PG 用户应 not found")
	}
	if _, err := svc.GetMediaFileByIDInSpaceForViewer(models.DefaultSpaceID, idU, "PG"); err != nil {
		t.Fatalf("UNRATED 应可读: %v", err)
	}
	// 无上限时可见 R
	if _, err := svc.GetMediaFileByIDInSpaceForViewer(models.DefaultSpaceID, idR, ""); err != nil {
		t.Fatalf("无上限应可读 R: %v", err)
	}
}
