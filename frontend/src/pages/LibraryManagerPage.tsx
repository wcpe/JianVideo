import { useState, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { Stack, Box, Text, Progress } from '@mantine/core';
import { useLibraryPaths } from '@/hooks/useLibraryPaths';
import { useScanProgress } from '@/hooks/useScanProgress';
import LibraryPathManager from '@/components/LibraryPathManager';
import PageHeader from '@/components/PageHeader';
import ConfirmModal from '@/components/ConfirmModal';
import * as libApi from '@/api/library';
import type { LibraryPath } from '@/types';

const initDelPath = { opened: false, path: null as LibraryPath | null, loading: false };

/** 存储库管理页：仅管理存储库目录（增删、扫描、进度、媒体数量），点击卡片跳转目录视图 */
export default function LibraryManagerPage() {
  const navigate = useNavigate();
  const [delPath, setDelPath] = useState<typeof initDelPath>(initDelPath);
  const paths = useLibraryPaths(undefined);
  // 扫描完成后刷新路径（媒体数量随之更新）
  const progress = useScanProgress(() => {
    paths.loadPaths();
  });

  // 点击卡片跳转到该库的目录视图，带库定位（library_id + 起始路径）
  const handleOpenLibrary = useCallback(
    (p: LibraryPath) => {
      navigate(`/browse?library_id=${p.id}&path=${encodeURIComponent(p.path)}`);
    },
    [navigate],
  );

  const handleDelPath = useCallback(async () => {
    if (!delPath.path) return;
    setDelPath((p) => ({ ...p, loading: true }));
    try {
      await libApi.deleteLibraryPath(delPath.path.id);
    } catch {
      /* 静默 */
    }
    setDelPath(initDelPath);
    await paths.loadPaths();
  }, [delPath.path, paths]);

  return (
    <Stack gap="md">
      <PageHeader title="存储库管理" />
      <LibraryPathManager
        paths={paths.paths}
        loading={paths.loading}
        scanLoading={paths.scanLoading}
        libraryKindLoading={paths.libraryKindLoading}
        newPath={paths.newPath}
        newLibraryKind={paths.newLibraryKind}
        libraryKinds={paths.libraryKinds}
        addingPath={paths.addingPath}
        extensionInputs={paths.extensionInputs}
        extensionTypes={paths.extensionTypes}
        extensionLoading={paths.extensionLoading}
        extensionsByLibrary={paths.extensionsByLibrary}
        onNewPathChange={paths.setNewPath}
        onNewLibraryKindChange={paths.setNewLibraryKind}
        onAddPath={paths.handleAddPath}
        onUpdateLibraryKind={paths.handleUpdateLibraryKind}
        onDeletePath={(p) => setDelPath({ opened: true, path: p, loading: false })}
        onScan={(id, mode) => paths.handleScan(id, mode)}
        onBrowsePath={handleOpenLibrary}
        onExtensionInputChange={(id, v) => paths.setExtensionInputs((p) => ({ ...p, [id]: v }))}
        onExtensionTypeChange={(id, t) => paths.setExtensionTypes((p) => ({ ...p, [id]: t }))}
        onAddExtension={paths.handleAddExtension}
        onDeleteExtension={paths.handleDeleteExtension}
        onToggleExtension={paths.handleToggleExtension}
      />
      {progress?.status === 'scanning' && (
        <Box>
          <Text size="sm" c="dimmed" mb={4}>
            正在扫描… 已扫描 {progress.scanned_files} / {progress.total_files} 个文件
          </Text>
          <Progress
            value={
              progress.total_files > 0 ? (progress.scanned_files / progress.total_files) * 100 : 0
            }
            color="purple"
            animated
            size="sm"
          />
        </Box>
      )}
      <ConfirmModal
        opened={delPath.opened}
        title="删除目录"
        message={`确定要删除目录 "${delPath.path?.label || delPath.path?.path}" 吗？关联的媒体文件也会被删除。`}
        confirmLabel="删除"
        cancelLabel="取消"
        confirmColor="red"
        loading={delPath.loading}
        onConfirm={handleDelPath}
        onCancel={() => setDelPath(initDelPath)}
      />
    </Stack>
  );
}
