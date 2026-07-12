// Package watcher 基于 fsnotify 监听本地媒体目录的文件变更并上报媒体库。
package watcher

import (
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
)

const debounceInterval = 500 * time.Millisecond

const smbPollInterval = 5 * time.Minute

type pathBinding struct {
	libraryID int64
	spaceID   string
}

// Watcher 文件系统事件监听器。
type Watcher struct {
	watcher   *fsnotify.Watcher
	library   *library.Service
	scanQueue *library.TaskQueue
	debounce  map[string]*time.Timer
	mu        sync.RWMutex
	done      chan struct{}
	bindings  map[string][]pathBinding // 目录路径 → 全部 Space/媒体库绑定
	smbLibs   []models.LibraryPath
}

// New 创建文件监听器。
func New(lib *library.Service) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &Watcher{
		watcher:  fw,
		library:  lib,
		debounce: make(map[string]*time.Timer),
		done:     make(chan struct{}),
		bindings: make(map[string][]pathBinding),
		smbLibs:  make([]models.LibraryPath, 0),
	}, nil
}

// WithScanQueue 注入扫描队列，使文件事件进入统一扫描任务。
func (w *Watcher) WithScanQueue(q *library.TaskQueue) *Watcher {
	w.scanQueue = q
	return w
}

// Start 启动监听，注册所有已存在的媒体库目录。
func (w *Watcher) Start() error {
	paths, err := w.library.ListAllLibraryPaths()
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
		if err := w.addDir(lp.ID, lp.SpaceID, lp.Path); err != nil {
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
	// 停止流程关闭底层监听器，关闭错误可忽略
	_ = w.watcher.Close()
	close(w.done)
}

// addDir 递归添加目录及其子目录到监听列表。
func (w *Watcher) addDir(libraryID int64, spaceID, dir string) error {
	binding := pathBinding{libraryID: libraryID, spaceID: spaceID}
	if w.addBinding(filepath.ToSlash(dir), binding) {
		if err := w.watcher.Add(dir); err != nil {
			return err
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil // 目录可能暂时不可读，跳过子目录
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		subDir := filepath.Join(dir, entry.Name())
		if !w.addBinding(filepath.ToSlash(subDir), binding) {
			continue
		}
		if err := w.watcher.Add(subDir); err != nil {
			log.Printf("[WARN] 添加子目录监听失败: %s, 错误: %v", subDir, err)
		}
	}
	return nil
}

func (w *Watcher) addBinding(path string, binding pathBinding) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	items := w.bindings[path]
	for _, item := range items {
		if item == binding {
			return false
		}
	}
	w.bindings[path] = append(items, binding)
	return len(items) == 0
}

// isMediaFile 判断文件是否为内置媒体文件。
func isMediaFile(path string) bool {
	return library.NewService(nil).IsMediaFile(path)
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
		// 检查是否是目录，如果是则把全部上级绑定附加到新目录。
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			for _, binding := range w.findBindings(path) {
				if err := w.addDir(binding.libraryID, binding.spaceID, path); err != nil {
					log.Printf("[WARN] 添加新目录监听失败: %s, 错误: %v", path, err)
				}
			}
			return
		}
		if w.isWatchedMediaFile(path) {
			w.scheduleInsert(path)
		}

	case event.Op&fsnotify.Write == fsnotify.Write:
		if w.isWatchedMediaFile(path) {
			w.scheduleInsert(path)
		}

	case event.Op&fsnotify.Remove == fsnotify.Remove, event.Op&fsnotify.Rename == fsnotify.Rename:
		w.cancelDebounce(path)
		w.removeRecord(path)
	}
}

// isWatchedMediaFile 判断文件是否属于任一已监听媒体库支持的媒体文件。
func (w *Watcher) isWatchedMediaFile(path string) bool {
	for _, binding := range w.findBindings(path) {
		if w.library.IsMediaFileForLibrary(binding.libraryID, path) {
			return true
		}
	}
	return false
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

// insertRecord 将文件变更 fan-out 到全部 Space/媒体库绑定。
func (w *Watcher) insertRecord(path string) {
	for _, binding := range w.findBindings(path) {
		if !w.library.IsMediaFileForLibrary(binding.libraryID, path) {
			continue
		}
		w.applyChange(library.ScanChange{
			SpaceID:   binding.spaceID,
			LibraryID: binding.libraryID,
			Path:      path,
			Op:        library.ScanChangeModified,
		})
	}
}

// removeRecord 将文件缺失 fan-out 到全部 Space/媒体库绑定。
func (w *Watcher) removeRecord(path string) {
	for _, binding := range w.findBindings(path) {
		w.applyChange(library.ScanChange{
			SpaceID:   binding.spaceID,
			LibraryID: binding.libraryID,
			Path:      path,
			Op:        library.ScanChangeRemoved,
		})
	}
}

func (w *Watcher) applyChange(change library.ScanChange) {
	if w.scanQueue != nil {
		if _, err := w.scanQueue.EnqueueChange(change); err != nil {
			log.Printf("[WARN] 媒体文件变更入队失败: %s, 错误: %v", change.Path, err)
		}
		return
	}
	if _, err := w.library.ApplyScanChange(change); err != nil {
		log.Printf("[WARN] 媒体文件变更处理失败: %s, 错误: %v", change.Path, err)
		return
	}
	log.Printf("[INFO] 媒体文件变更已处理: spaceID=%s, libraryID=%d, path=%s", change.SpaceID, change.LibraryID, change.Path)
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
		if w.scanQueue != nil {
			if _, err := w.scanQueue.EnqueueInSpace(lp.SpaceID, lp.ID, lp.Path, lp.Type, library.ScanModeIncremental); err != nil {
				log.Printf("[WARN] SMB 轮询扫描入队失败: %s, err=%v", lp.Path, err)
			}
			continue
		}
		count, err := w.library.ScanLibraryWithTypeInSpace(lp.SpaceID, lp.ID, lp.Path, "smb", library.ScanModeIncremental)
		if err != nil {
			log.Printf("[WARN] SMB 轮询扫描失败: %s, err=%v", lp.Path, err)
			continue
		}
		if count > 0 {
			log.Printf("[INFO] SMB 轮询扫描发现 %d 个新文件: %s", count, lp.Path)
		}
	}
}

// findLibraryID 根据文件路径查找首个兼容绑定的 library_id。
func (w *Watcher) findLibraryID(filePath string) int64 {
	id, _ := w.findLibrary(filePath)
	return id
}

func (w *Watcher) findSpaceID(filePath string) string {
	_, spaceID := w.findLibrary(filePath)
	return spaceID
}

func (w *Watcher) findLibrary(filePath string) (int64, string) {
	bindings := w.findBindings(filePath)
	if len(bindings) == 0 {
		return 0, ""
	}
	return bindings[0].libraryID, bindings[0].spaceID
}

func (w *Watcher) findBindings(filePath string) []pathBinding {
	// 统一为正斜杠，与 bindings 的 key 格式一致。
	dir := filepath.ToSlash(filepath.Dir(filePath))
	for dir != "/" && dir != "." {
		w.mu.RLock()
		items := append([]pathBinding(nil), w.bindings[dir]...)
		w.mu.RUnlock()
		if len(items) > 0 {
			return items
		}
		parent := filepath.ToSlash(filepath.Dir(dir))
		if parent == dir {
			break // 已到达根目录，避免 Windows 上无限循环
		}
		dir = parent
	}
	return nil
}
