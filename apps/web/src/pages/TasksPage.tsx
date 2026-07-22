import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Alert,
  Badge,
  Button,
  Card,
  Group,
  Progress,
  SegmentedControl,
  Select,
  SimpleGrid,
  Skeleton,
  Stack,
  Table,
  Text,
  TextInput,
  Title,
} from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { IconAlertCircle, IconRefresh, IconRotateClockwise, IconX } from '@tabler/icons-react';
import { cancelTask, getTaskStats, listTasks, retryTask } from '@/api/tasks';
import { extractErrorMessage } from '@/utils/error';
import type { AuditScope, TaskItem, TaskListQuery, TaskStats, TaskStatus } from '@/types';

const PAGE_SIZE = 20;

const STATUS_OPTIONS: { value: TaskStatus | ''; label: string }[] = [
  { value: '', label: '全部' },
  { value: 'pending', label: '排队中' },
  { value: 'running', label: '运行中' },
  { value: 'succeeded', label: '已完成' },
  { value: 'failed', label: '失败' },
  { value: 'canceled', label: '已取消' },
];

const STATUS_META: Record<TaskStatus, { color: string; label: string }> = {
  pending: { color: 'gray', label: '排队中' },
  running: { color: 'blue', label: '运行中' },
  succeeded: { color: 'green', label: '已完成' },
  failed: { color: 'red', label: '失败' },
  canceled: { color: 'yellow', label: '已取消' },
};

type FilterState = {
  scope: AuditScope;
  type: string;
  status: TaskStatus | '';
  resource_type: string;
  resource_id: string;
};

const EMPTY_FILTERS: FilterState = {
  scope: 'space',
  type: '',
  status: '',
  resource_type: '',
  resource_id: '',
};

function buildQuery(filters: FilterState, page = 1): TaskListQuery {
  return {
    ...filters,
    page,
    page_size: PAGE_SIZE,
  };
}

function statusLabel(status: TaskStatus): string {
  return STATUS_META[status]?.label ?? status;
}

function statusColor(status: TaskStatus): string {
  return STATUS_META[status]?.color ?? 'gray';
}

function formatTime(value: string | null | undefined): string {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString('zh-CN', { hour12: false });
}

function progressValue(task: TaskItem): number {
  if (task.progress <= 1) return Math.round(task.progress * 100);
  return Math.round(task.progress);
}

function resourceLabel(task: TaskItem): string {
  if (!task.resource_type && !task.resource_id) return '-';
  return `${task.resource_type || '-'}:${task.resource_id || '-'}`;
}

export default function TasksPage() {
  const [filters, setFilters] = useState<FilterState>(EMPTY_FILTERS);
  const [appliedFilters, setAppliedFilters] = useState<FilterState>(EMPTY_FILTERS);
  const [items, setItems] = useState<TaskItem[]>([]);
  const [stats, setStats] = useState<TaskStats | null>(null);
  const [page, setPage] = useState(1);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [actionID, setActionID] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const activeQuery = useMemo(() => buildQuery(appliedFilters, page), [appliedFilters, page]);

  const load = useCallback(async (query: TaskListQuery) => {
    setLoading(true);
    setError(null);
    try {
      const [taskPage, taskStats] = await Promise.all([
        listTasks(query),
        getTaskStats({
          scope: query.scope,
          type: query.type,
          status: query.status,
        }),
      ]);
      setItems(taskPage.items);
      setTotal(taskPage.total);
      setStats(taskStats);
    } catch (err) {
      setError(extractErrorMessage(err, '加载任务中心失败'));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load(activeQuery);
  }, [activeQuery, load]);

  const updateFilter = useCallback(<K extends keyof FilterState>(key: K, value: FilterState[K]) => {
    setFilters((current) => ({ ...current, [key]: value }));
  }, []);

  const applyFilters = useCallback(() => {
    setPage(1);
    setAppliedFilters(filters);
  }, [filters]);

  const resetFilters = useCallback(() => {
    setPage(1);
    setFilters(EMPTY_FILTERS);
    setAppliedFilters(EMPTY_FILTERS);
  }, []);

  const runTaskAction = useCallback(
    async (task: TaskItem, action: 'cancel' | 'retry') => {
      setActionID(task.id);
      try {
        if (action === 'cancel') {
          await cancelTask(task.id);
        } else {
          await retryTask(task.id);
        }
        await load(activeQuery);
      } catch (err) {
        notifications.show({
          title: action === 'cancel' ? '取消失败' : '重试失败',
          message: extractErrorMessage(err, '任务操作失败'),
          color: 'red',
          autoClose: 3000,
        });
      } finally {
        setActionID(null);
      }
    },
    [activeQuery, load],
  );

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  return (
    <Stack gap="md">
      <Title order={2}>任务中心</Title>

      <SimpleGrid cols={{ base: 1, sm: 2, md: 4 }} spacing="sm">
        {STATUS_OPTIONS.filter(
          (item): item is { value: TaskStatus; label: string } => !!item.value,
        ).map((item) => (
          <Card key={item.value} withBorder padding="sm" radius="md">
            <Text size="xs" c="dimmed">
              {item.label}
            </Text>
            <Text size="xl" fw={700}>
              {stats?.by_status[item.value] ?? 0}
            </Text>
          </Card>
        ))}
      </SimpleGrid>

      <Card withBorder padding="md" radius="md">
        <Stack gap="sm">
          <SimpleGrid cols={{ base: 1, sm: 2, lg: 5 }} spacing="sm">
            <Stack gap={4}>
              <Text size="sm" fw={500}>
                作用域
              </Text>
              <SegmentedControl
                aria-label="作用域"
                data={[
                  { value: 'space', label: 'Space' },
                  { value: 'system', label: '系统' },
                ]}
                value={filters.scope}
                onChange={(value) => updateFilter('scope', value as AuditScope)}
              />
            </Stack>
            <Select
              label="状态"
              data={STATUS_OPTIONS}
              value={filters.status}
              allowDeselect={false}
              onChange={(value) => updateFilter('status', (value ?? '') as TaskStatus | '')}
            />
            <TextInput
              label="类型"
              placeholder="如 library.scan"
              value={filters.type}
              onChange={(e) => updateFilter('type', e.currentTarget.value)}
            />
            <TextInput
              label="资源类型"
              placeholder="如 media"
              value={filters.resource_type}
              onChange={(e) => updateFilter('resource_type', e.currentTarget.value)}
            />
            <TextInput
              label="资源 ID"
              value={filters.resource_id}
              onChange={(e) => updateFilter('resource_id', e.currentTarget.value)}
            />
          </SimpleGrid>
          <Group gap="xs">
            <Button onClick={applyFilters}>查询</Button>
            <Button
              variant="default"
              leftSection={<IconRefresh size={16} />}
              onClick={resetFilters}
            >
              重置
            </Button>
          </Group>
        </Stack>
      </Card>

      {error && (
        <Alert icon={<IconAlertCircle size={16} />} color="red" title="加载失败">
          {error}
        </Alert>
      )}

      {loading ? (
        <Skeleton height={260} radius="md" />
      ) : (
        <Card withBorder padding="md" radius="md">
          <Stack gap="md">
            {items.length === 0 ? (
              <Text size="sm" c="dimmed">
                暂无任务。
              </Text>
            ) : (
              <Table striped highlightOnHover withTableBorder>
                <Table.Thead>
                  <Table.Tr>
                    <Table.Th>任务</Table.Th>
                    <Table.Th>状态</Table.Th>
                    <Table.Th>进度</Table.Th>
                    <Table.Th>资源</Table.Th>
                    <Table.Th>尝试</Table.Th>
                    <Table.Th>更新时间</Table.Th>
                    <Table.Th>操作</Table.Th>
                  </Table.Tr>
                </Table.Thead>
                <Table.Tbody>
                  {items.map((task) => (
                    <Table.Tr key={task.id}>
                      <Table.Td>
                        <Stack gap={2}>
                          <Text size="sm" fw={600}>
                            {task.type}
                          </Text>
                          <Text size="xs" c="dimmed">
                            #{task.id}
                          </Text>
                        </Stack>
                      </Table.Td>
                      <Table.Td>
                        <Badge color={statusColor(task.status)} variant="light">
                          {statusLabel(task.status)}
                        </Badge>
                      </Table.Td>
                      <Table.Td style={{ minWidth: 120 }}>
                        <Group gap="xs" wrap="nowrap">
                          <Progress value={progressValue(task)} size="sm" style={{ flex: 1 }} />
                          <Text size="xs">{progressValue(task)}%</Text>
                        </Group>
                        {task.error && (
                          <Text size="xs" c="red" mt={4}>
                            {task.error}
                          </Text>
                        )}
                      </Table.Td>
                      <Table.Td>{resourceLabel(task)}</Table.Td>
                      <Table.Td>
                        {task.attempts}/{task.max_attempts}
                      </Table.Td>
                      <Table.Td>{formatTime(task.updated_at)}</Table.Td>
                      <Table.Td>
                        <Group gap={4} wrap="nowrap">
                          <Button
                            size="xs"
                            variant="light"
                            color="red"
                            leftSection={<IconX size={14} />}
                            disabled={task.status !== 'pending' && task.status !== 'running'}
                            loading={actionID === task.id}
                            onClick={() => void runTaskAction(task, 'cancel')}
                          >
                            取消
                          </Button>
                          <Button
                            size="xs"
                            variant="light"
                            leftSection={<IconRotateClockwise size={14} />}
                            disabled={task.status !== 'failed' && task.status !== 'canceled'}
                            loading={actionID === task.id}
                            onClick={() => void runTaskAction(task, 'retry')}
                          >
                            重试
                          </Button>
                        </Group>
                      </Table.Td>
                    </Table.Tr>
                  ))}
                </Table.Tbody>
              </Table>
            )}

            <Group justify="space-between">
              <Text size="sm" c="dimmed">
                第 {page} / {totalPages} 页，共 {total} 条
              </Text>
              <Group gap="xs">
                <Button
                  variant="default"
                  disabled={page <= 1}
                  onClick={() => setPage((n) => n - 1)}
                >
                  上一页
                </Button>
                <Button
                  variant="default"
                  disabled={page >= totalPages}
                  onClick={() => setPage((n) => n + 1)}
                >
                  下一页
                </Button>
              </Group>
            </Group>
          </Stack>
        </Card>
      )}
    </Stack>
  );
}
