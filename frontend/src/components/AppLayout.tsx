import { useState, useEffect, useMemo, useCallback, useRef } from 'react';
import { Link, useNavigate, useLocation } from 'react-router-dom';
import {
  AppShell,
  Text,
  Group,
  ActionIcon,
  Burger,
  Drawer,
  Stack,
  Tooltip,
  Divider,
  Menu,
  Avatar,
  TextInput,
  UnstyledButton,
  useMantineColorScheme,
  useComputedColorScheme,
} from '@mantine/core';
import { useDisclosure, useHotkeys } from '@mantine/hooks';
import {
  IconVideo,
  IconLogout,
  IconSettings,
  IconClock,
  IconFolderOpen,
  IconPhoto,
  IconSun,
  IconMoon,
  IconDeviceDesktopAnalytics,
  IconTrash,
  IconMapPin,
  IconStethoscope,
  IconCopy,
  IconChartBar,
  IconLayoutSidebarLeftCollapse,
  IconLayoutSidebarLeftExpand,
  IconLicense,
  IconCommand,
  IconSearch,
  IconRefresh,
  IconPalette,
  IconMovie,
  IconLayoutDashboard,
  IconActivity,
  IconUpload,
  IconListCheck,
  IconDatabaseOff,
  IconLayoutGrid,
} from '@tabler/icons-react';
import { useAuthStore } from '@/stores/auth';
import { useNavCollapsed } from '@/hooks/useNavCollapsed';
import { useNavWidth, NAV_WIDTH_MIN, NAV_WIDTH_MAX } from '@/hooks/useNavWidth';
import { useDocumentTitle } from '@/hooks/useDocumentTitle';
import { CinemaContext } from '@/hooks/cinema-context';
import { getSystemInfo } from '@/api/system';
import ScanTaskIndicator from './ScanTaskIndicator';
import UpdateIndicator from './UpdateIndicator';
import CommandPalette, { type Command } from './CommandPalette';
import RouteTransition from './RouteTransition';
import UploadModal from './UploadModal';
import SearchSyntaxHelp from './SearchSyntaxHelp';

// 桌面导航收缩态的 navbar 宽度（像素）：仅留图标。
// 展开态宽度改由 useNavWidth 提供（可拖拽调整、持久化），不再固定。
const NAVBAR_WIDTH_COLLAPSED = 64;

/** 全局布局 — Mantine AppShell */
export default function AppLayout({ children }: { children: React.ReactNode }) {
  const { username, logout } = useAuthStore();
  const navigate = useNavigate();
  // 当前路由路径，用于侧栏激活态 pill（FR-95）判定
  const { pathname } = useLocation();
  const [drawerOpened, { toggle: toggleDrawer, close: closeDrawer }] = useDisclosure(false);
  // 命令面板（FR-74）：全局 Ctrl/Cmd+K 打开、header 入口按钮亦可触发
  const [paletteOpened, { open: openPalette, close: closePalette }] = useDisclosure(false);
  // Web 上传入库（FR-149）：页眉上传按钮打开上传弹窗
  const [uploadOpened, { open: openUpload, close: closeUpload }] = useDisclosure(false);
  // 桌面导航收缩态（FR-54）：持久化到 localStorage，刷新后保持；仅影响桌面 Navbar，移动端抽屉不受影响
  const [navCollapsed, toggleNavCollapsed] = useNavCollapsed();
  // 桌面导航展开态宽度（FR-115 扩展）：可拖拽调整、夹紧 160–360、持久化；仅展开态生效，收缩态固定图标宽度、移动端不受影响
  const [navWidth, setNavWidth] = useNavWidth();
  // 拖拽中标记：拖拽过程关闭 navbar 宽度过渡动画，避免逐帧过渡造成的卡顿
  const [navResizing, setNavResizing] = useState(false);
  // 影院模式（FR-85）：播放页临时收起导航的本地会话态，不写 localStorage、不改 navCollapsed 持久语义。
  // 自持本地态并经 CinemaContext 下发给子页面（如播放页）；不消费自身 Provider，便于下方计算有效收缩态。
  const [cinema, setCinemaState] = useState(false);
  const setCinema = useCallback((value: boolean) => setCinemaState(value), []);
  const cinemaValue = useMemo(() => ({ cinema, setCinema }), [cinema, setCinema]);
  // 有效收缩态 = 全局持久收缩 或 影院态临时收缩；二者任一为真即收缩，互不污染
  const effectiveCollapsed = navCollapsed || cinema;
  // 主题切换：当前色方案与切换方法（认证恢复已交由 ProtectedRoute 负责）
  const { toggleColorScheme } = useMantineColorScheme();
  const computedColorScheme = useComputedColorScheme('dark', { getInitialValueInEffect: true });
  // 导航底部版本号（FR-61）：取自系统信息；失败静默不显，不阻塞布局
  const [appVersion, setAppVersion] = useState('');
  // 页眉刷新（FR-114）：自增刷新序号，作为主内容区 key 的一部分；
  // 序号变化使当前路由内容区重挂载、重跑数据拉取副作用，仅刷新内容数据，
  // 不整页 reload、不重载导航/页眉、不触动登录态。
  const [refreshNonce, setRefreshNonce] = useState(0);
  const handleRefresh = useCallback(() => setRefreshNonce((n) => n + 1), []);

  // 页眉居中全局搜索（FR-132）：受控输入 + 引用以支持「/」快捷键聚焦。
  // 回车把关键词交给时间轴页（搜全部媒体的真源 FR-35），跳转携带 search 查询参数；
  // 空白关键词不跳转（避免空搜索）。
  const [searchQuery, setSearchQuery] = useState('');
  const searchInputRef = useRef<HTMLInputElement>(null);
  const submitSearch = useCallback(() => {
    const q = searchQuery.trim();
    if (!q) return;
    navigate(`/timeline?search=${encodeURIComponent(q)}`);
  }, [searchQuery, navigate]);

  // 拖拽调整导航宽度（FR-115 扩展）：按下手柄后监听全局 mousemove，按指针 X 设置宽度（useNavWidth 内部夹紧）；
  // 松开则解绑监听并结束拖拽态。拖拽期间关闭过渡动画，避免卡顿。仅展开态可拖。
  const handleResizeStart = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      setNavResizing(true);
      const onMove = (ev: MouseEvent) => setNavWidth(ev.clientX);
      const onUp = () => {
        setNavResizing(false);
        window.removeEventListener('mousemove', onMove);
        window.removeEventListener('mouseup', onUp);
      };
      window.addEventListener('mousemove', onMove);
      window.addEventListener('mouseup', onUp);
    },
    [setNavWidth],
  );

  // 拉取应用版本用于导航底部展示；失败仅静默（版本缺省，不影响其余布局）
  useEffect(() => {
    let active = true;
    getSystemInfo()
      .then((info) => {
        if (active) setAppVersion(info.app_version);
      })
      .catch(() => {
        /* 版本拉取失败不阻塞页面，不显版本即可 */
      });
    return () => {
      active = false;
    };
  }, []);

  const handleLogout = async () => {
    await logout();
    navigate('/login');
  };

  // 导航项激活判定（FR-95）：根路由 '/' 用精确匹配，避免前缀匹配误命中所有路径；
  // 其余项命中自身或其子路径（如 /albums 命中 /albums 与 /albums/...）。
  const isNavActive = (path: string) =>
    path === '/' ? pathname === '/' : pathname === path || pathname.startsWith(`${path}/`);

  // 导航项定义（桌面 Navbar 与移动 Drawer 共用，避免重复）
  const navItems = [
    // 库管理（FR-130）：标签由「管理」改为「库管理」，避免与「管理」组标题同名歧义
    { path: '/library-manager', label: '库管理', icon: IconSettings },
    // 首页概览数据看板（FR-117）：根路由 '/' 为概览
    { path: '/', label: '概览', icon: IconLayoutDashboard },
    // 时间轴迁至 /timeline（FR-117）：浏览媒体入口，仍用时钟图标
    { path: '/timeline', label: '时间轴', icon: IconClock },
    // Pixi 高密度媒体网格（FR2-009）
    { path: '/media-grid', label: '媒体网格', icon: IconLayoutGrid },
    { path: '/browse', label: '目录', icon: IconFolderOpen },
    { path: '/albums', label: '相册', icon: IconPhoto },
    { path: '/map', label: '地图', icon: IconMapPin },
    { path: '/recycle', label: '回收站', icon: IconTrash },
    // 问题媒体 / 健康巡检（FR-73）
    { path: '/inspect', label: '巡检', icon: IconStethoscope },
    // 感知哈希去重（FR-70）：检出近似重复媒体、批量清理候选
    { path: '/duplicates', label: '重复项', icon: IconCopy },
    // 观看热力与统计（FR-75）：观看次数、续播位置热力、最近观看时间线
    { path: '/stats', label: '统计', icon: IconChartBar },
    // 转码预设与预生成队列（FR-77）：自定义编码/分辨率预设、预热首播
    { path: '/transcode', label: '转码', icon: IconMovie },
    // 通用任务队列中心（FR2-037）：扫描、转码、缩略图等异步任务统一监控与操作
    { path: '/tasks', label: '任务', icon: IconListCheck },
    // 存储与缓存管理（FR2-048）：可重建缓存占用统计、盘点与白名单清理
    { path: '/storage-cache', label: '缓存', icon: IconDatabaseOff },
    // 系统监控（FR-119）：CPU/内存/磁盘/转码并发当前值与时序折线
    { path: '/monitor', label: '监控', icon: IconActivity },
    // 系统信息与设置合并为单页两 tab（FR-55），导航合并为一个「系统」入口
    { path: '/system', label: '系统', icon: IconDeviceDesktopAnalytics },
  ];

  // 左侧导航分组（FR-130 重构，扩 FR-83）：把扁平 navItems 按「浏览 / 洞察 / 管理」三组重排用于渲染。
  // 相对旧三组（浏览/管理/系统）的调整：统计移出浏览、监控移出系统，二者并入新增的「洞察」组；
  // 原「系统」组并入「管理」组。navItems 仍是命令面板（FR-74）的扁平真源；
  // 此处仅按 path 引用其中同一对象做视觉分组，不复制项、不改路径/语义。
  const navItemByPath = (path: string) => navItems.find((item) => item.path === path)!;
  const navGroups = [
    {
      title: '浏览',
      items: ['/', '/timeline', '/media-grid', '/browse', '/albums', '/map'].map(navItemByPath),
    },
    {
      // 洞察组（FR-130）：统计（自浏览移入）+ 监控（自系统移入）
      title: '洞察',
      items: ['/stats', '/monitor'].map(navItemByPath),
    },
    {
      // 管理组（FR-130）：原管理项 + 系统（自独立「系统」组并入，置末项）
      title: '管理',
      items: [
        '/library-manager',
        '/recycle',
        '/inspect',
        '/duplicates',
        '/transcode',
        '/tasks',
        '/storage-cache',
        '/system',
      ].map(navItemByPath),
    },
  ];

  // 动态文档标题（FR-129）：按当前路由解析页名，设浏览器标签页标题为「<页面名> - JianVideo」。
  // 页名真源复用 navItems（导航即页名的单一来源）；非导航页（播放/开源协议）补少量映射；
  // 无匹配（未知路由）传空 → hook 回退「JianVideo」。
  const pageName = useMemo(() => {
    const navHit = navItems.find((item) => isNavActive(item.path));
    if (navHit) return navHit.label;
    if (pathname.startsWith('/play/')) return '播放';
    if (pathname === '/licenses') return '开源协议';
    return '';
    // navItems 为常量定义、isNavActive 为纯函数，仅依赖 pathname 变化重算
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pathname]);
  useDocumentTitle(pageName);

  // 命令面板（FR-74）注册全局快捷键 Ctrl/Cmd+K；
  // 全局搜索（FR-132）注册「/」聚焦页眉搜索框。
  // useHotkeys 默认忽略来自 input/textarea/select 的事件，故在其它输入框内打「/」不会误触。
  useHotkeys([
    ['mod+K', openPalette],
    ['/', () => searchInputRef.current?.focus()],
  ]);

  // 命令清单（FR-74）：跳转类复用 navItems + 开源协议/扫描媒体库/搜索；直接执行类切主题/收展导航/退出登录。
  // 在此构造以拿到 navigate / toggleColorScheme / toggleNavCollapsed / logout 闭包，注入 CommandPalette 做纯展示。
  const commands: Command[] = [
    ...navItems.map((item) => ({
      id: `nav-${item.path}`,
      label: item.label,
      icon: item.icon,
      run: () => navigate(item.path),
    })),
    // 「搜索」与「扫描媒体库」不在面板内直接执行（依赖页面级上下文），仅跳到对应页面承接
    // 搜索框在时间轴页（FR-35/FR-86），时间轴迁至 /timeline 后搜索命令随之指向 /timeline（FR-117）
    { id: 'search', label: '搜索', icon: IconSearch, run: () => navigate('/timeline') },
    { id: 'scan', label: '扫描媒体库', icon: IconRefresh, run: () => navigate('/library-manager') },
    { id: 'licenses', label: '开源协议', icon: IconLicense, run: () => navigate('/licenses') },
    { id: 'toggle-theme', label: '切换主题', icon: IconPalette, run: () => toggleColorScheme() },
    {
      id: 'toggle-nav',
      label: '收起/展开导航',
      icon: IconLayoutSidebarLeftCollapse,
      run: () => toggleNavCollapsed(),
    },
    {
      id: 'logout',
      label: '退出登录',
      icon: IconLogout,
      run: () => {
        logout();
        navigate('/login');
      },
    },
  ];

  // 单个导航链接。onNavigate 用于移动端点击后关闭抽屉；collapsed 为收缩态——仅渲染图标并以 Tooltip 提示导航名
  const renderNavLink = (
    { path, label, icon: Icon }: (typeof navItems)[number],
    onNavigate?: () => void,
    collapsed = false,
  ) => {
    // 激活态 pill（FR-95）：当前路由对应项加品牌紫浅底 + 紫字（复用 FR-93 primaryColor purple）、token 圆角；
    // data-active 供测试与样式钩子，收缩态同样高亮底以保可辨。
    const active = isNavActive(path);
    const link = (
      <Link
        key={path}
        to={path}
        onClick={onNavigate}
        data-active={active || undefined}
        aria-label={label}
        style={{ textDecoration: 'none' }}
      >
        {/* 紧凑密度（FR-131）：gap 8→6、内边距交由 .nav-link 的 padding:4px 8px 控制（去掉 p="xs"），
            字号 sm→xs，整体收紧每项行高 */}
        <Group
          gap={6}
          justify={collapsed ? 'center' : undefined}
          className="nav-link"
          style={{
            borderRadius: 'var(--mantine-radius-sm)',
            cursor: 'pointer',
            backgroundColor: active ? 'var(--mantine-color-purple-light)' : undefined,
            color: active ? 'var(--mantine-color-purple-light-color)' : undefined,
          }}
        >
          <Icon size={16} />
          {!collapsed && (
            <Text size="xs" c={active ? 'inherit' : undefined} fw={active ? 600 : undefined}>
              {label}
            </Text>
          )}
        </Group>
      </Link>
    );
    // 收缩态文字隐藏，hover 出 Tooltip（label=导航名）以保可用性
    return collapsed ? (
      <Tooltip key={path} label={label} position="right" withArrow>
        {link}
      </Tooltip>
    ) : (
      link
    );
  };

  // 分组渲染（FR-83）：桌面 Navbar 与移动 Drawer 共用。
  // 展开态每组前置 dimmed 小标题；收缩态（64px）不显标题文字，改用 Divider 分隔（首组前不加）以免破版。
  const renderNavGroups = (onNavigate?: () => void, collapsed = false) =>
    navGroups.map((group, groupIndex) => (
      // 组内项间距收紧（FR-131）：gap xs→4，提高导航密度
      <Stack key={group.title} gap={4}>
        {collapsed ? (
          groupIndex > 0 && <Divider my={2} />
        ) : (
          <Text size="xs" c="dimmed" fw={600} px={4} mt={groupIndex > 0 ? 'xs' : 0}>
            {group.title}
          </Text>
        )}
        {group.items.map((item) => renderNavLink(item, onNavigate, collapsed))}
      </Stack>
    ));

  // 版本号短标签（FR-61）：版本号移到页眉品牌右侧展示（取自系统信息，缺省不显）
  const versionLabel = appVersion ? `v${appVersion}` : '';
  // 移动端抽屉底部仍保留「版本号 + 开源协议」（不受导航宽度/收缩影响）：
  // 抽屉宽度充裕，沿用原样左右分布。
  const renderDrawerVersionLicense = (onNavigate?: () => void) => (
    <Group justify="space-between" mb="xs" px={4}>
      <Text size="xs" c="dimmed">
        JianVideo{versionLabel ? ` ${versionLabel}` : ''}
      </Text>
      <Link to="/licenses" onClick={onNavigate} style={{ textDecoration: 'none' }}>
        <Text size="xs" c="dimmed">
          开源协议
        </Text>
      </Link>
    </Group>
  );
  // navbar 底部「开源协议」入口（FR-115 重排）：版本号已移至页眉，底部仅留协议链接，
  // 与右下角收缩按钮同处一行（左协议、右收缩），不再换行。
  // collapsed 收缩态完全不显示协议入口（FR-115 后续修复）：64px 图标态底部仅留收缩/展开按钮，
  // 避免图标拥挤；展开态保留左下「开源协议」文字链接。
  const renderLicenseLink = (collapsed = false) =>
    collapsed ? null : (
      <Link to="/licenses" style={{ textDecoration: 'none' }}>
        <Text size="xs" c="dimmed">
          开源协议
        </Text>
      </Link>
    );

  return (
    <AppShell
      header={{ height: 56 }}
      navbar={{ width: effectiveCollapsed ? NAVBAR_WIDTH_COLLAPSED : navWidth, breakpoint: 'sm' }}
      padding="md"
    >
      <AppShell.Header>
        <Group justify="space-between" h="100%" px="md">
          <Group gap="sm">
            {/* 移动端汉堡菜单按钮 */}
            <Burger
              opened={drawerOpened}
              onClick={toggleDrawer}
              size="sm"
              aria-label="导航菜单"
              hiddenFrom="sm"
            />
            {/* 品牌标志（FR-95 放大 logo；FR-115 点击切换导航收展）：
                点击 logo 切换左侧导航展开/收缩（不再回首页，回首页改走「时间轴」导航项）。 */}
            <UnstyledButton
              onClick={toggleNavCollapsed}
              aria-label={navCollapsed ? '展开导航' : '收起导航'}
              title={navCollapsed ? '展开导航' : '收起导航'}
            >
              <Group gap={8}>
                <Group
                  justify="center"
                  align="center"
                  style={{
                    width: 36,
                    height: 36,
                    borderRadius: 'var(--mantine-radius-md)',
                    backgroundColor: 'var(--mantine-color-purple-light)',
                  }}
                >
                  <IconVideo
                    size={26}
                    style={{ color: 'var(--mantine-color-purple-light-color)' }}
                  />
                </Group>
                <Text fw={700} size="xl" style={{ color: 'var(--mantine-color-purple-4)' }}>
                  JianVideo
                </Text>
              </Group>
            </UnstyledButton>
            {/* 当前版本号（FR-61）：移到品牌「JianVideo」右侧，小号 dimmed，与 logo 同一行；取自系统信息，缺省不显 */}
            {versionLabel && (
              <Text size="xs" c="dimmed" data-testid="app-version">
                {versionLabel}
              </Text>
            )}
          </Group>

          {/* 页眉居中全局搜索（FR-132）：占据中部弹性区并水平居中，
              输入框宽度有上限以保持居中观感；回车搜全部媒体（跳时间轴，FR-35），「/」快捷键聚焦。
              窄屏（移动端）隐藏，避免与汉堡/品牌挤占；移动端搜索仍走时间轴页内搜索框（FR-86）。 */}
          <Group justify="center" gap="xs" wrap="nowrap" style={{ flex: 1 }} visibleFrom="sm">
            <TextInput
              ref={searchInputRef}
              aria-label="全局搜索"
              placeholder="搜索媒体（名称 / 路径 / 显示名）"
              leftSection={<IconSearch size={16} />}
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.currentTarget.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') submitSearch();
              }}
              style={{ width: '100%', maxWidth: 420 }}
            />
            {/* 搜索语法帮助（FR-136）：说明可搜字段与表达式 token；随全局搜索统一入口移入页眉 */}
            <SearchSyntaxHelp />
          </Group>

          <Group gap="sm">
            {/* Web 上传入库入口（FR-149）：打开上传弹窗，选/拖文件落盘到库内并触发扫描 */}
            <ActionIcon
              variant="subtle"
              color="gray"
              onClick={openUpload}
              title="上传媒体"
              aria-label="上传媒体"
            >
              <IconUpload size={18} />
            </ActionIcon>
            {/* 刷新当前页面内容（FR-114）：仅重载当前路由内容数据，
                不整页 reload、不重载导航/页眉、不重置登录态 */}
            <ActionIcon
              variant="subtle"
              color="gray"
              onClick={handleRefresh}
              title="刷新当前页面"
              aria-label="刷新当前页面"
            >
              <IconRefresh size={18} />
            </ActionIcon>
            {/* 命令面板入口（FR-74）：移动端无物理键盘时点击打开，桌面端 Ctrl/Cmd+K 亦可 */}
            <ActionIcon
              variant="subtle"
              color="gray"
              onClick={openPalette}
              title="命令面板（Ctrl+K）"
              aria-label="命令面板"
            >
              <IconCommand size={18} />
            </ActionIcon>
            {/* 「更新可用」提示（FR-58）：有新版本时常驻展示，点击跳转系统信息 tab 更新区 */}
            <UpdateIndicator />
            {/* 扫描任务队列指示器（FR-29）：有进行中任务时常驻展示 */}
            <ScanTaskIndicator />
            {/* 主题切换：暗色显示太阳（点击切浅色），浅色显示月亮（点击切暗色） */}
            <ActionIcon
              variant="subtle"
              color="gray"
              onClick={toggleColorScheme}
              title="切换主题"
              aria-label="切换主题"
            >
              {computedColorScheme === 'dark' ? <IconSun size={18} /> : <IconMoon size={18} />}
            </ActionIcon>
            {/* 用户头像下拉菜单（FR-95）：把「用户名 + 退出登录」收进头像菜单，减少顶栏拥挤。
                target 头像以用户名首字符为标识，菜单含用户名（dimmed 标签）与「退出登录」项。 */}
            <Menu position="bottom-end" withArrow shadow="md" width={180}>
              <Menu.Target>
                <Tooltip label={username} position="bottom" withArrow>
                  <UnstyledButton aria-label={`用户菜单：${username}`}>
                    <Avatar color="purple" radius="xl" size={32}>
                      {username ? username.charAt(0).toUpperCase() : '?'}
                    </Avatar>
                  </UnstyledButton>
                </Tooltip>
              </Menu.Target>
              <Menu.Dropdown>
                <Menu.Label>{username}</Menu.Label>
                <Menu.Item
                  color="red"
                  leftSection={<IconLogout size={16} />}
                  onClick={handleLogout}
                >
                  退出登录
                </Menu.Item>
              </Menu.Dropdown>
            </Menu>
          </Group>
        </Group>
      </AppShell.Header>

      {/* 移动端抽屉导航 */}
      <Drawer
        opened={drawerOpened}
        onClose={closeDrawer}
        title="导航"
        padding="md"
        size={200}
        hiddenFrom="sm"
      >
        <Stack gap="xs">
          {/* 分组导航（FR-83）：抽屉内固定展开，点击后关闭抽屉（跳转由各项 Link 负责） */}
          {renderNavGroups(closeDrawer, false)}
          {/* 版本号 + 「开源协议」入口（FR-61）：移动端抽屉不受导航宽度/收缩影响，底部保留版本号 + 协议 */}
          {renderDrawerVersionLicense(closeDrawer)}
        </Stack>
      </Drawer>

      {/* 拖拽中关闭 navbar 宽度过渡动画（避免逐帧过渡卡顿）：给 navbar 加 no-transition 标记，由 index.css 关闭其过渡 */}
      <AppShell.Navbar
        p="xs"
        visibleFrom="sm"
        data-collapsed={effectiveCollapsed}
        className={navResizing ? 'nav-resizing' : undefined}
      >
        {/* 导航列表滚动区：navbar 为定高 flex 列，列表项多 + 视口矮时会溢出。
            flex:1 占据中间剩余高度、minHeight:0 允许收缩到内容以下、overflowY:auto 触发内部滚动，
            从而顶部收起按钮与底部版本/协议常驻、中间列表可纵向滚动（修复导航栏无法滚动） */}
        <Stack
          gap="xs"
          data-testid="nav-scroll-area"
          style={{ flex: 1, minHeight: 0, overflowY: 'auto' }}
        >
          {/* 分组导航（FR-83）：随有效收缩态（持久收缩 或 影院态，FR-85）切换展开/收缩态渲染 */}
          {renderNavGroups(undefined, effectiveCollapsed)}
        </Stack>
        {/* 导航底部一行（FR-115 重排）：展开态左为「开源协议」入口、右为收缩/展开按钮，单行不换行（版本号已移至页眉）。
            收缩态（64px）完全不显示协议入口，整行仅居中放收缩/展开按钮（FR-115 后续修复）。
            影院态（FR-85）下 navCollapsed 仍为持久态语义，故按钮文案/图标按 navCollapsed 判定 */}
        <Group
          justify={effectiveCollapsed ? 'center' : 'space-between'}
          align="center"
          wrap="nowrap"
          mt="xs"
        >
          {renderLicenseLink(effectiveCollapsed)}
          <ActionIcon
            variant="subtle"
            color="gray"
            onClick={toggleNavCollapsed}
            title={navCollapsed ? '展开导航' : '收起导航'}
            aria-label={navCollapsed ? '展开导航' : '收起导航'}
          >
            {navCollapsed ? (
              <IconLayoutSidebarLeftExpand size={18} />
            ) : (
              <IconLayoutSidebarLeftCollapse size={18} />
            )}
          </ActionIcon>
        </Group>
        {/* 右边缘拖拽手柄（FR-115 扩展）：窄竖条，hover 反馈 + col-resize 光标，按下后拖拽调整展开态宽度（夹紧 160–360）。
            仅展开态显示（收缩态固定图标宽度，不可拖）；移动端抽屉无 navbar 故不受影响。无障碍：role=separator + 范围语义。 */}
        {!effectiveCollapsed && (
          <div
            className="nav-resize-handle"
            role="separator"
            aria-orientation="vertical"
            aria-label="拖拽调整导航宽度"
            aria-valuemin={NAV_WIDTH_MIN}
            aria-valuemax={NAV_WIDTH_MAX}
            aria-valuenow={navWidth}
            onMouseDown={handleResizeStart}
          />
        )}
      </AppShell.Navbar>

      {/* 影院模式上下文（FR-85）：仅向页面内容下发本地态，让播放页可临时收起导航扩大视频区 */}
      <AppShell.Main>
        {/* 路由切换淡入（FR-96/FIX-2）：仅包裹主内容区，
            以路径为 key 重挂载触发渐入；导航栏与页眉在 Main 之外，不跟随淡入/闪动。
            内容区 key 叠加刷新序号（FR-114）：路径或刷新序号变化均重挂载内容、重跑数据拉取。 */}
        <RouteTransition>
          <CinemaContext.Provider key={`${pathname}:${refreshNonce}`} value={cinemaValue}>
            {children}
          </CinemaContext.Provider>
        </RouteTransition>
      </AppShell.Main>

      {/* 全局命令面板（FR-74）：Ctrl/Cmd+K 或 header 入口打开 */}
      <CommandPalette opened={paletteOpened} onClose={closePalette} commands={commands} />

      {/* Web 上传入库弹窗（FR-149）：页眉上传按钮打开 */}
      <UploadModal opened={uploadOpened} onClose={closeUpload} />
    </AppShell>
  );
}
