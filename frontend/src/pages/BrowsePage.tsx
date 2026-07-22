import { useState, useMemo, useEffect, useCallback } from 'react';
import { useSearchParams } from 'react-router-dom';
import {
  Stack,
  Group,
  SegmentedControl,
  NativeSelect,
  Text,
  Loader,
  Button,
  Drawer,
  Box,
  ActionIcon,
  Tooltip,
  Divider,
  Paper,
} from '@mantine/core';
import { useDisclosure } from '@mantine/hooks';
import { notifications } from '@mantine/notifications';
import {
  IconArrowUp,
  IconRefresh,
  IconFilter,
  IconFolders,
  IconDownload,
  IconPhotoPlus,
  IconTag,
  IconTrash,
  IconMovie,
  IconFolderShare,
} from '@tabler/icons-react';
import { useLibraryPaths } from '@/hooks/useLibraryPaths';
import { useDirectoryBrowse, BROWSE_ROOT, type BrowseSort } from '@/hooks/useDirectoryBrowse';
import DirectoryBrowser from '@/components/DirectoryBrowser';
import BrowseTabBar from '@/components/BrowseTabBar';
import { useBrowseTabsStore, type BrowseTab } from '@/stores/browse-tabs';
import { useBrowseTabsPersistence } from '@/stores/useBrowseTabsPersistence';
import { sortFiles, type DisplayMode } from '@/components/DirectoryBrowser.helpers';
import DirectoryTree from '@/components/DirectoryTree';
import DirectoryAddressBar from '@/components/DirectoryAddressBar';
import MediaQueryFilters from '@/components/MediaQueryFilters';
import MediaDetailPanel from '@/components/MediaDetailPanel';
import PageHeader from '@/components/PageHeader';
import ConfirmModal from '@/components/ConfirmModal';
import BatchActionsModals from '@/components/BatchActionsModals';
import { useBatchActions } from '@/hooks/useBatchActions';
import { extractErrorMessage } from '@/utils/error';
import { formatSize } from '@/utils/format';
import * as libApi from '@/api/library';
import type { MediaFile } from '@/types';

/**
 * 单个浏览会话（FR-121 资源管理器布局）：左导航树 + 可点地址栏 + 工具栏 + 视图模式（详情/列表/大中小）
 * + 排序（接后端 sort）+ 状态栏。复用 FR-33 视图、FR-36 筛选搜索、FR-69 多选、FR-91 批量、FR-34 详情。
 * 无筛选：浏览真实路径树；有筛选/搜索：按当前目录路径（前缀，递归）查媒体接口展示匹配结果。只读浏览。
 *
 * FR-150：由外层 BrowsePage 按 activeTabId 以 key 重挂——每个标签一个独立会话实例，
 * 挂载时用 tab 快照初始化位置/排序/筛选态，运行期变化实时写回 store 对应标签。
 */
function BrowseSession({ tab }: { tab: BrowseTab }) {
  const [searchParams] = useSearchParams();
  const paths = useLibraryPaths(undefined);
  const browse = useDirectoryBrowse(tab.sort);
  const exts = paths.customImageExtensions;
  // 写回 store 的稳定引用（FR-150）：实时把会话态镜像回激活标签，避免依赖抖动
  const updateActiveTab = useBrowseTabsStore((s) => s.updateActiveTab);

  // 展示方式（FR-121）：详情 / 列表 / 大中小图标。初值取自标签快照（FR-150）
  const [displayMode, setDisplayMode] = useState<DisplayMode>(() => tab.displayMode);
  // 筛选/搜索（FR-36）：表达式搜索 + 结构化筛选。初值取自标签快照（FR-150）
  const [searchInput, setSearchInput] = useState(() => tab.search);
  const [search, setSearch] = useState(() => tab.search);
  const [mediaType, setMediaType] = useState<'' | 'image' | 'video'>(() => tab.mediaType);
  const [sizeMin, setSizeMin] = useState(() => tab.sizeMin);
  // FR2-046：时长 / 分辨率筛选（不入标签快照，避免改持久化 schema）
  const [durationMin, setDurationMin] = useState(0);
  const [heightMin, setHeightMin] = useState(0);
  const [timeFrom, setTimeFrom] = useState(() => tab.timeFrom);
  const [timeTo, setTimeTo] = useState(() => tab.timeTo);
  // 详情面板选中下标（FR-34）
  const [detailIndex, setDetailIndex] = useState<number | null>(null);
  // 筛选结果（FR-36）
  const [filteredFiles, setFilteredFiles] = useState<MediaFile[]>([]);
  const [filtering, setFiltering] = useState(false);
  // 当前选中 id（FR-69）：由 DirectoryBrowser 上抛，供工具栏批量动作与状态栏统计
  const [selectedIds, setSelectedIds] = useState<number[]>([]);
  // 批量删除（FR-69）：待确认删除的 id 列表（非空即弹确认框）+ 删除进行中
  const [pendingDelete, setPendingDelete] = useState<number[]>([]);
  const [deleting, setDeleting] = useState(false);
  // 批量操作（FR-91）：加相册 / 打标签 / 打包下载
  const batch = useBatchActions();
  // 移动端筛选抽屉开合（FR-86）：窄屏将结构化筛选收进抽屉，搜索框常驻
  const [filterDrawerOpened, filterDrawer] = useDisclosure(false);
  // 移动端目录树抽屉开合（FR-86/FR-121）：窄屏将左树收进抽屉
  const [treeDrawerOpened, treeDrawer] = useDisclosure(false);

  // 搜索防抖 400ms
  useEffect(() => {
    const t = setTimeout(() => setSearch(searchInput), 400);
    return () => clearTimeout(t);
  }, [searchInput]);

  const filterActive = !!(
    search.trim() ||
    mediaType ||
    sizeMin ||
    durationMin ||
    heightMin ||
    timeFrom ||
    timeTo
  );
  const atRoot = browse.currentPath === BROWSE_ROOT;

  // 清除全部筛选（FR-98）：无结果态「清除筛选」CTA 调用
  const clearFilters = useCallback(() => {
    setSearchInput('');
    setSearch('');
    setMediaType('');
    setSizeMin(0);
    setDurationMin(0);
    setHeightMin(0);
    setTimeFrom('');
    setTimeTo('');
  }, []);

  // 初始化浏览起点（FR-121/FR-150）：标签自带位置（含恢复的标签）优先，
  // 仅根标签保留 ?path= 定位查询参数兜底（如库管理页跳转），否则以真实路径树根初始化。
  // library_id 已弃用（后端按真实路径跨库聚合），仅用 path。
  // 每次按 activeTabId 重挂得到全新 hook 实例（initialized 守卫复位），故只跑一次。
  useEffect(() => {
    if (tab.path !== BROWSE_ROOT) {
      browse.initPath(tab.path);
    } else {
      const path = searchParams.get('path');
      if (path) browse.initPath(path);
      else browse.initRoot();
    }
    // 挂载时初始化一次（hook 内部以 initialized 守卫防重复）
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // 会话态实时写回激活标签（FR-150）：仅写 store、不读，store 变更不回流本组件，无环。
  useEffect(() => {
    updateActiveTab({
      path: browse.currentPath,
      sort: browse.sort,
      displayMode,
      search,
      mediaType,
      sizeMin,
      timeFrom,
      timeTo,
    });
  }, [
    browse.currentPath,
    browse.sort,
    displayMode,
    search,
    mediaType,
    sizeMin,
    timeFrom,
    timeTo,
    updateActiveTab,
  ]);

  // 切换路径即退出筛选态（避免跨目录残留筛选结果）
  useEffect(() => {
    clearFilters();
    setSelectedIds([]);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [browse.currentPath]);

  // 筛选生效时按当前目录路径（前缀，递归）查媒体接口（消费 FR-35），否则清空。
  // 真实路径树下不再依赖 library_id，按 path 前缀跨库筛选。
  useEffect(() => {
    if (!filterActive || atRoot) {
      setFilteredFiles([]);
      return;
    }
    let active = true;
    setFiltering(true);
    libApi
      .getMediaFiles({
        path: browse.currentPath,
        search: search.trim() || undefined,
        type: mediaType || undefined,
        size_min: sizeMin || undefined,
        duration_min: durationMin || undefined,
        height_min: heightMin || undefined,
        time_from: timeFrom || undefined,
        time_to: timeTo || undefined,
        page_size: 100,
      })
      .then((res) => {
        if (active) setFilteredFiles(res.items);
      })
      .catch(() => {
        if (active) setFilteredFiles([]);
      })
      .finally(() => {
        if (active) setFiltering(false);
      });
    return () => {
      active = false;
    };
  }, [
    filterActive,
    search,
    mediaType,
    sizeMin,
    durationMin,
    heightMin,
    timeFrom,
    timeTo,
    atRoot,
    browse.currentPath,
  ]);

  // 详情面板与浏览器用同一套排序，保证双击下标一致。
  // DirectoryBrowser 内部对 files 再跑一遍 sortFiles，此处以同一函数排序使详情面板下标与列表完全对齐
  // （浏览模式后端已按 sort 排序，再排一次幂等；筛选模式媒体接口排序口径不同，统一按本地 sort 收敛）。
  const activeFiles = filterActive ? filteredFiles : browse.files;
  const sortedFiles = useMemo(
    () => sortFiles(activeFiles, browse.sort),
    [activeFiles, browse.sort],
  );

  // 状态栏统计：项目数（子目录 + 文件）、选中数与选中体量。
  const itemCount = (filterActive ? 0 : browse.directories.length) + activeFiles.length;
  const selectedStat = useMemo(() => {
    if (selectedIds.length === 0) return null;
    const idSet = new Set(selectedIds);
    const size = activeFiles
      .filter((f) => idSet.has(f.id))
      .reduce((sum, f) => sum + f.file_size, 0);
    return { count: selectedIds.length, size };
  }, [selectedIds, activeFiles]);

  // 删除后刷新当前视图：筛选模式重跑筛选、浏览模式重载目录。
  const refreshAfterDelete = useCallback(() => {
    if (filterActive && !atRoot) {
      libApi
        .getMediaFiles({
          path: browse.currentPath,
          search: search.trim() || undefined,
          type: mediaType || undefined,
          size_min: sizeMin || undefined,
          duration_min: durationMin || undefined,
          height_min: heightMin || undefined,
          time_from: timeFrom || undefined,
          time_to: timeTo || undefined,
          page_size: 100,
        })
        .then((res) => setFilteredFiles(res.items))
        .catch(() => {});
    } else {
      browse.reload();
    }
  }, [
    filterActive,
    atRoot,
    browse,
    search,
    mediaType,
    sizeMin,
    durationMin,
    heightMin,
    timeFrom,
    timeTo,
  ]);

  // 执行批量软删（FR-69）：确认后调端点，成功刷新 + 清空选择
  const confirmDelete = useCallback(async () => {
    setDeleting(true);
    try {
      const n = await libApi.batchDeleteMediaFiles(pendingDelete);
      notifications.show({ color: 'green', message: `已删除 ${n} 项到回收站` });
      setPendingDelete([]);
      setSelectedIds([]);
      refreshAfterDelete();
    } catch (err) {
      notifications.show({ color: 'red', message: extractErrorMessage(err, '批量删除失败') });
    } finally {
      setDeleting(false);
    }
  }, [pendingDelete, refreshAfterDelete]);

  // 上一级（FR-121）：回到面包屑倒数第二段；仅一段或根时回根。
  const goUp = useCallback(() => {
    const bc = browse.breadcrumbs;
    if (atRoot) return;
    if (bc.length >= 2) browse.navigateTo(bc[bc.length - 2].path);
    else browse.navigateTo(BROWSE_ROOT);
  }, [browse, atRoot]);

  // 工具栏批量动作（FR-91/FR-69）：以当前选中集为对象；无选中时禁用。
  const hasSelection = selectedIds.length > 0;

  // 共用的 DirectoryBrowser 渲染（浏览 / 筛选两态复用）。
  const browserNode = filterActive ? (
    // 筛选模式：展示当前目录（递归）下的匹配媒体，不展示子目录
    <DirectoryBrowser
      directories={[]}
      files={filteredFiles}
      loading={filtering}
      error={null}
      customImageExtensions={exts}
      onEnterDir={() => {}}
      onErrorClose={() => {}}
      onOpenFile={(_, index) => setDetailIndex(index)}
      displayMode={displayMode}
      sort={browse.sort}
      filtered
      onClearFilter={clearFilters}
      hideSelectionBar
      onSelectionChange={setSelectedIds}
      onBatchDelete={(ids) => setPendingDelete(ids)}
      onDeleteOne={(f) => setPendingDelete([f.id])}
      onBatchAddToAlbum={batch.openAddToAlbum}
      onBatchAddTag={batch.openAddTag}
      onBatchDownload={batch.download}
      onBatchTranscode={batch.openTranscode}
      onBatchMove={batch.openMove}
    />
  ) : (
    // 浏览模式：真实路径树（后端已按 sort 排序）
    <DirectoryBrowser
      directories={browse.directories}
      files={browse.files}
      loading={browse.loading}
      error={browse.error}
      customImageExtensions={exts}
      onEnterDir={browse.handleEnterDir}
      onErrorClose={() => browse.setError(null)}
      onOpenFile={(_, index) => setDetailIndex(index)}
      displayMode={displayMode}
      sort={browse.sort}
      hideSelectionBar
      onSelectionChange={setSelectedIds}
      onBatchDelete={(ids) => setPendingDelete(ids)}
      onDeleteOne={(f) => setPendingDelete([f.id])}
      onBatchAddToAlbum={batch.openAddToAlbum}
      onBatchAddTag={batch.openAddTag}
      onBatchDownload={batch.download}
      onBatchTranscode={batch.openTranscode}
      onBatchMove={batch.openMove}
    />
  );

  return (
    <Stack gap="sm">
      {/* 工具栏（FR-121）：导航（上一级/刷新/移动端目录树）+ 批量动作（FR-91/FR-69）+ 视图模式 + 排序 */}
      <Group gap="xs" align="center" wrap="wrap">
        <Tooltip label="上一级">
          <ActionIcon
            variant="default"
            size="lg"
            aria-label="上一级"
            disabled={atRoot}
            onClick={goUp}
          >
            <IconArrowUp size={18} />
          </ActionIcon>
        </Tooltip>
        <Tooltip label="刷新">
          <ActionIcon variant="default" size="lg" aria-label="刷新" onClick={() => browse.reload()}>
            <IconRefresh size={18} />
          </ActionIcon>
        </Tooltip>
        {/* 移动端目录树入口（FR-86）：打开左树抽屉，桌面隐藏 */}
        <Tooltip label="目录树">
          <ActionIcon
            variant="default"
            size="lg"
            aria-label="目录树"
            hiddenFrom="md"
            onClick={treeDrawer.open}
          >
            <IconFolders size={18} />
          </ActionIcon>
        </Tooltip>

        <Divider orientation="vertical" />

        {/* 批量动作（FR-91/FR-69）：以当前选中集为对象，无选中禁用 */}
        <Tooltip label="打包下载选中">
          <ActionIcon
            variant="default"
            size="lg"
            aria-label="下载"
            disabled={!hasSelection}
            onClick={() => batch.download(selectedIds)}
          >
            <IconDownload size={18} />
          </ActionIcon>
        </Tooltip>
        <Tooltip label="加入相册">
          <ActionIcon
            variant="default"
            size="lg"
            aria-label="加入相册"
            disabled={!hasSelection}
            onClick={() => batch.openAddToAlbum(selectedIds)}
          >
            <IconPhotoPlus size={18} />
          </ActionIcon>
        </Tooltip>
        <Tooltip label="打标签">
          <ActionIcon
            variant="default"
            size="lg"
            aria-label="打标签"
            disabled={!hasSelection}
            onClick={() => batch.openAddTag(selectedIds)}
          >
            <IconTag size={18} />
          </ActionIcon>
        </Tooltip>
        <Tooltip label="批量转码">
          <ActionIcon
            variant="default"
            size="lg"
            aria-label="批量转码"
            disabled={!hasSelection}
            onClick={() => batch.openTranscode(selectedIds)}
          >
            <IconMovie size={18} />
          </ActionIcon>
        </Tooltip>
        <Tooltip label="移动到媒体库">
          <ActionIcon
            variant="default"
            size="lg"
            aria-label="移动到媒体库"
            disabled={!hasSelection}
            onClick={() => batch.openMove(selectedIds)}
          >
            <IconFolderShare size={18} />
          </ActionIcon>
        </Tooltip>
        <Tooltip label="删除选中">
          <ActionIcon
            variant="default"
            size="lg"
            color="red"
            aria-label="删除"
            disabled={!hasSelection}
            onClick={() => setPendingDelete(selectedIds)}
          >
            <IconTrash size={18} />
          </ActionIcon>
        </Tooltip>

        {/* 右侧：视图模式 + 排序（接后端 sort） */}
        <Group gap="md" align="center" ml="auto">
          <SegmentedControl
            aria-label="视图模式"
            size="xs"
            value={displayMode}
            onChange={(v) => setDisplayMode(v as DisplayMode)}
            data={[
              { value: 'details', label: '详情' },
              { value: 'list', label: '列表' },
              { value: 'large', label: '大图标' },
              { value: 'medium', label: '中图标' },
              { value: 'small', label: '小图标' },
            ]}
          />
          <Group gap={4} align="center" wrap="nowrap">
            <Text size="xs" c="dimmed">
              排序
            </Text>
            <NativeSelect
              aria-label="排序方式"
              size="xs"
              value={browse.sort}
              onChange={(e) => browse.setSort(e.currentTarget.value as BrowseSort)}
              data={[
                { value: 'name', label: '名称' },
                { value: 'size', label: '大小' },
                { value: 'type', label: '类型' },
                { value: 'time', label: '修改时间' },
              ]}
            />
          </Group>
        </Group>
      </Group>

      {/* 地址栏（FR-121）：可点路径段 + 当前目录搜索框（FR-36）。移动端「筛选」入口紧随其后 */}
      <Group gap="xs" align="center" wrap="nowrap">
        <Box style={{ flex: 1, minWidth: 0 }}>
          <DirectoryAddressBar
            breadcrumbs={browse.breadcrumbs}
            onNavigate={browse.navigateTo}
            searchValue={searchInput}
            onSearchChange={setSearchInput}
          />
        </Box>
        {/* 移动端「筛选」入口（FR-86）：打开抽屉，桌面隐藏 */}
        <Button
          variant="default"
          size="sm"
          leftSection={<IconFilter size={16} />}
          onClick={filterDrawer.open}
          hiddenFrom="sm"
          style={{ flexShrink: 0 }}
        >
          筛选
        </Button>
      </Group>

      {/* 桌面端结构化筛选内联铺开（FR-86）：窄屏隐藏，改由抽屉承载 */}
      <Box visibleFrom="sm">
        <Group gap="md" align="center" wrap="wrap">
          <MediaQueryFilters
            mediaType={mediaType}
            onMediaTypeChange={setMediaType}
            sizeMin={sizeMin}
            onSizeMinChange={setSizeMin}
            durationMin={durationMin}
            onDurationMinChange={setDurationMin}
            heightMin={heightMin}
            onHeightMinChange={setHeightMin}
            timeFrom={timeFrom}
            onTimeFromChange={setTimeFrom}
            timeTo={timeTo}
            onTimeToChange={setTimeTo}
          />
          {filterActive &&
            (filtering ? (
              <Loader size="xs" />
            ) : (
              <Text size="xs" c="dimmed">
                筛选结果 {filteredFiles.length} 条
              </Text>
            ))}
        </Group>
      </Box>

      {/* 移动端筛选抽屉（FR-86）：承载与桌面同一套受控结构化筛选 */}
      <Drawer
        opened={filterDrawerOpened}
        onClose={filterDrawer.close}
        title="筛选"
        position="right"
        padding="md"
        hiddenFrom="sm"
        closeButtonProps={{ 'aria-label': '关闭筛选' }}
      >
        <MediaQueryFilters
          mediaType={mediaType}
          onMediaTypeChange={setMediaType}
          sizeMin={sizeMin}
          onSizeMinChange={setSizeMin}
          durationMin={durationMin}
          onDurationMinChange={setDurationMin}
          heightMin={heightMin}
          onHeightMinChange={setHeightMin}
          timeFrom={timeFrom}
          onTimeFromChange={setTimeFrom}
          timeTo={timeTo}
          onTimeToChange={setTimeTo}
        />
      </Drawer>

      {/* 主区（FR-121）：左导航树（桌面常驻）+ 主文件区 */}
      <Group align="flex-start" gap="md" wrap="nowrap">
        {/* 左导航树（FR-121）：桌面常驻，窄屏收进抽屉 */}
        <Paper
          withBorder
          radius="sm"
          p="xs"
          visibleFrom="md"
          style={{ width: 260, flexShrink: 0, maxHeight: '70vh', overflowY: 'auto' }}
        >
          <DirectoryTree currentPath={browse.currentPath} onNavigate={browse.navigateTo} />
        </Paper>

        {/* 主文件区 */}
        <Box style={{ flex: 1, minWidth: 0 }}>{browserNode}</Box>
      </Group>

      {/* 移动端目录树抽屉（FR-86/FR-121）：承载与桌面同一棵树，点节点后关闭抽屉 */}
      <Drawer
        opened={treeDrawerOpened}
        onClose={treeDrawer.close}
        title="目录树"
        position="left"
        padding="md"
        hiddenFrom="md"
        closeButtonProps={{ 'aria-label': '关闭目录树' }}
      >
        <DirectoryTree
          currentPath={browse.currentPath}
          onNavigate={(p) => {
            browse.navigateTo(p);
            treeDrawer.close();
          }}
        />
      </Drawer>

      {/* 状态栏（FR-121）：项目数 + 选中数与体量 */}
      <Group
        gap="md"
        align="center"
        justify="space-between"
        style={{ borderTop: '1px solid var(--mantine-color-default-border)', paddingTop: 6 }}
      >
        <Text size="xs" c="dimmed">
          {itemCount} 个项目
        </Text>
        {selectedStat && (
          <Text size="xs" c="dimmed">
            已选 {selectedStat.count} 项 · {formatSize(selectedStat.size)}
          </Text>
        )}
      </Group>

      <BatchActionsModals state={batch.modalState} />

      <MediaDetailPanel
        files={sortedFiles}
        initialIndex={detailIndex}
        onClose={() => setDetailIndex(null)}
        customImageExtensions={exts}
        onFilterByTag={(tag) => {
          // 浏览页无独立 tag 筛选态：用 search 表达式 tag 名兜底触发结果列表
          setSearchInput(tag.name);
          setSearch(tag.name);
          setDetailIndex(null);
        }}
      />

      {/* 批量删除二次确认（FR-69）：删除进回收站，可在回收站还原 */}
      <ConfirmModal
        opened={pendingDelete.length > 0}
        title="删除媒体"
        message={`确定删除选中的 ${pendingDelete.length} 项？删除后进入回收站，可在回收站还原。`}
        confirmLabel="删除"
        onConfirm={confirmDelete}
        onCancel={() => setPendingDelete([])}
        loading={deleting}
      />
    </Stack>
  );
}

/**
 * 目录浏览页（FR-150）：顶部多标签栏（参考 Windows 资源管理器）+ 当前标签对应的浏览会话。
 * 标签状态（位置/排序/筛选）由 browse-tabs store 持有；切换标签以 key 重挂 BrowseSession，
 * 每个标签是独立会话实例，互不串扰。单标签时与原资源管理器布局行为等价。
 */
export default function BrowsePage() {
  // 标签与上次位置持久化接线（FR-151）：恢复上次打开的标签、变化时防抖写回后端
  useBrowseTabsPersistence();
  const tabs = useBrowseTabsStore((s) => s.tabs);
  const activeTabId = useBrowseTabsStore((s) => s.activeTabId);
  const addTab = useBrowseTabsStore((s) => s.addTab);
  const closeTab = useBrowseTabsStore((s) => s.closeTab);
  const setActiveTab = useBrowseTabsStore((s) => s.setActiveTab);
  const activeTab = tabs.find((t) => t.id === activeTabId) ?? tabs[0];

  return (
    <Stack gap="sm">
      <PageHeader title="目录浏览" />
      <BrowseTabBar
        tabs={tabs}
        activeTabId={activeTabId}
        onSelect={setActiveTab}
        onClose={closeTab}
        onAdd={() => addTab()}
      />
      <BrowseSession key={activeTab.id} tab={activeTab} />
    </Stack>
  );
}
