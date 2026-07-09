import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Alert,
  Badge,
  Button,
  Card,
  Checkbox,
  Group,
  SimpleGrid,
  Skeleton,
  Stack,
  Table,
  Text,
  Title,
} from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { IconAlertCircle, IconRefresh, IconSearch, IconTrash } from '@tabler/icons-react';
import {
  cleanStorageCache,
  getStorageCacheSummary,
  inventoryStorageCache,
  type CacheKind,
  type StorageCacheCleanResult,
  type StorageCacheSummary,
} from '@/api/storage-cache';
import { extractErrorMessage } from '@/utils/error';
import { formatBytes } from '@/utils/format';

const KIND_OPTIONS: { kind: CacheKind; label: string; tone: string }[] = [
  { kind: 'thumbnail', label: '缩略图', tone: 'blue' },
  { kind: 'hls', label: 'HLS', tone: 'green' },
  { kind: 'image_proxy', label: '图片代理', tone: 'cyan' },
  { kind: 'cover', label: '封面', tone: 'orange' },
  { kind: 'metadata_temp', label: '元数据临时项', tone: 'gray' },
];

const ALL_KINDS = KIND_OPTIONS.map((item) => item.kind);

export default function StorageCachePage() {
  const [summary, setSummary] = useState<StorageCacheSummary | null>(null);
  const [selectedKinds, setSelectedKinds] = useState<CacheKind[]>(['thumbnail', 'hls']);
  const [cleanResult, setCleanResult] = useState<StorageCacheCleanResult | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setSummary(await getStorageCacheSummary());
    } catch (err) {
      setError(extractErrorMessage(err, '加载缓存统计失败'));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const selectedRows = useMemo(
    () => KIND_OPTIONS.filter((item) => selectedKinds.includes(item.kind)),
    [selectedKinds],
  );

  const runInventory = useCallback(async () => {
    setBusy(true);
    try {
      const result = await inventoryStorageCache();
      notifications.show({
        title: '盘点完成',
        message: `发现 ${result.discovered} 项，缺失 ${result.missing} 项`,
        color: 'green',
      });
      await load();
    } catch (err) {
      notifications.show({
        title: '盘点失败',
        message: extractErrorMessage(err, '缓存盘点失败'),
        color: 'red',
      });
    } finally {
      setBusy(false);
    }
  }, [load]);

  const runClean = useCallback(
    async (dryRun: boolean) => {
      setBusy(true);
      try {
        const result = await cleanStorageCache({ dry_run: dryRun, kinds: selectedKinds });
        setCleanResult(result);
        if (!dryRun) {
          await load();
        }
      } catch (err) {
        notifications.show({
          title: dryRun ? '预览清理失败' : '清理失败',
          message: extractErrorMessage(err, '缓存清理失败'),
          color: 'red',
        });
      } finally {
        setBusy(false);
      }
    },
    [load, selectedKinds],
  );

  return (
    <Stack gap="md">
      <Group justify="space-between" align="flex-start">
        <Stack gap={4}>
          <Title order={2}>缓存管理</Title>
          <Text size="sm" c="dimmed">
            仅管理数据目录下的可重建缓存资产。
          </Text>
        </Stack>
        <Group gap="xs">
          <Button variant="default" leftSection={<IconRefresh size={16} />} onClick={load}>
            刷新
          </Button>
          <Button variant="light" leftSection={<IconSearch size={16} />} loading={busy} onClick={runInventory}>
            盘点
          </Button>
        </Group>
      </Group>

      {error && (
        <Alert icon={<IconAlertCircle size={16} />} color="red" title="加载失败">
          {error}
        </Alert>
      )}

      {loading ? (
        <Skeleton height={220} radius="md" />
      ) : (
        <>
          <SimpleGrid cols={{ base: 1, sm: 3 }} spacing="sm">
            <Card withBorder padding="md" radius="md">
              <Text size="xs" c="dimmed">
                总占用
              </Text>
              <Text size="xl" fw={700}>
                {formatBytes(summary?.total_size_bytes ?? 0)}
              </Text>
            </Card>
            <Card withBorder padding="md" radius="md">
              <Text size="xs" c="dimmed">
                缓存资产
              </Text>
              <Text size="xl" fw={700}>
                {summary?.total_assets ?? 0}
              </Text>
            </Card>
            <Card withBorder padding="md" radius="md">
              <Text size="xs" c="dimmed">
                文件数
              </Text>
              <Text size="xl" fw={700}>
                {summary?.total_file_count ?? 0}
              </Text>
            </Card>
          </SimpleGrid>

          <Card withBorder padding="md" radius="md">
            <Stack gap="md">
              <Checkbox.Group
                label="清理范围"
                value={selectedKinds}
                onChange={(value) => setSelectedKinds(value as CacheKind[])}
              >
                <Group mt="xs">
                  {KIND_OPTIONS.map((item) => (
                    <Checkbox key={item.kind} value={item.kind} label={item.label} />
                  ))}
                </Group>
              </Checkbox.Group>

              <Table striped highlightOnHover withTableBorder>
                <Table.Thead>
                  <Table.Tr>
                    <Table.Th>类型</Table.Th>
                    <Table.Th>占用</Table.Th>
                    <Table.Th>资产</Table.Th>
                    <Table.Th>文件数</Table.Th>
                    <Table.Th>可重建</Table.Th>
                  </Table.Tr>
                </Table.Thead>
                <Table.Tbody>
                  {KIND_OPTIONS.map((item) => {
                    const row = summary?.by_kind[item.kind];
                    return (
                      <Table.Tr key={item.kind}>
                        <Table.Td>
                          <Badge color={item.tone} variant="light">
                            {item.label}
                          </Badge>
                        </Table.Td>
                        <Table.Td>{formatBytes(row?.size_bytes ?? 0)}</Table.Td>
                        <Table.Td>{row?.asset_count ?? 0}</Table.Td>
                        <Table.Td>{row?.file_count ?? 0}</Table.Td>
                        <Table.Td>{row?.rebuildable === false ? '否' : '是'}</Table.Td>
                      </Table.Tr>
                    );
                  })}
                </Table.Tbody>
              </Table>

              <Group justify="space-between">
                <Text size="sm" c="dimmed">
                  已选择 {selectedRows.length} 类缓存
                </Text>
                <Group gap="xs">
                  <Button variant="default" onClick={() => setSelectedKinds(ALL_KINDS)}>
                    全选
                  </Button>
                  <Button variant="default" onClick={() => setSelectedKinds([])}>
                    清空
                  </Button>
                  <Button
                    variant="light"
                    disabled={selectedKinds.length === 0}
                    loading={busy}
                    onClick={() => void runClean(true)}
                  >
                    预览清理
                  </Button>
                  <Button
                    color="red"
                    leftSection={<IconTrash size={16} />}
                    disabled={selectedKinds.length === 0}
                    loading={busy}
                    onClick={() => void runClean(false)}
                  >
                    清理
                  </Button>
                </Group>
              </Group>
            </Stack>
          </Card>
        </>
      )}

      {cleanResult && (
        <Alert color={cleanResult.failed_count > 0 ? 'yellow' : 'blue'} title="清理预览">
          {cleanResult.dry_run
            ? `预计影响 ${cleanResult.candidate_count} 项，占用 ${formatBytes(cleanResult.total_size_bytes)}`
            : `已清理 ${cleanResult.deleted_count} 项，释放 ${formatBytes(cleanResult.deleted_size_bytes)}`}
        </Alert>
      )}
    </Stack>
  );
}
