import { useState, useEffect, useCallback, useMemo } from 'react';
import {
  Stack,
  Title,
  Card,
  TextInput,
  Button,
  Alert,
  Skeleton,
  Text,
  Table,
  Badge,
  Group,
  Code,
  ActionIcon,
  Switch,
  Box,
  Select,
  Progress,
  Divider,
} from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { IconAlertCircle, IconTrash, IconPlus, IconDownload } from '@tabler/icons-react';
import { Link } from 'react-router-dom';
import {
  getSettingDefinitions,
  getSettings,
  getStorageSettings,
  updateSettings,
  SETTING_SENSITIVE_DISPLAY_VALUE,
  SETTING_KEY_RECYCLE_BIN_PATHS,
  SETTING_KEY_RECYCLE_RETENTION_DAYS,
  SETTING_KEY_RECYCLE_AUTO_CLEANUP_ENABLED,
  SETTING_KEY_RECYCLE_AUTO_CLEANUP_INTERVAL_SEC,
  SETTING_KEY_SCAN_INTERVAL,
  SETTING_KEY_FFMPEG_PATH,
  SETTING_KEY_FFPROBE_PATH,
  SETTING_KEY_MAGICK_PATH,
  SETTING_KEY_NETWORK_PROXY,
  SETTING_KEY_DEBUG_LOG,
  SETTING_KEY_MEDIA_INFERENCE_ENABLED,
  SETTING_KEY_MEDIA_INFERENCE_DISABLED_LIBRARIES,
  parseBooleanSetting,
  SETTING_KEY_UPLOAD_TARGET_DIR,
  SETTING_KEY_UPLOAD_NAMING_RULE,
} from '@/api/settings';
import { getLibraryPaths } from '@/api/library';
import {
  getEnvVars,
  detectFFmpeg,
  testProxy,
  getTools,
  getToolSources,
  downloadTool,
} from '@/api/system';
import { listTasks } from '@/api/tasks';
import { changePassword } from '@/api/auth';
import {
  addSpaceMember,
  createSpace,
  createUser,
  listSpaceMembers,
  listSpaces,
  listUsers,
  removeSpaceMember,
  setUserStatus,
  updateMemberMaxRating,
  updateSpaceParental,
  type ManagedUser,
  type SpaceMember,
  type SpaceSummary,
} from '@/api/space';
import AnchorNav from '@/components/AnchorNav';
import { useAuthStore } from '@/stores/auth';
import { extractErrorMessage } from '@/utils/error';
import {
  parseRecycleBinRows,
  validateRecycleBinRows,
  serializeRecycleBinRows,
  type RecycleBinRow,
} from '@/utils/recycle-bin';
import { MAX_RATING_OPTIONS } from '@/utils/content-rating';
import type {
  EnvVar,
  LibraryPath,
  SettingDefinition,
  SettingsMap,
  TaskItem,
  ToolName,
  ToolSource,
  ToolStatus,
} from '@/types';
import type { StorageSettingsInfo } from '@/api/settings';

// 设置页左侧锚点（FR-113）：各分区标题挂同名 id，点击滚动定位、滚动高亮
const SETTINGS_ANCHORS = [
  { id: 'set-storage', label: '存储与 Space' },
  { id: 'set-account', label: '账户安全' },
  { id: 'set-users-spaces', label: '用户与 Space' },
  { id: 'set-parental', label: '家长控制' },
  { id: 'set-scan', label: '扫描' },
  { id: 'set-inference', label: '影视信息' },
  { id: 'set-upload', label: '上传' },
  { id: 'set-network', label: '网络' },
  { id: 'set-tools', label: '工具路径' },
  { id: 'set-recycle', label: '回收站' },
  { id: 'set-diagnostics', label: '诊断' },
  { id: 'set-env', label: '环境变量' },
];

/** Space 成员角色选项（不可经 API 直接设 owner） */
const MEMBER_ROLE_OPTIONS = [
  { value: 'viewer', label: 'viewer · 只读' },
  { value: 'editor', label: 'editor · 可写' },
];

const TOOL_OPTIONS: { value: ToolName; label: string }[] = [
  { value: 'ffmpeg', label: 'FFmpeg' },
  { value: 'ffprobe', label: 'FFprobe' },
  { value: 'magick', label: 'Magick' },
];

function formatToolSize(size: number): string {
  if (size <= 0) return '大小未知';
  if (size >= 1024 * 1024) return `${(size / 1024 / 1024).toFixed(1)} MB`;
  return `${Math.ceil(size / 1024)} KB`;
}

function taskProgress(task: TaskItem | null): number {
  if (!task) return 0;
  return Math.max(0, Math.min(100, Math.round(task.progress * 100)));
}

function parseDisabledInferenceLibraries(raw: string | undefined): Set<number> {
  try {
    const values = JSON.parse(raw ?? '[]') as unknown;
    if (!Array.isArray(values)) return new Set();
    return new Set(values.filter((value): value is number => Number.isInteger(value) && value > 0));
  } catch {
    return new Set();
  }
}

function serializeDisabledInferenceLibraries(values: Set<number>): string {
  return JSON.stringify([...values].sort((a, b) => a - b));
}

/** 设置页（FR-24/FR-56/FR-63/FR-87）：按扫描/网络/工具路径/回收站分区读写运行期键值设置，并只读查看环境变量 */
export default function SettingsPage() {
  const [scanInterval, setScanInterval] = useState('');
  // 回收站路径以结构化「盘符→路径」行列表编辑（FR-87），保存时序列化为 JSON 串
  const [recycleBinRows, setRecycleBinRows] = useState<RecycleBinRow[]>([]);
  // 回收站保留期与自动清理（FR2-054）
  const [recycleRetentionDays, setRecycleRetentionDays] = useState('30');
  const [recycleAutoCleanupEnabled, setRecycleAutoCleanupEnabled] = useState(true);
  const [recycleAutoCleanupIntervalSec, setRecycleAutoCleanupIntervalSec] = useState('3600');
  const [ffmpegPath, setFfmpegPath] = useState('');
  const [ffprobePath, setFfprobePath] = useState('');
  const [magickPath, setMagickPath] = useState('');
  const [networkProxy, setNetworkProxy] = useState('');
  const [networkProxyMasked, setNetworkProxyMasked] = useState(false);
  // 调试日志开关（FR-110）：开启输出 GORM 详细 SQL/慢查询日志，关闭恢复安静；保存即生效
  const [debugLog, setDebugLog] = useState(false);
  // 本地离线影视信息推断总开关（FR2-031）：关闭后扫描与回填不再产生新推断
  const [mediaInferenceEnabled, setMediaInferenceEnabled] = useState(true);
  const [inferenceDisabledLibraries, setInferenceDisabledLibraries] = useState<Set<number>>(
    new Set(),
  );
  const [inferenceLibraries, setInferenceLibraries] = useState<LibraryPath[]>([]);
  // Web 上传默认落盘目录与命名规则（FR-149）：目录须为已注册本地库目录或其子目录
  const [uploadTargetDir, setUploadTargetDir] = useState('');
  const [uploadNamingRule, setUploadNamingRule] = useState('');
  // 本地库目录列表，供「默认上传位置」下拉选择
  const [localLibraryPaths, setLocalLibraryPaths] = useState<LibraryPath[]>([]);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [definitions, setDefinitions] = useState<Record<string, SettingDefinition>>({});
  const [storageInfo, setStorageInfo] = useState<StorageSettingsInfo | null>(null);
  const [storageError, setStorageError] = useState<string | null>(null);

  // 修改密码（FR-108）：独立于设置读写，不被设置加载阻塞
  const [currentPassword, setCurrentPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [changingPassword, setChangingPassword] = useState(false);
  const [pwError, setPwError] = useState<string | null>(null);

  // 家长控制（FR2-051）：Space 默认最高可见级 + 成员覆盖；改策略需密码确认
  const [parentalSpace, setParentalSpace] = useState<SpaceSummary | null>(null);
  const [parentalMembers, setParentalMembers] = useState<SpaceMember[]>([]);
  const [parentalLoading, setParentalLoading] = useState(true);
  const [parentalError, setParentalError] = useState<string | null>(null);
  const [defaultMaxRating, setDefaultMaxRating] = useState('');
  const [parentalPassword, setParentalPassword] = useState('');
  const [parentalSaving, setParentalSaving] = useState(false);
  // 成员 max_rating 草稿：user_id → 字符串（空=继承 Space 默认）
  const [memberRatingDrafts, setMemberRatingDrafts] = useState<Record<number, string>>({});
  const [memberSavingID, setMemberSavingID] = useState<number | null>(null);

  // 用户与 Space 管理（FR2-010）：默认 Space owner 管用户；各 Space owner 管成员
  const authUsername = useAuthStore((s) => s.username);
  const [managedUsers, setManagedUsers] = useState<ManagedUser[]>([]);
  const [managedSpaces, setManagedSpaces] = useState<SpaceSummary[]>([]);
  const [usersSpacesLoading, setUsersSpacesLoading] = useState(true);
  const [usersSpacesError, setUsersSpacesError] = useState<string | null>(null);
  const [usersForbidden, setUsersForbidden] = useState(false);
  const [newUsername, setNewUsername] = useState('');
  const [newUserPassword, setNewUserPassword] = useState('');
  const [creatingUser, setCreatingUser] = useState(false);
  const [userStatusBusyID, setUserStatusBusyID] = useState<number | null>(null);
  const [newSpaceID, setNewSpaceID] = useState('');
  const [newSpaceName, setNewSpaceName] = useState('');
  const [creatingSpace, setCreatingSpace] = useState(false);
  const [memberSpaceID, setMemberSpaceID] = useState('');
  const [memberUsername, setMemberUsername] = useState('');
  const [memberRole, setMemberRole] = useState('viewer');
  const [addingMember, setAddingMember] = useState(false);
  const [memberList, setMemberList] = useState<SpaceMember[]>([]);
  const [memberListLoading, setMemberListLoading] = useState(false);
  const [removingMemberKey, setRemovingMemberKey] = useState<string | null>(null);

  // 环境变量（FR-56，只读）
  const [envVars, setEnvVars] = useState<EnvVar[]>([]);
  const [envError, setEnvError] = useState<string | null>(null);
  const [envLoading, setEnvLoading] = useState(true);

  // FFmpeg 路径检测（FR-56）
  const [detecting, setDetecting] = useState(false);
  const [detectResult, setDetectResult] = useState<{ available: boolean; version: string } | null>(
    null,
  );
  const [toolStatuses, setToolStatuses] = useState<ToolStatus[]>([]);
  const [toolSources, setToolSources] = useState<ToolSource[]>([]);
  const [toolsLoading, setToolsLoading] = useState(true);
  const [toolsError, setToolsError] = useState<string | null>(null);
  const [selectedTool, setSelectedTool] = useState<ToolName>('ffmpeg');
  const [selectedSourceID, setSelectedSourceID] = useState<string | null>(null);
  const [customURL, setCustomURL] = useState('');
  const [customSHA256, setCustomSHA256] = useState('');
  const [customVersion, setCustomVersion] = useState('');
  const [allowLocalHTTP, setAllowLocalHTTP] = useState(false);
  const [downloadingTool, setDownloadingTool] = useState(false);
  const [downloadTaskID, setDownloadTaskID] = useState('');
  const [latestToolTask, setLatestToolTask] = useState<TaskItem | null>(null);

  // 代理连通性测试（FR-89）
  const [testingProxy, setTestingProxy] = useState(false);
  const [proxyTestResult, setProxyTestResult] = useState<{
    reachable: boolean;
    detail: string;
  } | null>(null);

  // 挂载时加载 owner 可见的存储目录、索引库与 Space 信息。
  useEffect(() => {
    let active = true;
    getStorageSettings()
      .then((data) => {
        if (active) setStorageInfo(data);
      })
      .catch((err) => {
        if (active) setStorageError(extractErrorMessage(err, '加载存储与 Space 信息失败'));
      });
    return () => {
      active = false;
    };
  }, []);

  // 挂载时加载家长控制策略（FR2-051）：取可访问 Space 列表首项 + 成员；失败不阻塞其余设置
  // 不依赖 storageInfo，避免与存储区加载时序竞态导致长期停留在 Skeleton
  useEffect(() => {
    let active = true;
    setParentalLoading(true);
    setParentalError(null);
    (async () => {
      try {
        const spaces = await listSpaces();
        if (!active) return;
        const current = spaces[0] ?? null;
        setParentalSpace(current);
        setDefaultMaxRating((current?.default_max_rating ?? '').trim());
        if (!current) {
          setParentalMembers([]);
          setMemberRatingDrafts({});
          return;
        }
        const members = await listSpaceMembers(current.id);
        if (!active) return;
        setParentalMembers(members);
        const drafts: Record<number, string> = {};
        for (const m of members) {
          drafts[m.user_id] = (m.max_rating ?? '').trim();
        }
        setMemberRatingDrafts(drafts);
      } catch (err) {
        if (active) setParentalError(extractErrorMessage(err, '加载家长控制策略失败'));
      } finally {
        if (active) setParentalLoading(false);
      }
    })();
    return () => {
      active = false;
    };
  }, []);

  // 加载用户列表 + 可访问 Space（FR2-010）；用户列表仅默认 Space owner 可调，403 时仅隐藏用户管理
  const reloadUsersAndSpaces = useCallback(async () => {
    setUsersSpacesLoading(true);
    setUsersSpacesError(null);
    setUsersForbidden(false);
    try {
      const spaces = await listSpaces();
      setManagedSpaces(spaces);
      if (!memberSpaceID && spaces[0]) {
        setMemberSpaceID(spaces[0].id);
      }
      try {
        const users = await listUsers();
        setManagedUsers(users);
      } catch (err) {
        // 非默认 Space owner 禁止列用户：不阻塞 Space 管理
        const status =
          (err as { status?: number })?.status ??
          (err as { cause?: { response?: { status?: number } } })?.cause?.response?.status;
        const msg = extractErrorMessage(err, '加载用户列表失败');
        if (
          status === 403 ||
          /FORBIDDEN|仅默认 Space owner/i.test(msg) ||
          msg.includes('仅默认')
        ) {
          setUsersForbidden(true);
          setManagedUsers([]);
        } else {
          throw err;
        }
      }
    } catch (err) {
      setUsersSpacesError(extractErrorMessage(err, '加载用户与 Space 失败'));
    } finally {
      setUsersSpacesLoading(false);
    }
  }, [memberSpaceID]);

  useEffect(() => {
    void reloadUsersAndSpaces();
    // 仅挂载时拉取一次
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // 选中 Space 变化时加载其成员列表（FR2-010）
  useEffect(() => {
    if (!memberSpaceID) {
      setMemberList([]);
      return;
    }
    let active = true;
    setMemberListLoading(true);
    listSpaceMembers(memberSpaceID)
      .then((items) => {
        if (active) setMemberList(items);
      })
      .catch(() => {
        if (active) setMemberList([]);
      })
      .finally(() => {
        if (active) setMemberListLoading(false);
      });
    return () => {
      active = false;
    };
  }, [memberSpaceID]);

  // 挂载时加载现有设置
  useEffect(() => {
    let active = true;
    setLoading(true);
    setLoadError(null);
    Promise.all([getSettings(), getSettingDefinitions()])
      .then(([data, defs]) => {
        if (!active) return;
        setDefinitions(Object.fromEntries(defs.map((def) => [def.key, def])));
        setScanInterval(data[SETTING_KEY_SCAN_INTERVAL] ?? '');
        setRecycleBinRows(parseRecycleBinRows(data[SETTING_KEY_RECYCLE_BIN_PATHS] ?? ''));
        setRecycleRetentionDays(data[SETTING_KEY_RECYCLE_RETENTION_DAYS] ?? '30');
        setRecycleAutoCleanupEnabled(
          parseBooleanSetting(data[SETTING_KEY_RECYCLE_AUTO_CLEANUP_ENABLED], true),
        );
        setRecycleAutoCleanupIntervalSec(
          data[SETTING_KEY_RECYCLE_AUTO_CLEANUP_INTERVAL_SEC] ?? '3600',
        );
        setFfmpegPath(data[SETTING_KEY_FFMPEG_PATH] ?? '');
        setFfprobePath(data[SETTING_KEY_FFPROBE_PATH] ?? '');
        setMagickPath(data[SETTING_KEY_MAGICK_PATH] ?? '');
        const proxy = data[SETTING_KEY_NETWORK_PROXY] ?? '';
        setNetworkProxy(proxy === SETTING_SENSITIVE_DISPLAY_VALUE ? '' : proxy);
        setNetworkProxyMasked(proxy === SETTING_SENSITIVE_DISPLAY_VALUE);
        setDebugLog(parseBooleanSetting(data[SETTING_KEY_DEBUG_LOG], false));
        setMediaInferenceEnabled(
          parseBooleanSetting(data[SETTING_KEY_MEDIA_INFERENCE_ENABLED], true),
        );
        setInferenceDisabledLibraries(
          parseDisabledInferenceLibraries(data[SETTING_KEY_MEDIA_INFERENCE_DISABLED_LIBRARIES]),
        );
        setUploadTargetDir(data[SETTING_KEY_UPLOAD_TARGET_DIR] ?? '');
        setUploadNamingRule(data[SETTING_KEY_UPLOAD_NAMING_RULE] ?? '');
      })
      .catch((err) => {
        if (active) setLoadError(extractErrorMessage(err, '加载设置失败'));
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, []);

  // 挂载时加载本地库目录（FR-149）：供「默认上传位置」下拉，失败静默（仍可手填后端校验）
  useEffect(() => {
    let active = true;
    getLibraryPaths()
      .then((paths) => {
        if (!active) return;
        setInferenceLibraries(paths.filter((p) => p.enabled));
        setLocalLibraryPaths(paths.filter((p) => p.type === 'local' && p.enabled));
      })
      .catch(() => {
        /* 拉取库目录失败不阻塞设置页 */
      });
    return () => {
      active = false;
    };
  }, []);

  // 挂载时加载环境变量（只读）
  useEffect(() => {
    let active = true;
    setEnvLoading(true);
    setEnvError(null);
    getEnvVars()
      .then((data) => {
        if (active) setEnvVars(data);
      })
      .catch((err) => {
        if (active) setEnvError(extractErrorMessage(err, '加载环境变量失败'));
      })
      .finally(() => {
        if (active) setEnvLoading(false);
      });
    return () => {
      active = false;
    };
  }, []);

  // 工具下载状态（FR2-022）：读取可下载源与当前安装状态，下载任务走通用任务中心。
  useEffect(() => {
    let active = true;
    setToolsLoading(true);
    setToolsError(null);
    Promise.all([getTools(), getToolSources()])
      .then(([statuses, sources]) => {
        if (!active) return;
        setToolStatuses(statuses);
        setToolSources(sources);
      })
      .catch((err) => {
        if (active) setToolsError(extractErrorMessage(err, '加载工具下载信息失败'));
      })
      .finally(() => {
        if (active) setToolsLoading(false);
      });
    return () => {
      active = false;
    };
  }, []);

  // 回收站行编辑器操作
  const addRecycleRow = useCallback(() => {
    setRecycleBinRows((rows) => [...rows, { drive: '', path: '' }]);
  }, []);
  const removeRecycleRow = useCallback((index: number) => {
    setRecycleBinRows((rows) => rows.filter((_, i) => i !== index));
  }, []);
  const updateRecycleRow = useCallback((index: number, patch: Partial<RecycleBinRow>) => {
    setRecycleBinRows((rows) => rows.map((row, i) => (i === index ? { ...row, ...patch } : row)));
  }, []);

  // 回收站行校验结果，驱动行内错误展示与提交拦截
  const recycleValidation = validateRecycleBinRows(recycleBinRows);

  const settingLabel = useCallback(
    (key: string, fallback: string) => definitions[key]?.label ?? fallback,
    [definitions],
  );
  const settingDescription = useCallback(
    (key: string, fallback: string) => definitions[key]?.description ?? fallback,
    [definitions],
  );
  const selectedToolSources = useMemo(
    () => toolSources.filter((source) => source.tool === selectedTool),
    [toolSources, selectedTool],
  );
  const downloadableSources = useMemo(
    () => selectedToolSources.filter((source) => source.sha256),
    [selectedToolSources],
  );
  const selectedSource = useMemo(
    () => selectedToolSources.find((source) => source.id === selectedSourceID) ?? null,
    [selectedToolSources, selectedSourceID],
  );
  const selectedToolStatus = useMemo(
    () => toolStatuses.find((status) => status.tool === selectedTool) ?? null,
    [toolStatuses, selectedTool],
  );

  useEffect(() => {
    setSelectedSourceID((current) => {
      if (current && downloadableSources.some((source) => source.id === current)) return current;
      return downloadableSources[0]?.id ?? null;
    });
  }, [downloadableSources]);

  const refreshToolTask = useCallback(async () => {
    const page = await listTasks({
      scope: 'system',
      type: 'tool.download',
      resource_type: 'tool',
      resource_id: selectedTool,
      page_size: 1,
    });
    setLatestToolTask(page.items[0] ?? null);
  }, [selectedTool]);

  useEffect(() => {
    setLatestToolTask(null);
    setDownloadTaskID('');
    refreshToolTask().catch(() => {
      /* 工具任务为空或加载失败不阻塞设置页 */
    });
  }, [refreshToolTask]);

  useEffect(() => {
    if (
      !downloadTaskID &&
      latestToolTask?.status !== 'pending' &&
      latestToolTask?.status !== 'running'
    ) {
      return;
    }
    const timer = window.setInterval(() => {
      refreshToolTask().catch(() => {
        /* 轮询失败时保持现有任务状态 */
      });
    }, 1200);
    return () => window.clearInterval(timer);
  }, [downloadTaskID, latestToolTask?.status, refreshToolTask]);

  const handleSave = useCallback(async () => {
    // 回收站行非法（空盘符/重复盘符）时行内提示并阻止提交
    const validation = validateRecycleBinRows(recycleBinRows);
    if (!validation.valid) {
      notifications.show({
        title: '回收站路径有误',
        message: '请修正盘符为空或重复的行后再保存',
        color: 'red',
        autoClose: 4000,
      });
      return;
    }
    setSaving(true);
    try {
      const payload: SettingsMap = {
        [SETTING_KEY_SCAN_INTERVAL]: scanInterval,
        [SETTING_KEY_RECYCLE_BIN_PATHS]: serializeRecycleBinRows(recycleBinRows),
        [SETTING_KEY_RECYCLE_RETENTION_DAYS]: recycleRetentionDays,
        [SETTING_KEY_RECYCLE_AUTO_CLEANUP_ENABLED]: recycleAutoCleanupEnabled ? '1' : '0',
        [SETTING_KEY_RECYCLE_AUTO_CLEANUP_INTERVAL_SEC]: recycleAutoCleanupIntervalSec,
        [SETTING_KEY_FFMPEG_PATH]: ffmpegPath,
        [SETTING_KEY_FFPROBE_PATH]: ffprobePath,
        [SETTING_KEY_MAGICK_PATH]: magickPath,
        [SETTING_KEY_DEBUG_LOG]: debugLog ? '1' : '0',
        [SETTING_KEY_MEDIA_INFERENCE_ENABLED]: mediaInferenceEnabled ? '1' : '0',
        [SETTING_KEY_MEDIA_INFERENCE_DISABLED_LIBRARIES]: serializeDisabledInferenceLibraries(
          inferenceDisabledLibraries,
        ),
        [SETTING_KEY_UPLOAD_TARGET_DIR]: uploadTargetDir,
        [SETTING_KEY_UPLOAD_NAMING_RULE]: uploadNamingRule,
      };
      if (!networkProxyMasked || networkProxy.trim() !== '') {
        payload[SETTING_KEY_NETWORK_PROXY] = networkProxy;
      }
      const updated = await updateSettings(payload);
      // 以回读结果刷新输入框，确保展示与持久化一致
      setScanInterval(updated[SETTING_KEY_SCAN_INTERVAL] ?? '');
      setRecycleBinRows(parseRecycleBinRows(updated[SETTING_KEY_RECYCLE_BIN_PATHS] ?? ''));
      setRecycleRetentionDays(updated[SETTING_KEY_RECYCLE_RETENTION_DAYS] ?? '30');
      setRecycleAutoCleanupEnabled(
        parseBooleanSetting(updated[SETTING_KEY_RECYCLE_AUTO_CLEANUP_ENABLED], true),
      );
      setRecycleAutoCleanupIntervalSec(
        updated[SETTING_KEY_RECYCLE_AUTO_CLEANUP_INTERVAL_SEC] ?? '3600',
      );
      setFfmpegPath(updated[SETTING_KEY_FFMPEG_PATH] ?? '');
      setFfprobePath(updated[SETTING_KEY_FFPROBE_PATH] ?? '');
      setMagickPath(updated[SETTING_KEY_MAGICK_PATH] ?? '');
      const nextProxy = updated[SETTING_KEY_NETWORK_PROXY] ?? '';
      setNetworkProxy(nextProxy === SETTING_SENSITIVE_DISPLAY_VALUE ? '' : nextProxy);
      setNetworkProxyMasked(nextProxy === SETTING_SENSITIVE_DISPLAY_VALUE);
      setDebugLog(parseBooleanSetting(updated[SETTING_KEY_DEBUG_LOG], false));
      setMediaInferenceEnabled(
        parseBooleanSetting(updated[SETTING_KEY_MEDIA_INFERENCE_ENABLED], true),
      );
      setInferenceDisabledLibraries(
        parseDisabledInferenceLibraries(updated[SETTING_KEY_MEDIA_INFERENCE_DISABLED_LIBRARIES]),
      );
      setUploadTargetDir(updated[SETTING_KEY_UPLOAD_TARGET_DIR] ?? '');
      setUploadNamingRule(updated[SETTING_KEY_UPLOAD_NAMING_RULE] ?? '');
      notifications.show({
        title: '保存成功',
        message: '设置已保存',
        color: 'green',
        autoClose: 3000,
      });
    } catch (err) {
      notifications.show({
        title: '保存失败',
        message: extractErrorMessage(err, '保存设置失败'),
        color: 'red',
        autoClose: 4000,
      });
    } finally {
      setSaving(false);
    }
  }, [
    scanInterval,
    recycleBinRows,
    recycleRetentionDays,
    recycleAutoCleanupEnabled,
    recycleAutoCleanupIntervalSec,
    ffmpegPath,
    ffprobePath,
    magickPath,
    networkProxy,
    debugLog,
    mediaInferenceEnabled,
    inferenceDisabledLibraries,
    uploadTargetDir,
    uploadNamingRule,
    networkProxyMasked,
  ]);

  // 检测当前输入的 ffmpeg 路径是否可用（保存前先验）
  const handleDetect = useCallback(async () => {
    setDetecting(true);
    setDetectResult(null);
    try {
      const res = await detectFFmpeg(ffmpegPath);
      setDetectResult({ available: res.ffmpeg_available, version: res.ffmpeg_version });
    } catch (err) {
      notifications.show({
        title: '检测失败',
        message: extractErrorMessage(err, '检测 FFmpeg 路径失败'),
        color: 'red',
        autoClose: 4000,
      });
    } finally {
      setDetecting(false);
    }
  }, [ffmpegPath]);

  const handleDownloadTool = useCallback(async () => {
    const customURLValue = customURL.trim();
    const customSHAValue = customSHA256.trim();
    if (!customURLValue && !selectedSourceID) {
      notifications.show({
        title: '下载源缺失',
        message: '请选择内置来源或填写自定义下载 URL',
        color: 'red',
        autoClose: 4000,
      });
      return;
    }
    if (customURLValue && !customSHAValue) {
      notifications.show({
        title: '校验值缺失',
        message: '自定义下载 URL 必须填写 SHA-256',
        color: 'red',
        autoClose: 4000,
      });
      return;
    }
    setDownloadingTool(true);
    try {
      const result = await downloadTool({
        tool: selectedTool,
        source_id: customURLValue ? undefined : (selectedSourceID ?? undefined),
        custom_url: customURLValue || undefined,
        sha256: customURLValue ? customSHAValue : undefined,
        version: customURLValue ? customVersion.trim() || undefined : undefined,
        allow_insecure_http: customURLValue ? allowLocalHTTP : undefined,
      });
      setDownloadTaskID(result.task_id);
      await refreshToolTask();
      notifications.show({
        title: '下载已入队',
        message: `任务 ${result.task_id} 已创建`,
        color: 'green',
        autoClose: 3000,
      });
    } catch (err) {
      notifications.show({
        title: '下载失败',
        message: extractErrorMessage(err, '工具下载入队失败'),
        color: 'red',
        autoClose: 4000,
      });
    } finally {
      setDownloadingTool(false);
    }
  }, [
    allowLocalHTTP,
    customSHA256,
    customURL,
    customVersion,
    refreshToolTask,
    selectedSourceID,
    selectedTool,
  ]);

  // 测试当前输入的代理是否可达（保存前先验；后端用临时 client 探测，不改运行期代理）
  const handleTestProxy = useCallback(async () => {
    setTestingProxy(true);
    setProxyTestResult(null);
    try {
      const res = await testProxy(networkProxy);
      setProxyTestResult({ reachable: res.reachable, detail: res.detail });
    } catch (err) {
      notifications.show({
        title: '测试失败',
        message: extractErrorMessage(err, '测试代理连通性失败'),
        color: 'red',
        autoClose: 4000,
      });
    } finally {
      setTestingProxy(false);
    }
  }, [networkProxy]);

  const handleChangePassword = useCallback(async () => {
    setPwError(null);
    if (newPassword.length < 6) {
      setPwError('新密码至少 6 位');
      return;
    }
    if (newPassword !== confirmPassword) {
      setPwError('两次输入的新密码不一致');
      return;
    }
    setChangingPassword(true);
    try {
      await changePassword(currentPassword, newPassword);
      setCurrentPassword('');
      setNewPassword('');
      setConfirmPassword('');
      notifications.show({
        title: '修改成功',
        message: '密码已更新',
        color: 'green',
        autoClose: 2000,
      });
    } catch (err) {
      setPwError(extractErrorMessage(err, '修改密码失败'));
    } finally {
      setChangingPassword(false);
    }
  }, [currentPassword, newPassword, confirmPassword]);

  // 保存 Space 默认最高可见分级（FR2-051，需密码确认）
  const handleSaveParentalDefault = useCallback(async () => {
    if (!parentalSpace) return;
    if (!parentalPassword.trim()) {
      notifications.show({
        color: 'orange',
        message: '修改家长策略需输入当前账户密码确认',
        autoClose: 3000,
      });
      return;
    }
    setParentalSaving(true);
    try {
      await updateSpaceParental(parentalSpace.id, parentalPassword, defaultMaxRating);
      setParentalPassword('');
      setParentalSpace((prev) =>
        prev ? { ...prev, default_max_rating: defaultMaxRating } : prev,
      );
      notifications.show({
        color: 'green',
        title: '已保存',
        message: 'Space 默认最高可见分级已更新',
        autoClose: 2500,
      });
    } catch (err) {
      notifications.show({
        color: 'red',
        title: '保存失败',
        message: extractErrorMessage(err, '更新家长控制策略失败'),
        autoClose: 4000,
      });
    } finally {
      setParentalSaving(false);
    }
  }, [parentalSpace, parentalPassword, defaultMaxRating]);

  // 创建用户（FR2-010，默认 Space owner）
  const handleCreateUser = useCallback(async () => {
    const name = newUsername.trim();
    if (!name || newUserPassword.length < 6) {
      notifications.show({
        color: 'orange',
        message: '用户名必填，密码至少 6 位',
        autoClose: 3000,
      });
      return;
    }
    setCreatingUser(true);
    try {
      await createUser(name, newUserPassword);
      setNewUsername('');
      setNewUserPassword('');
      notifications.show({ color: 'green', title: '已创建', message: `用户「${name}」已创建` });
      const users = await listUsers();
      setManagedUsers(users);
    } catch (err) {
      notifications.show({
        color: 'red',
        title: '创建失败',
        message: extractErrorMessage(err, '创建用户失败'),
      });
    } finally {
      setCreatingUser(false);
    }
  }, [newUsername, newUserPassword]);

  // 启用/禁用用户（FR2-010）
  const handleToggleUserStatus = useCallback(async (u: ManagedUser) => {
    const next = u.status === 'disabled' ? 'active' : 'disabled';
    setUserStatusBusyID(u.id);
    try {
      await setUserStatus(u.id, next);
      setManagedUsers((prev) => prev.map((x) => (x.id === u.id ? { ...x, status: next } : x)));
      notifications.show({
        color: 'green',
        message: next === 'disabled' ? `已禁用「${u.username}」` : `已启用「${u.username}」`,
      });
    } catch (err) {
      notifications.show({
        color: 'red',
        message: extractErrorMessage(err, '更新用户状态失败'),
      });
    } finally {
      setUserStatusBusyID(null);
    }
  }, []);

  // 创建 Space（FR2-010）
  const handleCreateSpace = useCallback(async () => {
    const id = newSpaceID.trim();
    const name = newSpaceName.trim();
    if (!id || !name) {
      notifications.show({ color: 'orange', message: 'Space ID 与名称必填', autoClose: 3000 });
      return;
    }
    setCreatingSpace(true);
    try {
      const sp = await createSpace(id, name);
      setNewSpaceID('');
      setNewSpaceName('');
      notifications.show({ color: 'green', title: '已创建', message: `Space「${sp.name}」已创建` });
      const spaces = await listSpaces();
      setManagedSpaces(spaces);
      setMemberSpaceID(sp.id);
    } catch (err) {
      notifications.show({
        color: 'red',
        title: '创建失败',
        message: extractErrorMessage(err, '创建 Space 失败'),
      });
    } finally {
      setCreatingSpace(false);
    }
  }, [newSpaceID, newSpaceName]);

  // 添加/更新成员角色（FR2-010，当前选中 Space 的 owner）
  const handleAddMember = useCallback(async () => {
    if (!memberSpaceID) return;
    const uname = memberUsername.trim();
    if (!uname) {
      notifications.show({ color: 'orange', message: '请填写用户名', autoClose: 3000 });
      return;
    }
    setAddingMember(true);
    try {
      await addSpaceMember(memberSpaceID, { username: uname, role: memberRole });
      setMemberUsername('');
      notifications.show({ color: 'green', message: `已将「${uname}」设为 ${memberRole}` });
      const items = await listSpaceMembers(memberSpaceID);
      setMemberList(items);
    } catch (err) {
      notifications.show({
        color: 'red',
        message: extractErrorMessage(err, '添加成员失败'),
      });
    } finally {
      setAddingMember(false);
    }
  }, [memberSpaceID, memberUsername, memberRole]);

  // 移除成员（FR2-010）
  const handleRemoveMember = useCallback(
    async (userID: number) => {
      if (!memberSpaceID) return;
      const key = `${memberSpaceID}:${userID}`;
      setRemovingMemberKey(key);
      try {
        await removeSpaceMember(memberSpaceID, userID);
        notifications.show({ color: 'green', message: '已移除成员' });
        const items = await listSpaceMembers(memberSpaceID);
        setMemberList(items);
      } catch (err) {
        notifications.show({
          color: 'red',
          message: extractErrorMessage(err, '移除成员失败'),
        });
      } finally {
        setRemovingMemberKey(null);
      }
    },
    [memberSpaceID],
  );

  // 保存成员最高可见分级（FR2-051，需密码确认）
  const handleSaveMemberRating = useCallback(
    async (userID: number) => {
      if (!parentalSpace) return;
      if (!parentalPassword.trim()) {
        notifications.show({
          color: 'orange',
          message: '修改成员可见分级需输入当前账户密码确认',
          autoClose: 3000,
        });
        return;
      }
      const rating = memberRatingDrafts[userID] ?? '';
      setMemberSavingID(userID);
      try {
        await updateMemberMaxRating(parentalSpace.id, userID, parentalPassword, rating);
        setParentalPassword('');
        setParentalMembers((prev) =>
          prev.map((m) =>
            m.user_id === userID ? { ...m, max_rating: rating === '' ? null : rating } : m,
          ),
        );
        notifications.show({
          color: 'green',
          title: '已保存',
          message: `成员 #${userID} 可见分级已更新`,
          autoClose: 2500,
        });
      } catch (err) {
        notifications.show({
          color: 'red',
          title: '保存失败',
          message: extractErrorMessage(err, '更新成员可见分级失败'),
          autoClose: 4000,
        });
      } finally {
        setMemberSavingID(null);
      }
    },
    [parentalSpace, parentalPassword, memberRatingDrafts],
  );

  return (
    <Group align="flex-start" gap="lg" wrap="nowrap">
      {/* 左侧锚点导航（FR-113）：窄屏隐藏、点击滚动定位到对应分区。
          sticky 常驻（FR-113 修复）：内容随页面滚动时锚点列吸顶常驻可见、不随内容滚走；
          position 内联（便于 jsdom 单测断言），top 由 .anchor-nav-sticky 设为「页眉 + 一级 tab 条」高度，
          让开固定页眉与 sticky tab 条（FR-113 第三批修复），max-height + 自滚动防锚点项过多时溢出。 */}
      <Box
        w={160}
        className="anchor-nav-sticky"
        style={{ flexShrink: 0, alignSelf: 'flex-start', position: 'sticky' }}
        visibleFrom="sm"
      >
        <AnchorNav sections={SETTINGS_ANCHORS} />
      </Box>
      <Stack gap="md" style={{ flex: 1, minWidth: 0 }}>
        <Title order={2}>设置</Title>

        <Title id="set-storage" order={3}>
          存储与 Space
        </Title>
        {storageError ? (
          <Alert icon={<IconAlertCircle size={16} />} color="red" title="加载失败">
            {storageError}
          </Alert>
        ) : storageInfo ? (
          <Card withBorder padding="md" radius="md">
            <Stack gap="sm">
              <Group justify="space-between" align="flex-start">
                <Box>
                  <Text size="sm" c="dimmed">
                    当前 Space
                  </Text>
                  <Group gap="xs">
                    <Text fw={600}>{storageInfo.space.name}</Text>
                    <Code>{storageInfo.space.id}</Code>
                    <Badge color="purple">owner</Badge>
                  </Group>
                </Box>
                <Badge variant="light">{storageInfo.library_count} 个已注册目录</Badge>
              </Group>
              <Box>
                <Text size="sm" c="dimmed">
                  存储数据目录
                </Text>
                <Code>{storageInfo.data_dir || '未配置'}</Code>
              </Box>
              <Box>
                <Text size="sm" c="dimmed">
                  SQLite 索引库
                </Text>
                <Code>{storageInfo.database_path || '未配置'}</Code>
              </Box>
              <Text size="xs" c="dimmed">
                原媒体保留在已注册媒体库目录；缩略图、HLS、缓存与 SQLite
                索引库位于存储数据目录，实现原媒体与可重建索引分离。
              </Text>
              <Button
                component={Link}
                to="/library-manager"
                variant="default"
                style={{ alignSelf: 'flex-start' }}
              >
                管理媒体库目录
              </Button>
            </Stack>
          </Card>
        ) : (
          <Skeleton height={190} radius="md" />
        )}

        {/* 账户安全（FR-108）：修改当前登录用户密码，独立于下方运行期设置 */}
        <Title id="set-account" order={3}>
          账户安全
        </Title>
        <Card withBorder padding="md" radius="md">
          <Stack gap="md">
            {pwError && (
              <Alert
                icon={<IconAlertCircle size={16} />}
                color="red"
                withCloseButton
                onClose={() => setPwError(null)}
              >
                {pwError}
              </Alert>
            )}
            <TextInput
              label="当前密码"
              type="password"
              value={currentPassword}
              onChange={(e) => setCurrentPassword(e.currentTarget.value)}
            />
            <TextInput
              label="新密码"
              description="至少 6 位"
              type="password"
              value={newPassword}
              onChange={(e) => setNewPassword(e.currentTarget.value)}
            />
            <TextInput
              label="确认新密码"
              type="password"
              value={confirmPassword}
              onChange={(e) => setConfirmPassword(e.currentTarget.value)}
            />
            <Group>
              <Button
                onClick={handleChangePassword}
                loading={changingPassword}
                disabled={!currentPassword || !newPassword || !confirmPassword}
              >
                修改密码
              </Button>
            </Group>
          </Stack>
        </Card>

        {/* 用户与 Space（FR2-010）：默认 Space owner 管用户；各 Space owner 管成员 */}
        <Title id="set-users-spaces" order={3}>
          用户与 Space
        </Title>
        <Card withBorder padding="md" radius="md">
          {usersSpacesLoading ? (
            <Skeleton height={200} radius="md" />
          ) : usersSpacesError ? (
            <Alert icon={<IconAlertCircle size={16} />} color="orange" title="用户与 Space">
              {usersSpacesError}
            </Alert>
          ) : (
            <Stack gap="lg">
              <Text size="xs" c="dimmed">
                创建用户限默认 Space owner；创建 Space 后创建者为该 Space owner。成员角色仅
                editor / viewer（不可经此界面设 owner）。
              </Text>

              {/* 用户管理：仅默认 Space owner 可见完整表；403 时提示无权限 */}
              <Divider label="用户" labelPosition="left" />
              {usersForbidden ? (
                <Text size="sm" c="dimmed">
                  当前账号不是默认 Space owner，无法管理用户列表。
                </Text>
              ) : (
                <Stack gap="sm">
                  <Table striped highlightOnHover withTableBorder>
                    <Table.Thead>
                      <Table.Tr>
                        <Table.Th>ID</Table.Th>
                        <Table.Th>用户名</Table.Th>
                        <Table.Th>状态</Table.Th>
                        <Table.Th>操作</Table.Th>
                      </Table.Tr>
                    </Table.Thead>
                    <Table.Tbody>
                      {managedUsers.map((u) => (
                        <Table.Tr key={u.id}>
                          <Table.Td>{u.id}</Table.Td>
                          <Table.Td>
                            {u.username}
                            {authUsername === u.username ? (
                              <Badge size="xs" ml={6} variant="light">
                                当前
                              </Badge>
                            ) : null}
                          </Table.Td>
                          <Table.Td>
                            <Badge
                              size="sm"
                              color={u.status === 'disabled' ? 'red' : 'teal'}
                              variant="light"
                            >
                              {u.status === 'disabled' ? '已禁用' : 'active'}
                            </Badge>
                          </Table.Td>
                          <Table.Td>
                            <Button
                              size="xs"
                              variant="default"
                              loading={userStatusBusyID === u.id}
                              disabled={authUsername === u.username}
                              onClick={() => void handleToggleUserStatus(u)}
                              aria-label={
                                u.status === 'disabled'
                                  ? `启用用户 ${u.username}`
                                  : `禁用用户 ${u.username}`
                              }
                            >
                              {u.status === 'disabled' ? '启用' : '禁用'}
                            </Button>
                          </Table.Td>
                        </Table.Tr>
                      ))}
                    </Table.Tbody>
                  </Table>
                  <Group align="flex-end" gap="sm" wrap="wrap">
                    <TextInput
                      label="新用户名"
                      aria-label="新用户名"
                      value={newUsername}
                      onChange={(e) => setNewUsername(e.currentTarget.value)}
                      style={{ minWidth: 140 }}
                    />
                    <TextInput
                      label="初始密码"
                      aria-label="新用户初始密码"
                      type="password"
                      description="至少 6 位"
                      value={newUserPassword}
                      onChange={(e) => setNewUserPassword(e.currentTarget.value)}
                      style={{ minWidth: 160 }}
                    />
                    <Button
                      leftSection={<IconPlus size={14} />}
                      loading={creatingUser}
                      onClick={() => void handleCreateUser()}
                    >
                      创建用户
                    </Button>
                  </Group>
                </Stack>
              )}

              {/* Space 列表与创建 */}
              <Divider label="Space" labelPosition="left" />
              <Stack gap="sm">
                {managedSpaces.length === 0 ? (
                  <Text size="sm" c="dimmed">
                    暂无可用 Space
                  </Text>
                ) : (
                  <Table striped withTableBorder>
                    <Table.Thead>
                      <Table.Tr>
                        <Table.Th>ID</Table.Th>
                        <Table.Th>名称</Table.Th>
                        <Table.Th>我的角色</Table.Th>
                      </Table.Tr>
                    </Table.Thead>
                    <Table.Tbody>
                      {managedSpaces.map((sp) => (
                        <Table.Tr key={sp.id}>
                          <Table.Td>
                            <Code>{sp.id}</Code>
                          </Table.Td>
                          <Table.Td>{sp.name}</Table.Td>
                          <Table.Td>
                            <Badge size="sm" variant="light">
                              {sp.role || '—'}
                            </Badge>
                          </Table.Td>
                        </Table.Tr>
                      ))}
                    </Table.Tbody>
                  </Table>
                )}
                <Group align="flex-end" gap="sm" wrap="wrap">
                  <TextInput
                    label="Space ID"
                    aria-label="新 Space ID"
                    description="字母数字与 . _ -"
                    value={newSpaceID}
                    onChange={(e) => setNewSpaceID(e.currentTarget.value)}
                    style={{ minWidth: 160 }}
                  />
                  <TextInput
                    label="名称"
                    aria-label="新 Space 名称"
                    value={newSpaceName}
                    onChange={(e) => setNewSpaceName(e.currentTarget.value)}
                    style={{ minWidth: 140 }}
                  />
                  <Button
                    leftSection={<IconPlus size={14} />}
                    loading={creatingSpace}
                    onClick={() => void handleCreateSpace()}
                  >
                    创建 Space
                  </Button>
                </Group>
              </Stack>

              {/* 成员管理：选 Space → 加成员 / 改角色 / 移除 */}
              <Divider label="成员" labelPosition="left" />
              <Stack gap="sm">
                <Select
                  label="管理成员的 Space"
                  aria-label="管理成员的 Space"
                  data={managedSpaces.map((sp) => ({
                    value: sp.id,
                    label: `${sp.name} (${sp.id})`,
                  }))}
                  value={memberSpaceID || null}
                  onChange={(v) => setMemberSpaceID(v ?? '')}
                  allowDeselect={false}
                  placeholder={managedSpaces.length === 0 ? '暂无 Space' : '选择 Space'}
                />
                {memberListLoading ? (
                  <Skeleton height={80} radius="md" />
                ) : memberSpaceID ? (
                  memberList.length === 0 ? (
                    <Text size="sm" c="dimmed">
                      暂无成员
                    </Text>
                  ) : (
                    <Table striped withTableBorder>
                      <Table.Thead>
                        <Table.Tr>
                          <Table.Th>用户 ID</Table.Th>
                          <Table.Th>角色</Table.Th>
                          <Table.Th>操作</Table.Th>
                        </Table.Tr>
                      </Table.Thead>
                      <Table.Tbody>
                        {memberList.map((m) => (
                          <Table.Tr key={`${m.space_id}-${m.user_id}`}>
                            <Table.Td>#{m.user_id}</Table.Td>
                            <Table.Td>
                              <Badge size="sm" variant="light">
                                {m.role}
                              </Badge>
                            </Table.Td>
                            <Table.Td>
                              {m.role === 'owner' ? (
                                <Text size="xs" c="dimmed">
                                  owner 不可移除
                                </Text>
                              ) : (
                                <Button
                                  size="xs"
                                  color="red"
                                  variant="light"
                                  loading={removingMemberKey === `${m.space_id}:${m.user_id}`}
                                  onClick={() => void handleRemoveMember(m.user_id)}
                                  aria-label={`移除成员 ${m.user_id}`}
                                >
                                  移除
                                </Button>
                              )}
                            </Table.Td>
                          </Table.Tr>
                        ))}
                      </Table.Tbody>
                    </Table>
                  )
                ) : null}
                <Group align="flex-end" gap="sm" wrap="wrap">
                  <TextInput
                    label="用户名"
                    aria-label="添加成员用户名"
                    value={memberUsername}
                    onChange={(e) => setMemberUsername(e.currentTarget.value)}
                    style={{ minWidth: 140 }}
                  />
                  <Select
                    label="角色"
                    aria-label="添加成员角色"
                    data={MEMBER_ROLE_OPTIONS}
                    value={memberRole}
                    onChange={(v) => setMemberRole(v ?? 'viewer')}
                    allowDeselect={false}
                    style={{ minWidth: 160 }}
                  />
                  <Button
                    loading={addingMember}
                    disabled={!memberSpaceID}
                    onClick={() => void handleAddMember()}
                  >
                    添加/更新成员
                  </Button>
                </Group>
              </Stack>
            </Stack>
          )}
        </Card>

        {/* 家长控制（FR2-051）：Space 默认最高可见级 + 成员覆盖；改策略需密码确认 */}
        <Title id="set-parental" order={3}>
          家长控制
        </Title>
        <Card withBorder padding="md" radius="md">
          {parentalLoading ? (
            <Skeleton height={160} radius="md" />
          ) : parentalError ? (
            <Alert icon={<IconAlertCircle size={16} />} color="orange" title="家长控制">
              {parentalError}
            </Alert>
          ) : !parentalSpace ? (
            <Text size="sm" c="dimmed">
              暂无可用 Space，无法配置家长控制。
            </Text>
          ) : (
            <Stack gap="md">
              <Text size="xs" c="dimmed">
                限制成员在本 Space 可见的最高内容分级。成员覆盖优先于 Space
                默认；皆为空表示不限制。修改策略需输入当前账户密码确认（家长锁）。
              </Text>
              <Group gap="xs">
                <Text size="sm" c="dimmed">
                  当前 Space
                </Text>
                <Text size="sm" fw={600}>
                  {parentalSpace.name}
                </Text>
                <Code>{parentalSpace.id}</Code>
                {parentalSpace.role && (
                  <Badge size="sm" variant="light">
                    {parentalSpace.role}
                  </Badge>
                )}
              </Group>
              <Select
                label="Space 默认最高可见分级"
                aria-label="Space 默认最高可见分级"
                description="空=不限制；成员无个人覆盖时生效"
                data={MAX_RATING_OPTIONS}
                value={defaultMaxRating}
                onChange={(v) => setDefaultMaxRating(v ?? '')}
                allowDeselect={false}
              />
              <TextInput
                label="确认密码（家长锁）"
                aria-label="家长控制确认密码"
                type="password"
                placeholder="修改策略时必填"
                description="仅本次提交使用，不会保存"
                value={parentalPassword}
                onChange={(e) => setParentalPassword(e.currentTarget.value)}
              />
              <Button
                onClick={() => void handleSaveParentalDefault()}
                loading={parentalSaving}
                style={{ alignSelf: 'flex-start' }}
              >
                保存默认分级
              </Button>
              <Divider label="成员可见分级覆盖" labelPosition="left" />
              {parentalMembers.length === 0 ? (
                <Text size="sm" c="dimmed">
                  暂无成员
                </Text>
              ) : (
                <Stack gap="sm">
                  {parentalMembers.map((m) => (
                    <Group key={m.user_id} gap="sm" align="flex-end" wrap="wrap">
                      <Box style={{ minWidth: 120 }}>
                        <Text size="sm" fw={500}>
                          用户 #{m.user_id}
                        </Text>
                        <Text size="xs" c="dimmed">
                          角色 {m.role}
                        </Text>
                      </Box>
                      <Select
                        aria-label={`成员 ${m.user_id} 最高可见分级`}
                        data={MAX_RATING_OPTIONS}
                        value={memberRatingDrafts[m.user_id] ?? ''}
                        onChange={(v) =>
                          setMemberRatingDrafts((prev) => ({
                            ...prev,
                            [m.user_id]: v ?? '',
                          }))
                        }
                        allowDeselect={false}
                        style={{ flex: 1, minWidth: 180 }}
                      />
                      <Button
                        variant="default"
                        size="sm"
                        loading={memberSavingID === m.user_id}
                        onClick={() => void handleSaveMemberRating(m.user_id)}
                      >
                        保存
                      </Button>
                    </Group>
                  ))}
                </Stack>
              )}
            </Stack>
          )}
        </Card>

        {loadError && (
          <Alert icon={<IconAlertCircle size={16} />} color="red" title="加载失败">
            {loadError}
          </Alert>
        )}

        {loading ? (
          <Skeleton height={400} radius="md" />
        ) : (
          <>
            {/* 扫描分区（FR-24）：定时扫描周期 */}
            <Title id="set-scan" order={3}>
              扫描
            </Title>
            <Card withBorder padding="md" radius="md">
              <TextInput
                label={`${settingLabel(SETTING_KEY_SCAN_INTERVAL, '扫描周期')}（秒）`}
                description={settingDescription(
                  SETTING_KEY_SCAN_INTERVAL,
                  '定时扫描的间隔周期，供后续定时扫描能力消费',
                )}
                value={scanInterval}
                onChange={(e) => setScanInterval(e.currentTarget.value)}
              />
            </Card>

            <Title id="set-inference" order={3}>
              影视信息
            </Title>
            <Card withBorder padding="md" radius="md">
              <Stack gap="sm">
                <Switch
                  label="本地影视信息推断"
                  aria-label="本地影视信息推断"
                  description={settingDescription(
                    SETTING_KEY_MEDIA_INFERENCE_ENABLED,
                    '仅根据文件名和目录离线推断片名、年份与季集信息；重新开启会为尚无结果的已有媒体创建增量刷新任务。',
                  )}
                  checked={mediaInferenceEnabled}
                  onChange={(e) => setMediaInferenceEnabled(e.currentTarget.checked)}
                />
                {inferenceLibraries.length > 0 && (
                  <Stack gap="xs" pl="md">
                    <Text size="sm" fw={500}>
                      按媒体库启用
                    </Text>
                    {inferenceLibraries.map((libraryPath) => (
                      <Switch
                        key={libraryPath.id}
                        size="sm"
                        label={libraryPath.label || libraryPath.path}
                        aria-label={`${libraryPath.label || libraryPath.path}影视信息推断`}
                        checked={!inferenceDisabledLibraries.has(libraryPath.id)}
                        disabled={!mediaInferenceEnabled}
                        onChange={(event) => {
                          const enabled = event.currentTarget.checked;
                          setInferenceDisabledLibraries((current) => {
                            const next = new Set(current);
                            if (enabled) next.delete(libraryPath.id);
                            else next.add(libraryPath.id);
                            return next;
                          });
                        }}
                      />
                    ))}
                  </Stack>
                )}
              </Stack>
            </Card>

            {/* 上传分区（FR-149）：Web 上传默认落盘位置与命名规则；目标须为已注册本地库目录或其子目录 */}
            <Title id="set-upload" order={3}>
              上传
            </Title>
            <Card withBorder padding="md" radius="md">
              <Stack gap="sm">
                <Select
                  label={settingLabel(SETTING_KEY_UPLOAD_TARGET_DIR, '默认上传位置')}
                  description={settingDescription(
                    SETTING_KEY_UPLOAD_TARGET_DIR,
                    'Web 上传缺省落盘目录，须为已注册的本地媒体库目录；上传时仍可临时选择其他位置',
                  )}
                  placeholder={
                    localLibraryPaths.length > 0 ? '选择一个本地库目录' : '暂无本地库目录'
                  }
                  data={localLibraryPaths.map((p) => ({
                    value: p.path,
                    label: p.label ? `${p.label}（${p.path}）` : p.path,
                  }))}
                  value={uploadTargetDir || null}
                  onChange={(v) => setUploadTargetDir(v ?? '')}
                  clearable
                  searchable
                />
                <Select
                  label={settingLabel(SETTING_KEY_UPLOAD_NAMING_RULE, '命名规则')}
                  description={settingDescription(
                    SETTING_KEY_UPLOAD_NAMING_RULE,
                    '保留原样：直接落目标目录；按日期整齐归档：分 年/月 子目录存放',
                  )}
                  data={[
                    { value: 'original', label: '保留原样' },
                    { value: 'date', label: '按日期整齐归档（年/月）' },
                  ]}
                  value={uploadNamingRule || null}
                  onChange={(v) => setUploadNamingRule(v ?? '')}
                  placeholder="保留原样（默认）"
                  clearable
                />
              </Stack>
            </Card>

            {/* 网络分区（FR-80/FR-89）：后端出站网络代理，空=直连；随「保存设置」一并保存、保存即生效；可在保存前先「测试」连通性 */}
            <Title id="set-network" order={3}>
              网络
            </Title>
            <Card withBorder padding="md" radius="md">
              <Stack gap="md">
                <TextInput
                  label={settingLabel(SETTING_KEY_NETWORK_PROXY, '网络代理')}
                  description={
                    networkProxyMasked
                      ? '已保存代理已隐藏；留空不修改，输入新代理将覆盖。'
                      : settingDescription(
                          SETTING_KEY_NETWORK_PROXY,
                          '用于自更新等后端外部网络访问；留空则直连。支持 http/https/socks5/socks5h',
                        )
                  }
                  placeholder={
                    networkProxyMasked
                      ? '已设置，凭据不回显'
                      : '如 http://host:port 或 socks5h://host:port'
                  }
                  value={networkProxy}
                  onChange={(e) => {
                    setNetworkProxy(e.currentTarget.value);
                    setNetworkProxyMasked(false);
                    setProxyTestResult(null);
                  }}
                />
                <Group gap="sm">
                  <Button variant="default" onClick={handleTestProxy} loading={testingProxy}>
                    测试
                  </Button>
                  {networkProxyMasked && (
                    <Button
                      variant="subtle"
                      color="red"
                      onClick={() => {
                        setNetworkProxy('');
                        setNetworkProxyMasked(false);
                      }}
                    >
                      清除已保存代理
                    </Button>
                  )}
                  {proxyTestResult &&
                    (proxyTestResult.reachable ? (
                      <Badge color="green">可达：{proxyTestResult.detail}</Badge>
                    ) : (
                      <Badge color="red">不可达：{proxyTestResult.detail}</Badge>
                    ))}
                </Group>
                <Text size="xs" c="dimmed">
                  「测试」用临时连接探测代理（或直连）对外网目标的可达性，不改动运行期代理；确认可达后再用下方「保存设置」持久化。
                </Text>
              </Stack>
            </Card>

            {/* 工具路径分区（FR-56/FR-63）：ffmpeg/ffprobe/magick 可配置，随「保存设置」一并保存、保存即生效 */}
            <Title id="set-tools" order={3}>
              工具路径
            </Title>
            <Card withBorder padding="md" radius="md">
              <Stack gap="md">
                <TextInput
                  label={settingLabel(SETTING_KEY_FFMPEG_PATH, 'FFmpeg 路径')}
                  description={settingDescription(
                    SETTING_KEY_FFMPEG_PATH,
                    'ffmpeg 可执行文件路径；留空则按环境变量→同目录捆绑版→PATH 自动发现',
                  )}
                  placeholder="如 D:/tools/ffmpeg.exe"
                  value={ffmpegPath}
                  onChange={(e) => {
                    setFfmpegPath(e.currentTarget.value);
                    setDetectResult(null);
                  }}
                />
                <TextInput
                  label={settingLabel(SETTING_KEY_FFPROBE_PATH, 'FFprobe 路径')}
                  description={settingDescription(
                    SETTING_KEY_FFPROBE_PATH,
                    'ffprobe 可执行文件路径；留空则自动发现',
                  )}
                  placeholder="如 D:/tools/ffprobe.exe"
                  value={ffprobePath}
                  onChange={(e) => setFfprobePath(e.currentTarget.value)}
                />
                {/* Magick 路径（FR-63）：HEIC/RAW 转 JPEG 用 */}
                <TextInput
                  label={settingLabel(SETTING_KEY_MAGICK_PATH, 'Magick 路径')}
                  description={settingDescription(
                    SETTING_KEY_MAGICK_PATH,
                    'ImageMagick magick 可执行文件路径，用于 HEIC/RAW 转 JPEG；留空则按环境变量→同目录捆绑版→PATH 自动发现',
                  )}
                  placeholder="如 D:/tools/magick.exe"
                  value={magickPath}
                  onChange={(e) => setMagickPath(e.currentTarget.value)}
                />
                <Group gap="sm">
                  <Button variant="default" onClick={handleDetect} loading={detecting}>
                    检测
                  </Button>
                  {detectResult &&
                    (detectResult.available ? (
                      <Badge color="green">可用：{detectResult.version}</Badge>
                    ) : (
                      <Badge color="red">不可用</Badge>
                    ))}
                </Group>
                <Divider label="自动下载" labelPosition="left" />
                {toolsError && (
                  <Alert icon={<IconAlertCircle size={16} />} color="red" title="加载失败">
                    {toolsError}
                  </Alert>
                )}
                {toolsLoading ? (
                  <Skeleton height={180} radius="md" />
                ) : (
                  <Stack gap="sm">
                    <Group grow align="flex-start">
                      <Select
                        label="工具"
                        data={TOOL_OPTIONS}
                        value={selectedTool}
                        onChange={(value) => {
                          if (value) setSelectedTool(value as ToolName);
                        }}
                        allowDeselect={false}
                      />
                      <Select
                        label="内置来源"
                        data={downloadableSources.map((source) => ({
                          value: source.id,
                          label: source.label,
                        }))}
                        value={selectedSourceID}
                        onChange={setSelectedSourceID}
                        placeholder="暂无可用内置来源"
                        clearable
                      />
                    </Group>
                    {selectedSource && (
                      <Group gap="xs">
                        <Text size="sm" fw={500}>
                          {selectedSource.label}
                        </Text>
                        <Badge variant="light">{selectedSource.version}</Badge>
                        <Badge variant="light">
                          {selectedSource.platform}/{selectedSource.arch}
                        </Badge>
                        <Badge variant="light">{formatToolSize(selectedSource.size)}</Badge>
                        <Text size="xs" c="dimmed">
                          SHA-256 <Code>{selectedSource.sha256.slice(0, 12)}...</Code>
                        </Text>
                      </Group>
                    )}
                    <TextInput
                      label="自定义下载 URL"
                      placeholder="https://example.com/ffmpeg.zip"
                      value={customURL}
                      onChange={(e) => setCustomURL(e.currentTarget.value)}
                    />
                    <TextInput
                      label="自定义 SHA-256"
                      placeholder="64 位 SHA-256"
                      value={customSHA256}
                      onChange={(e) => setCustomSHA256(e.currentTarget.value)}
                    />
                    <Group grow align="flex-start">
                      <TextInput
                        label="自定义版本"
                        placeholder="如 local-test"
                        value={customVersion}
                        onChange={(e) => setCustomVersion(e.currentTarget.value)}
                      />
                      <Switch
                        mt="xl"
                        label="允许本机 HTTP 测试源"
                        checked={allowLocalHTTP}
                        onChange={(e) => setAllowLocalHTTP(e.currentTarget.checked)}
                      />
                    </Group>
                    <Group gap="sm" align="center">
                      <Button
                        leftSection={<IconDownload size={16} />}
                        onClick={handleDownloadTool}
                        loading={downloadingTool}
                        disabled={!customURL.trim() && !selectedSourceID}
                      >
                        下载工具
                      </Button>
                      {selectedToolStatus?.configured_path && (
                        <Badge color="green" variant="light">
                          已配置：{selectedToolStatus.configured_path}
                        </Badge>
                      )}
                    </Group>
                    {latestToolTask && (
                      <Stack gap={4}>
                        <Group gap="xs">
                          <Text size="sm">任务 {latestToolTask.id}</Text>
                          <Badge>{latestToolTask.status}</Badge>
                          {latestToolTask.checkpoint && (
                            <Badge variant="light">{latestToolTask.checkpoint}</Badge>
                          )}
                        </Group>
                        <Progress value={taskProgress(latestToolTask)} />
                        {latestToolTask.error && (
                          <Text size="xs" c="red">
                            {latestToolTask.error}
                          </Text>
                        )}
                      </Stack>
                    )}
                  </Stack>
                )}
                <Text size="xs" c="dimmed">
                  先「检测」验证路径可用，再用下方「保存设置」持久化；保存后即时生效，无需重启。
                </Text>
              </Stack>
            </Card>

            {/* 回收站分区（FR-87 路径 + FR2-054 保留期）：盘符路径与到期自动清理策略 */}
            <Title id="set-recycle" order={3}>
              回收站
            </Title>
            <Card withBorder padding="md" radius="md">
              <Stack gap="sm">
                <Text size="xs" c="dimmed">
                  为各盘符配置回收站目录；删除媒体时按所在盘符移入对应目录。盘符不可为空、不可重复。
                </Text>
                {recycleBinRows.map((row, i) => (
                  <Group key={i} gap="sm" align="flex-start" wrap="nowrap">
                    <TextInput
                      aria-label={`盘符 ${i + 1}`}
                      placeholder="盘符，如 D"
                      style={{ width: 120 }}
                      value={row.drive}
                      error={recycleValidation.rowErrors[i] ?? undefined}
                      onChange={(e) => updateRecycleRow(i, { drive: e.currentTarget.value })}
                    />
                    <TextInput
                      aria-label={`回收站路径 ${i + 1}`}
                      placeholder="回收站目录，如 D:/.recycle"
                      style={{ flex: 1 }}
                      value={row.path}
                      onChange={(e) => updateRecycleRow(i, { path: e.currentTarget.value })}
                    />
                    <ActionIcon
                      variant="subtle"
                      color="red"
                      aria-label={`删除盘符 ${i + 1}`}
                      onClick={() => removeRecycleRow(i)}
                      mt={4}
                    >
                      <IconTrash size={16} />
                    </ActionIcon>
                  </Group>
                ))}
                <Button
                  variant="default"
                  leftSection={<IconPlus size={16} />}
                  onClick={addRecycleRow}
                  style={{ alignSelf: 'flex-start' }}
                >
                  添加盘符
                </Button>

                <Divider my="xs" />

                <Text size="xs" c="dimmed">
                  保留期与自动清理（FR2-054）：到期后将源文件移入对应盘符回收站目录并移除库内记录；保留天数为
                  0 或关闭开关时仅可手动清理。
                </Text>
                <TextInput
                  label={settingLabel(SETTING_KEY_RECYCLE_RETENTION_DAYS, '回收站保留天数')}
                  description={settingDescription(
                    SETTING_KEY_RECYCLE_RETENTION_DAYS,
                    '软删媒体保留天数，超过后可由自动清理处理；0 表示不自动清理。',
                  )}
                  aria-label="回收站保留天数"
                  type="number"
                  min={0}
                  value={recycleRetentionDays}
                  onChange={(e) => setRecycleRetentionDays(e.currentTarget.value)}
                />
                <Switch
                  label={settingLabel(SETTING_KEY_RECYCLE_AUTO_CLEANUP_ENABLED, '回收站自动清理')}
                  aria-label="回收站自动清理"
                  description={settingDescription(
                    SETTING_KEY_RECYCLE_AUTO_CLEANUP_ENABLED,
                    '是否启用到期自动清理；关闭后仅可手动清理。',
                  )}
                  checked={recycleAutoCleanupEnabled}
                  onChange={(e) => setRecycleAutoCleanupEnabled(e.currentTarget.checked)}
                />
                <TextInput
                  label={settingLabel(
                    SETTING_KEY_RECYCLE_AUTO_CLEANUP_INTERVAL_SEC,
                    '回收站自动清理周期（秒）',
                  )}
                  description={settingDescription(
                    SETTING_KEY_RECYCLE_AUTO_CLEANUP_INTERVAL_SEC,
                    '自动清理调度间隔秒数，0 表示关闭定时（仍可手动触发）。',
                  )}
                  aria-label="回收站自动清理周期（秒）"
                  type="number"
                  min={0}
                  value={recycleAutoCleanupIntervalSec}
                  onChange={(e) => setRecycleAutoCleanupIntervalSec(e.currentTarget.value)}
                  disabled={!recycleAutoCleanupEnabled}
                />
              </Stack>
            </Card>

            {/* 诊断分区（FR-110）：运行时调试日志开关，开启输出 GORM 详细 SQL/慢查询日志，随「保存设置」一并保存、保存即生效 */}
            <Title id="set-diagnostics" order={3}>
              诊断
            </Title>
            <Card withBorder padding="md" radius="md">
              <Stack gap="xs">
                <Switch
                  label="调试日志"
                  aria-label="调试日志"
                  description={settingDescription(
                    SETTING_KEY_DEBUG_LOG,
                    '开启后输出数据库 SQL 与慢查询详细日志，便于排查问题；默认关闭以保持日志安静。保存后即时生效、重启保留。',
                  )}
                  checked={debugLog}
                  onChange={(e) => setDebugLog(e.currentTarget.checked)}
                />
              </Stack>
            </Card>

            <Text size="xs" c="dimmed">
              设置保存到数据库，运行期可改、重启后保留。
            </Text>
            <Button
              color="purple"
              onClick={handleSave}
              loading={saving}
              style={{ alignSelf: 'flex-start' }}
            >
              保存设置
            </Button>
          </>
        )}

        {/* 环境变量（FR-56）：只读查看，敏感项脱敏 */}
        <Title id="set-env" order={3}>
          环境变量
        </Title>
        {envError && (
          <Alert icon={<IconAlertCircle size={16} />} color="red" title="加载失败">
            {envError}
          </Alert>
        )}
        {envLoading ? (
          <Skeleton height={200} radius="md" />
        ) : (
          <Card withBorder padding="md" radius="md">
            <Stack gap="sm">
              <Text size="xs" c="dimmed">
                环境变量为进程级，仅供查看；如需修改请调整启动配置后重启。敏感项不显示明文。
              </Text>
              {/* JWT_SECRET 未设警示（FR-87）：未设置时启动随机生成、每次重启需重新登录 */}
              {envVars.some((ev) => ev.key === 'JWT_SECRET' && !ev.set) && (
                <Alert icon={<IconAlertCircle size={16} />} color="yellow" title="会话密钥未持久化">
                  未设置
                  JWT_SECRET，启动时会随机生成会话签名密钥，导致每次重启后所有用户需重新登录。建议在启动环境中设置
                  JWT_SECRET 环境变量以持久化会话。
                </Alert>
              )}
              <Table striped withTableBorder>
                <Table.Thead>
                  <Table.Tr>
                    <Table.Th>变量名</Table.Th>
                    <Table.Th>用途</Table.Th>
                    <Table.Th>值</Table.Th>
                  </Table.Tr>
                </Table.Thead>
                <Table.Tbody>
                  {envVars.map((ev) => (
                    <Table.Tr key={ev.key}>
                      <Table.Td>
                        <Code>{ev.key}</Code>
                      </Table.Td>
                      <Table.Td>
                        <Text size="sm">{ev.description}</Text>
                      </Table.Td>
                      <Table.Td>
                        {ev.sensitive ? (
                          <Badge color={ev.set ? 'gray' : 'dark'} variant="light">
                            {ev.value}
                          </Badge>
                        ) : ev.value ? (
                          <Code>{ev.value}</Code>
                        ) : (
                          <Text size="sm" c="dimmed">
                            （未设置）
                          </Text>
                        )}
                      </Table.Td>
                    </Table.Tr>
                  ))}
                </Table.Tbody>
              </Table>
            </Stack>
          </Card>
        )}
      </Stack>
    </Group>
  );
}
