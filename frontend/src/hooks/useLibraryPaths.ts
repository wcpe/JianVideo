import { useState, useCallback, useRef, useEffect } from 'react';
import { notifications } from '@mantine/notifications';
import * as libApi from '@/api/library';
import type {
  LibraryKind,
  LibraryKindInfo,
  LibraryPath,
  MediaExtension,
  MediaExtensionType,
  MediaTypesResponse,
  ScanMode,
} from '@/types';

function libraryKindName(items: LibraryKindInfo[], kind: LibraryKind): string {
  return items.find((item) => item.kind === kind)?.name ?? kind;
}

function mediaTypesToExtensions(data: MediaTypesResponse, libraryID: number): MediaExtension[] {
  const definitions = new Map(data.types.map((item) => [item.type, item]));
  return data.rules.map((rule) => {
    const definition = definitions.get(rule.type);
    return {
      id: rule.id,
      library_id: rule.library_id ?? libraryID,
      extension: rule.extension,
      type: rule.type,
      is_builtin: rule.builtin ? 1 : 0,
      builtin: rule.builtin,
      enabled: rule.enabled,
      label: rule.label,
      description: rule.description,
      capabilities: rule.capabilities,
      type_name: definition?.name,
      type_description: definition?.description,
    };
  });
}

export function useLibraryPaths(onPathsChanged?: () => void) {
  const [paths, setPaths] = useState<LibraryPath[]>([]);
  const [loading, setLoading] = useState(false);
  const [newPath, setNewPath] = useState('');
  const [newLibraryKind, setNewLibraryKind] = useState<LibraryKind>('mixed');
  const [libraryKinds, setLibraryKinds] = useState<LibraryKindInfo[]>(libApi.defaultLibraryKinds);
  const [addingPath, setAddingPath] = useState(false);
  const addingPathRef = useRef(false);
  const [scanLoading, setScanLoading] = useState<Record<number, boolean>>({});
  const [libraryKindLoading, setLibraryKindLoading] = useState<Record<number, boolean>>({});
  const [extensionInputs, setExtensionInputs] = useState<Record<number, string>>({});
  const [extensionTypes, setExtensionTypes] = useState<Record<number, MediaExtensionType>>({});
  const [extensionLoading, setExtensionLoading] = useState<Record<number, boolean>>({});
  const [customImageExtensions, setCustomImageExtensions] = useState<Record<number, string[]>>({});
  // 各库完整后缀列表（内置 + 自定义），供后缀管理 UI 列出与删除（FR-64）
  const [extensionsByLibrary, setExtensionsByLibrary] = useState<Record<number, MediaExtension[]>>(
    {},
  );

  const loadExtensionPolicies = useCallback(async (items: LibraryPath[], replace = true) => {
    const entries = await Promise.all(
      items.map(async (path) => {
        try {
          const mediaTypes = await libApi.listMediaTypes(path.id);
          const extensions = mediaTypesToExtensions(mediaTypes, path.id);
          const imageExts = extensions
            .filter((ext) => ext.type === 'image' && (ext.enabled ?? true))
            .map((ext) => ext.extension.toLowerCase().replace(/^\./, ''));
          return [path.id, { imageExts, all: extensions }] as const;
        } catch {
          return [path.id, { imageExts: [] as string[], all: [] as MediaExtension[] }] as const;
        }
      }),
    );
    const imageNext = Object.fromEntries(entries.map(([id, v]) => [id, v.imageExts]));
    const allNext = Object.fromEntries(entries.map(([id, v]) => [id, v.all]));
    setCustomImageExtensions((prev) => (replace ? imageNext : { ...prev, ...imageNext }));
    setExtensionsByLibrary((prev) => (replace ? allNext : { ...prev, ...allNext }));
  }, []);

  const loadPaths = useCallback(async () => {
    setLoading(true);
    try {
      const items = await libApi.getLibraryPaths();
      setPaths(items);
      await loadExtensionPolicies(items);
    } catch {
      // 错误由上层处理
    } finally {
      setLoading(false);
    }
  }, [loadExtensionPolicies]);

  const loadLibraryKinds = useCallback(async () => {
    try {
      const items = await libApi.getLibraryKinds();
      setLibraryKinds(items.length > 0 ? items : libApi.defaultLibraryKinds);
    } catch {
      setLibraryKinds(libApi.defaultLibraryKinds);
    }
  }, []);

  // 挂载时自动加载路径列表
  useEffect(() => {
    loadPaths();
  }, [loadPaths]);

  useEffect(() => {
    loadLibraryKinds();
  }, [loadLibraryKinds]);

  const handleAddPath = useCallback(async () => {
    if (!newPath.trim() || addingPathRef.current) return;
    addingPathRef.current = true;
    setAddingPath(true);
    try {
      const created = await libApi.createLibraryPath(newPath.trim(), 'local', '', newLibraryKind);
      setNewPath('');
      setNewLibraryKind('mixed');
      notifications.show({
        title: '添加成功',
        message: `目录 "${created.label || created.path}" 已添加`,
        color: 'green',
        autoClose: 3000,
      });
      await loadPaths();
      await onPathsChanged?.();
    } catch (err) {
      const message = err instanceof Error ? err.message : '无法添加目录，请检查路径是否正确';
      notifications.show({ title: '添加失败', message, color: 'red', autoClose: 3000 });
    } finally {
      addingPathRef.current = false;
      setAddingPath(false);
    }
  }, [newPath, newLibraryKind, loadPaths, onPathsChanged]);

  const handleUpdateLibraryKind = useCallback(
    async (path: LibraryPath, libraryKind: LibraryKind) => {
      if ((path.library_kind || 'mixed') === libraryKind) return;
      setLibraryKindLoading((prev) => ({ ...prev, [path.id]: true }));
      try {
        const updated = await libApi.updateLibraryPath(path.id, { library_kind: libraryKind });
        setPaths((prev) => prev.map((item) => (item.id === updated.id ? updated : item)));
        notifications.show({
          title: '分型已更新',
          message: `"${updated.label || updated.path}" 已设为 ${libraryKindName(libraryKinds, updated.library_kind || 'mixed')}`,
          color: 'green',
          autoClose: 3000,
        });
      } catch (err) {
        const message = err instanceof Error ? err.message : '更新媒体库分型失败';
        notifications.show({ title: '更新失败', message, color: 'red', autoClose: 3000 });
      } finally {
        setLibraryKindLoading((prev) => ({ ...prev, [path.id]: false }));
      }
    },
    [libraryKinds],
  );

  const handleDeletePath = useCallback(
    async (path: LibraryPath, onConfirm?: () => Promise<void>) => {
      try {
        await onConfirm?.();
        notifications.show({
          title: '删除成功',
          message: `目录 "${path.label || path.path}" 已删除`,
          color: 'green',
          autoClose: 3000,
        });
        await loadPaths();
        await onPathsChanged?.();
      } catch {
        notifications.show({
          title: '删除失败',
          message: '无法删除目录',
          color: 'red',
          autoClose: 3000,
        });
      }
    },
    [loadPaths, onPathsChanged],
  );

  // 触发扫描（FR-27）：mode=incremental 增量更新只索引新增；mode=full 全量扫描并对账已删文件。
  const handleScan = useCallback(
    async (id: number, mode: ScanMode = 'incremental', onScanDone?: () => Promise<void>) => {
      setScanLoading((prev) => ({ ...prev, [id]: true }));
      try {
        // 扫描已改为后端异步执行，立即返回；实际进度由扫描进度 SSE 推送
        await libApi.scanLibrary(id, mode);
        const label = mode === 'full' ? '全量扫描已开始' : '增量更新已开始';
        notifications.show({
          title: label,
          message: '正在后台扫描，完成后将自动刷新',
          color: 'blue',
          autoClose: 3000,
        });
        await onPathsChanged?.();
        await onScanDone?.();
      } catch {
        notifications.show({
          title: '扫描失败',
          message: '扫描目录时出错',
          color: 'red',
          autoClose: 3000,
        });
      } finally {
        setScanLoading((prev) => ({ ...prev, [id]: false }));
      }
    },
    [onPathsChanged],
  );

  const handleAddExtension = useCallback(
    async (path: LibraryPath) => {
      const extension = (extensionInputs[path.id] || '').trim();
      if (!extension) return;
      const mediaType = extensionTypes[path.id] || 'video';
      setExtensionLoading((prev) => ({ ...prev, [path.id]: true }));
      try {
        await libApi.createMediaTypeRule({ library_id: path.id, extension, type: mediaType });
        setExtensionInputs((prev) => ({ ...prev, [path.id]: '' }));
        await loadExtensionPolicies([path], false);
        notifications.show({
          title: '后缀已添加',
          message: `${extension} 已绑定到 "${path.label || path.path}"`,
          color: 'green',
          autoClose: 3000,
        });
      } catch (err) {
        const message = err instanceof Error ? err.message : '添加后缀失败，请检查格式';
        notifications.show({ title: '添加失败', message, color: 'red', autoClose: 3000 });
      } finally {
        setExtensionLoading((prev) => ({ ...prev, [path.id]: false }));
      }
    },
    [extensionInputs, extensionTypes, loadExtensionPolicies],
  );

  // 删除自定义后缀（FR-64）：仅自定义可删，内置由 UI 禁用删除入口。
  const handleDeleteExtension = useCallback(
    async (path: LibraryPath, extension: MediaExtension) => {
      try {
        await libApi.deleteMediaTypeRule(extension.id ?? extension.extension);
        await loadExtensionPolicies([path], false);
        notifications.show({
          title: '后缀已删除',
          message: `${extension.extension} 已从 "${path.label || path.path}" 移除`,
          color: 'green',
          autoClose: 3000,
        });
      } catch (err) {
        const message = err instanceof Error ? err.message : '删除后缀失败';
        notifications.show({ title: '删除失败', message, color: 'red', autoClose: 3000 });
      }
    },
    [loadExtensionPolicies],
  );

  const handleToggleExtension = useCallback(
    async (path: LibraryPath, extension: MediaExtension) => {
      const nextEnabled = !(extension.enabled ?? true);
      try {
        await libApi.updateMediaTypeRule(extension.id ?? extension.extension, {
          library_id: path.id,
          enabled: nextEnabled,
        });
        await loadExtensionPolicies([path], false);
        notifications.show({
          title: nextEnabled ? '后缀已启用' : '后缀已禁用',
          message: `${extension.extension} 规则已更新`,
          color: 'green',
          autoClose: 3000,
        });
      } catch (err) {
        const message = err instanceof Error ? err.message : '更新后缀规则失败';
        notifications.show({ title: '更新失败', message, color: 'red', autoClose: 3000 });
      }
    },
    [loadExtensionPolicies],
  );

  return {
    // 状态
    paths,
    loading,
    newPath,
    newLibraryKind,
    libraryKinds,
    addingPath,
    scanLoading,
    libraryKindLoading,
    extensionInputs,
    extensionTypes,
    extensionLoading,
    customImageExtensions,
    extensionsByLibrary,
    // setter
    setNewPath,
    setNewLibraryKind,
    setExtensionInputs,
    setExtensionTypes,
    // 操作
    loadPaths,
    handleAddPath,
    handleUpdateLibraryKind,
    handleDeletePath,
    handleScan,
    handleAddExtension,
    handleDeleteExtension,
    handleToggleExtension,
  };
}
