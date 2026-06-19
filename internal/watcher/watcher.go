package watcher

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"jianvideo/internal/db/models"
	"jianvideo/internal/library"
)

// 视频文件后缀名白名单。
var videoExts = map[string]bool{
	"mp4": true, "mkv": true, "avi": true, "mov": true,
	"webm": true, "rmvb": true, "ts": true, "flv": true,
	"wmv": true, "m4v": true,
}

const debounceInterval = 500 * time.Millisecond

const smbPollInterval = 5 * time.Minute

// Watcher 文件系统事件监听器。
type Watcher struct {
	watcher    *fsnotify.Watcher
	library    *library.Service
	debounce   map[string]*time.Timer
	mu         sync.Mutex
	done       chan struct{}
	pathToLib  map[string]int64 // 目录路径 → library_id
	smbLibs    []models.LibraryPath // SMB 路径列表，用于轮询
}

// New 创建文件监听器。
func New(lib *library.Service) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &Watcher{
		watcher:    fw,
		library:    lib,
		debounce:   make(map[string]*time.Timer),
		done:       make(chan struct{}),
		pathToLib:  make(map[string]int64),
		smbLibs:    make([]models.LibraryPath, 0),
	}, nil
}

// Start 启动监听，注册所有已存在的媒体库目录。
func (w *Watcher) Start() error {
	paths, err := w.library.ListLibraryPaths()
	if err != nil {
		return err
	}

	for _, lp := range paths {
		if lp.Enabled == 0 {
			continue
		}
		if lp.Type == "smb" {
			w.smbLibs = append(w.smbLibs, lp)
			continue
		}
		if err := w.addDir(lp.ID, lp.Path); err != nil {
			log.Printf("[WARN] 添加监听目录失败: %s, 错误: %v", lp.Path, err)
		}
	}

	go w.loop()

	// 启动 SMB 轮询
	if len(w.smbLibs) > 0 {
		go w.pollSMBLoop()
	}

	return nil
}

// Stop 停止监听。
func (w *Watcher) Stop() {
	close(w.done)
	w.watcher.Close()
}

// addDir 递归添加目录及其子目录到监听列表。
func (w *Watcher) addDir(libraryID int64, dir string) error {
	w.mu.Lock()
	w.pathToLib[dir] = libraryID
	w.mu.Unlock()

	if err := w.watcher.Add(dir); err != nil {
		return err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil // 目录可能暂时不可读，跳过子目录
	}
	for _, entry := range entries {
		if entry.IsDir() {
			subDir := filepath.Join(dir, entry.Name())
			w.mu.Lock()
			w.pathToLib[subDir] = libraryID
			w.mu.Unlock()
			if err := w.watcher.Add(subDir); err != nil {
				log.Printf("[WARN] 添加子目录监听失败: %s, 错误: %v", subDir, err)
			}
		}
	}
	return nil
}

// isVideoFile 判断文件是否为视频文件（基于后缀名白名单）。
func isVideoFile(path string) bool {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	return videoExts[ext]
}

// loop 事件循环。
func (w *Watcher) loop() {
	for {
		select {
		case <-w.done:
			return
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			w.handleEvent(event)
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("[ERROR] fsnotify 错误: %v", err)
		}
	}
}

// handleEvent 处理单个文件系统事件。
func (w *Watcher) handleEvent(event fsnotify.Event) {
	path := event.Name

	switch {
	case event.Op&fsnotify.Create == fsnotify.Create:
		// 检查是否是目录，如果是则添加监听
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			libID := w.findLibraryID(path)
			if libID > 0 {
				if err := w.addDir(libID, path); err != nil {
					log.Printf("[WARN] 添加新目录监听失败: %s, 错误: %v", path, err)
				}
			}
			return
		}
		if isVideoFile(path) {
			w.scheduleInsert(path)
		}

	case event.Op&fsnotify.Write == fsnotify.Write:
		if isVideoFile(path) {
			w.scheduleInsert(path)
		}

	case event.Op&fsnotify.Remove == fsnotify.Remove, event.Op&fsnotify.Rename == fsnotify.Rename:
		w.cancelDebounce(path)
		w.removeRecord(path)
	}
}

// scheduleInsert 调度去抖插入。
func (w *Watcher) scheduleInsert(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if timer, ok := w.debounce[path]; ok {
		timer.Stop()
	}

	w.debounce[path] = time.AfterFunc(debounceInterval, func() {
		w.mu.Lock()
		delete(w.debounce, path)
		w.mu.Unlock()

		w.insertRecord(path)
	})
}

// cancelDebounce 取消去抖计时器。
func (w *Watcher) cancelDebounce(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if timer, ok := w.debounce[path]; ok {
		timer.Stop()
		delete(w.debounce, path)
	}
}

// insertRecord 将文件入库。
func (w *Watcher) insertRecord(path string) {
	libID := w.findLibraryID(path)
	if libID == 0 {
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		return
	}

	// 检查是否已存在，避免重复入库
	existing, err := w.library.GetMediaFileByPath(path)
	if err == nil && existing != nil {
		return
	}

	if _, err := w.library.CreateMediaFile(libID, path, info.Size()); err != nil {
		log.Printf("[WARN] 媒体文件入库失败: %s, 错误: %v", path, err)
	} else {
		log.Printf("[INFO] 新视频文件已入库: %s", path)
	}
}

// removeRecord 移除数据库记录。
func (w *Watcher) removeRecord(path string) {
	if err := w.library.DeleteMediaFileByPath(path); err != nil {
		log.Printf("[WARN] 删除媒体文件记录失败: %s, 错误: %v", path, err)
	} else {
		log.Printf("[INFO] 媒体文件记录已移除: %s", path)
	}
}

// pollSMBLoop 定期轮询 SMB 路径，索引新增视频文件。
func (w *Watcher) pollSMBLoop() {
	ticker := time.NewTicker(smbPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.done:
			return
		case <-ticker.C:
			w.pollAllSMB()
		}
	}
}

// pollAllSMB 轮询所有 SMB 媒体库路径。
func (w *Watcher) pollAllSMB() {
	for _, lp := range w.smbLibs {
		count, err := w.library.ScanLibraryWithType(lp.ID, lp.Path, "smb")
		if err != nil {
			log.Printf("[WARN] SMB 轮询扫描失败: %s, err=%v", lp.Path, err)
			continue
		}
		if count > 0 {
			log.Printf("[INFO] SMB 轮询扫描发现 %d 个新文件: %s", count, lp.Path)
		}
	}
}

// findLibraryID 根据文件路径查找所属的 library_id。
func (w *Watcher) findLibraryID(filePath string) int64 {
	// 从文件路径向上匹配已知的库目录
	dir := filepath.Dir(filePath)
	for dir != "/" && dir != "." {
		if id, ok := w.pathToLib[dir]; ok {
			return id
		}
		dir = filepath.Dir(dir)
	}
	return 0
}
