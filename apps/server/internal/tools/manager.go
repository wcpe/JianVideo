package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/settings"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

// ManagerOptions 描述工具管理器依赖。
type ManagerOptions struct {
	Installer *Installer
	Settings  *settings.Service
	Tasks     *tasksvc.Service
	Registry  []Source
	Apply     ApplyFunc
}

// Manager 编排工具源查询、下载任务入队与安装后热应用。
type Manager struct {
	installer *Installer
	settings  *settings.Service
	tasks     *tasksvc.Service
	registry  []Source
	apply     ApplyFunc
}

// NewManager 创建工具管理器。
func NewManager(options ManagerOptions) *Manager {
	registry := options.Registry
	if registry == nil {
		registry = DefaultRegistry()
	}
	return &Manager{
		installer: options.Installer,
		settings:  options.Settings,
		tasks:     options.Tasks,
		registry:  cloneSources(registry),
		apply:     options.Apply,
	}
}

// Sources 返回可展示的工具下载源副本。
func (m *Manager) Sources() []Source {
	return cloneSources(m.registry)
}

// Status 返回工具路径配置与受控目录内已安装版本。
func (m *Manager) Status(ctx context.Context) ([]Status, error) {
	tools := []string{ToolFFmpeg, ToolFFprobe, ToolMagick}
	result := make([]Status, 0, len(tools))
	for _, tool := range tools {
		key := settingKey(tool)
		value := ""
		if m.settings != nil {
			var err error
			value, err = m.settings.Get(key)
			if err != nil {
				return nil, err
			}
		}
		result = append(result, Status{
			Tool:           tool,
			SettingKey:     key,
			ConfiguredPath: value,
			Installed:      m.installed(ctx, tool),
		})
	}
	return result, nil
}

// EnqueueDownload 校验请求并创建系统级工具下载任务。
func (m *Manager) EnqueueDownload(ctx context.Context, req DownloadRequest) (*models.Task, error) {
	if m.tasks == nil {
		return nil, fmt.Errorf("任务服务未启用")
	}
	source, err := ResolveDownloadRequest(req, m.registry)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(source)
	if err != nil {
		return nil, fmt.Errorf("编码下载任务失败: %w", err)
	}
	return m.tasks.Enqueue(ctx, tasksvc.EnqueueInput{
		Scope:          models.TaskScopeSystem,
		Type:           TaskTypeDownload,
		Priority:       5,
		MaxAttempts:    1,
		IdempotencyKey: "tool.download:" + source.ID,
		PayloadJSON:    string(payload),
		ResourceType:   "tool",
		ResourceID:     source.Tool,
	})
}

// RegisterWorker 注册工具下载 worker。
func (m *Manager) RegisterWorker(registry *tasksvc.WorkerRegistry) error {
	if registry == nil {
		return fmt.Errorf("worker 注册表未启用")
	}
	return registry.Register(TaskTypeDownload, 1, m.handleTask)
}

func (m *Manager) handleTask(ctx context.Context, task models.Task) error {
	var source Source
	if err := json.Unmarshal([]byte(task.PayloadJSON), &source); err != nil {
		return fmt.Errorf("解析工具下载任务失败: %w", err)
	}
	if m.installer == nil {
		return fmt.Errorf("工具安装器未启用")
	}
	if m.settings == nil {
		return fmt.Errorf("设置服务未启用")
	}
	result, err := m.installer.Install(ctx, source, m.progress(ctx, task.ID))
	if err != nil {
		return err
	}
	if err := m.progress(ctx, task.ID)(0, 0, "应用中"); err != nil {
		return err
	}
	if m.apply != nil {
		if err := m.apply(result); err != nil {
			return fmt.Errorf("热应用工具路径失败: %w", err)
		}
	}
	if err := m.settings.SetMany(map[string]string{settingKey(result.Tool): result.Path}); err != nil {
		return fmt.Errorf("写入工具路径设置失败: %w", err)
	}
	return nil
}

func (m *Manager) progress(ctx context.Context, taskID int64) ProgressFunc {
	return func(downloaded, total int64, step string) error {
		if m.tasks == nil {
			return nil
		}
		task, err := m.tasks.Get(ctx, taskID, tasksvc.Query{Scope: models.TaskScopeSystem})
		if err == nil && task.Status == models.TaskStatusCanceled {
			return fmt.Errorf("任务已取消")
		}
		progress := 0
		if total > 0 {
			progress = int(downloaded * 80 / total)
		} else {
			progress = stepProgress(step)
		}
		if progress < 1 {
			progress = 1
		}
		if progress > 80 {
			progress = 80
		}
		return m.tasks.UpdateProgress(ctx, taskID, tasksvc.ProgressInput{Progress: progress, Checkpoint: step})
	}
}

func stepProgress(step string) int {
	switch strings.TrimSpace(step) {
	case "解压中":
		return 84
	case "探测中":
		return 90
	case "安装中":
		return 96
	case "应用中":
		return 98
	default:
		return 1
	}
}

func (m *Manager) installed(_ context.Context, tool string) []InstallRecord {
	if m.installer == nil {
		return nil
	}
	root := filepath.Join(m.installer.baseDir, safePathPart(tool))
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	records := make([]InstallRecord, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(root, entry.Name(), "bin", defaultExecutableName(tool))
		if _, err := os.Stat(path); err != nil {
			path = findInstalledExecutable(filepath.Join(root, entry.Name()), tool)
		}
		records = append(records, InstallRecord{Version: entry.Name(), Path: path, UpdatedAt: info.ModTime()})
	}
	return records
}

func settingKey(tool string) string {
	switch normalizeTool(tool) {
	case ToolFFmpeg:
		return settings.KeyFFmpegPath
	case ToolFFprobe:
		return settings.KeyFFprobePath
	case ToolMagick:
		return settings.KeyMagickPath
	default:
		return ""
	}
}

func findInstalledExecutable(root, tool string) string {
	names := executableNames(tool)
	var found string
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		name := strings.ToLower(entry.Name())
		for _, candidate := range names {
			if name == candidate {
				found = path
				return filepath.SkipAll
			}
		}
		return nil
	})
	return found
}

// TaskIDString 将任务 ID 转成前端轮询用字符串。
func TaskIDString(id int64) string {
	return strconv.FormatInt(id, 10)
}
