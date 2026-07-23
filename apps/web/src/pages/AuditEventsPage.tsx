import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Alert,
  Badge,
  Button,
  Card,
  Code,
  Group,
  Modal,
  SegmentedControl,
  SimpleGrid,
  Skeleton,
  Stack,
  Table,
  Text,
  TextInput,
  Title,
  Tooltip,
} from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { IconAlertCircle, IconSearch, IconRefresh, IconHistory } from '@tabler/icons-react';
import { listAuditEvents } from '@/api/audit';
import { applyRollback, listRollbackEvents } from '@/api/rollback';
import { extractErrorMessage } from '@/utils/error';
import type {
  AuditEvent,
  AuditEventQuery,
  AuditJsonValue,
  AuditScope,
  RollbackEvent,
} from '@/types';

const PAGE_LIMIT = 20;
const ROLLBACK_DAYS = 30;

/** 稳定 reason_key → 中文提示（FR2-041） */
const REASON_LABELS: Record<string, string> = {
  not_registered: '该动作未注册回滚',
  missing_before: '缺少变更前快照',
  sensitive_keys: '含敏感设置，无法还原',
  no_revertable_keys: '无可回滚的设置项',
  invalid_resource: '资源标识无效',
  missing_snapshot: '缺少写回快照路径',
  snapshot_gone: '写回快照文件已丢失',
  path_redacted: '路径已脱敏，无法安全还原',
  confirm_required: '需要二次确认',
  already_applied: '可能已回滚',
};

type FilterState = Required<
  Pick<AuditEventQuery, 'scope' | 'space_id' | 'action' | 'resource_type' | 'resource_id'>
> &
  Pick<AuditEventQuery, 'from' | 'to'>;

const EMPTY_FILTERS: FilterState = {
  scope: 'space',
  space_id: '',
  action: '',
  resource_type: '',
  resource_id: '',
  from: '',
  to: '',
};

function buildQuery(filters: FilterState, cursor?: string): AuditEventQuery {
  return {
    ...filters,
    space_id: filters.scope === 'system' ? '' : filters.space_id,
    cursor,
    limit: PAGE_LIMIT,
  };
}

function formatJson(value: AuditJsonValue | null | undefined): string {
  if (value === null || value === undefined) return 'null';
  return JSON.stringify(value, null, 2);
}

function formatTime(value: string): string {
  if (!value) return '—';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString('zh-CN', { hour12: false });
}

function actorLabel(event: AuditEvent): string {
  return `${event.actor_type}:${event.actor_id || '—'}`;
}

function resourceLabel(event: { resource_type: string; resource_id: string }): string {
  return `${event.resource_type}:${event.resource_id || '—'}`;
}

function reasonText(key?: string): string {
  if (!key) return '不可回滚';
  return REASON_LABELS[key] || key;
}

export default function AuditEventsPage() {
  const [filters, setFilters] = useState<FilterState>(EMPTY_FILTERS);
  const [appliedFilters, setAppliedFilters] = useState<FilterState>(EMPTY_FILTERS);
  const [items, setItems] = useState<AuditEvent[]>([]);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selected, setSelected] = useState<AuditEvent | null>(null);

  // FR2-041 可回滚时间线
  const [rollbackItems, setRollbackItems] = useState<RollbackEvent[]>([]);
  const [rollbackLoading, setRollbackLoading] = useState(true);
  const [rollbackError, setRollbackError] = useState<string | null>(null);
  const [rollbackScope, setRollbackScope] = useState<AuditScope>('space');
  const [confirmTarget, setConfirmTarget] = useState<RollbackEvent | null>(null);
  const [applying, setApplying] = useState(false);

  const activeQuery = useMemo(() => buildQuery(appliedFilters), [appliedFilters]);

  const loadFirstPage = useCallback(async (query: AuditEventQuery) => {
    setLoading(true);
    setError(null);
    try {
      const page = await listAuditEvents(query);
      setItems(page.items);
      setNextCursor(page.next_cursor);
    } catch (err) {
      setError(extractErrorMessage(err, '加载审计事件失败'));
    } finally {
      setLoading(false);
    }
  }, []);

  const loadRollback = useCallback(async (scope: AuditScope) => {
    setRollbackLoading(true);
    setRollbackError(null);
    try {
      const page = await listRollbackEvents({
        scope,
        days: ROLLBACK_DAYS,
        limit: 50,
      });
      setRollbackItems(page.items);
    } catch (err) {
      setRollbackError(extractErrorMessage(err, '加载可回滚事件失败'));
    } finally {
      setRollbackLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadFirstPage(activeQuery);
  }, [activeQuery, loadFirstPage]);

  useEffect(() => {
    void loadRollback(rollbackScope);
  }, [rollbackScope, loadRollback]);

  const updateFilter = useCallback((key: keyof FilterState, value: string) => {
    setFilters((current) => ({ ...current, [key]: value }));
  }, []);

  const handleSearch = useCallback(() => {
    setAppliedFilters(filters);
  }, [filters]);

  const handleReset = useCallback(() => {
    setFilters(EMPTY_FILTERS);
    setAppliedFilters(EMPTY_FILTERS);
  }, []);

  const handleLoadMore = useCallback(async () => {
    if (!nextCursor) return;
    setLoadingMore(true);
    setError(null);
    try {
      const page = await listAuditEvents(buildQuery(appliedFilters, nextCursor));
      setItems((current) => [...current, ...page.items]);
      setNextCursor(page.next_cursor);
    } catch (err) {
      setError(extractErrorMessage(err, '加载更多审计事件失败'));
    } finally {
      setLoadingMore(false);
    }
  }, [appliedFilters, nextCursor]);

  const handleApplyRollback = useCallback(async () => {
    if (!confirmTarget) return;
    setApplying(true);
    try {
      await applyRollback(confirmTarget.id);
      notifications.show({
        title: '回滚成功',
        message: `已回滚事件 #${confirmTarget.id}（${confirmTarget.action}）`,
        color: 'green',
        autoClose: 4000,
      });
      setConfirmTarget(null);
      void loadRollback(rollbackScope);
      void loadFirstPage(activeQuery);
    } catch (err) {
      notifications.show({
        title: '回滚失败',
        message: extractErrorMessage(err, '执行回滚失败'),
        color: 'red',
        autoClose: 5000,
      });
    } finally {
      setApplying(false);
    }
  }, [confirmTarget, rollbackScope, loadRollback, loadFirstPage, activeQuery]);

  return (
    <Stack gap="md">
      <Title order={2}>审计事件</Title>

      {/* FR2-041 可回滚时间线 */}
      <Card withBorder padding="md" radius="md">
        <Stack gap="sm">
          <Group justify="space-between" align="center">
            <Group gap="xs">
              <IconHistory size={18} />
              <Text fw={600}>可回滚操作（近 {ROLLBACK_DAYS} 天）</Text>
            </Group>
            <SegmentedControl
              aria-label="回滚列表作用域"
              size="xs"
              data={[
                { value: 'space', label: 'Space' },
                { value: 'system', label: '系统' },
              ]}
              value={rollbackScope}
              onChange={(value) => setRollbackScope(value as AuditScope)}
            />
          </Group>
          <Text size="xs" c="dimmed">
            对可回滚事件二次确认后执行逆操作；不可回滚项显示原因。
          </Text>
          {rollbackError && (
            <Alert icon={<IconAlertCircle size={16} />} color="red" title="加载失败">
              {rollbackError}
            </Alert>
          )}
          {rollbackLoading ? (
            <Skeleton height={120} radius="md" />
          ) : rollbackItems.length === 0 ? (
            <Text size="sm" c="dimmed">
              暂无可回滚相关事件。
            </Text>
          ) : (
            <Table striped highlightOnHover withTableBorder>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>时间</Table.Th>
                  <Table.Th>动作</Table.Th>
                  <Table.Th>资源</Table.Th>
                  <Table.Th>作用域</Table.Th>
                  <Table.Th>可回滚</Table.Th>
                  <Table.Th>操作</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {rollbackItems.map((event) => (
                  <Table.Tr key={`rb-${event.id}`}>
                    <Table.Td>{formatTime(event.created_at)}</Table.Td>
                    <Table.Td>
                      <Code>{event.action}</Code>
                    </Table.Td>
                    <Table.Td>{resourceLabel(event)}</Table.Td>
                    <Table.Td>
                      <Badge color={event.scope === 'system' ? 'gray' : 'blue'}>{event.scope}</Badge>
                    </Table.Td>
                    <Table.Td>
                      {event.rollbackable ? (
                        <Badge color="teal">可回滚</Badge>
                      ) : (
                        <Tooltip label={reasonText(event.reason_key)}>
                          <Badge color="gray">不可回滚</Badge>
                        </Tooltip>
                      )}
                    </Table.Td>
                    <Table.Td>
                      <Group gap={6}>
                        <Button
                          size="xs"
                          variant="light"
                          onClick={() =>
                            setSelected({
                              id: event.id,
                              scope: event.scope,
                              space_id: event.space_id,
                              actor_type: 'system',
                              actor_id: '',
                              action: event.action,
                              resource_type: event.resource_type,
                              resource_id: event.resource_id,
                              before_json: event.before_json,
                              after_json: event.after_json,
                              metadata_json: null,
                              request_id: '',
                              created_at: event.created_at,
                            })
                          }
                        >
                          详情
                        </Button>
                        {event.rollbackable ? (
                          <Button
                            size="xs"
                            color="orange"
                            variant="filled"
                            onClick={() => setConfirmTarget(event)}
                          >
                            回滚
                          </Button>
                        ) : (
                          <Tooltip label={reasonText(event.reason_key)}>
                            <Button size="xs" variant="default" disabled>
                              回滚
                            </Button>
                          </Tooltip>
                        )}
                      </Group>
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          )}
        </Stack>
      </Card>

      <Card withBorder padding="md" radius="md">
        <Stack gap="sm">
          <Text fw={600}>审计筛选</Text>
          <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }} spacing="sm">
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
                onChange={(value) => updateFilter('scope', value)}
              />
            </Stack>
            <TextInput
              label="动作"
              placeholder="如 media.deleted"
              value={filters.action}
              onChange={(e) => updateFilter('action', e.currentTarget.value)}
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
            <TextInput
              label="Space ID"
              value={filters.space_id}
              onChange={(e) => updateFilter('space_id', e.currentTarget.value)}
            />
            <TextInput
              label="开始时间"
              placeholder="RFC3339 或 YYYY-MM-DD"
              value={filters.from}
              onChange={(e) => updateFilter('from', e.currentTarget.value)}
            />
            <TextInput
              label="结束时间"
              placeholder="RFC3339 或 YYYY-MM-DD"
              value={filters.to}
              onChange={(e) => updateFilter('to', e.currentTarget.value)}
            />
          </SimpleGrid>
          <Group gap="xs">
            <Button leftSection={<IconSearch size={16} />} onClick={handleSearch}>
              查询
            </Button>
            <Button variant="default" leftSection={<IconRefresh size={16} />} onClick={handleReset}>
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
        <Skeleton height={240} radius="md" />
      ) : (
        <Card withBorder padding="md" radius="md">
          <Stack gap="md">
            {items.length === 0 ? (
              <Text size="sm" c="dimmed">
                暂无审计事件。
              </Text>
            ) : (
              <Table striped highlightOnHover withTableBorder>
                <Table.Thead>
                  <Table.Tr>
                    <Table.Th>时间</Table.Th>
                    <Table.Th>动作</Table.Th>
                    <Table.Th>资源</Table.Th>
                    <Table.Th>操作者</Table.Th>
                    <Table.Th>作用域</Table.Th>
                    <Table.Th>详情</Table.Th>
                  </Table.Tr>
                </Table.Thead>
                <Table.Tbody>
                  {items.map((event) => (
                    <Table.Tr key={event.id}>
                      <Table.Td>{formatTime(event.created_at)}</Table.Td>
                      <Table.Td>
                        <Code>{event.action}</Code>
                      </Table.Td>
                      <Table.Td>{resourceLabel(event)}</Table.Td>
                      <Table.Td>{actorLabel(event)}</Table.Td>
                      <Table.Td>
                        <Badge color={event.scope === 'system' ? 'gray' : 'blue'}>
                          {event.scope}
                        </Badge>
                      </Table.Td>
                      <Table.Td>
                        <Button size="xs" variant="light" onClick={() => setSelected(event)}>
                          查看详情
                        </Button>
                      </Table.Td>
                    </Table.Tr>
                  ))}
                </Table.Tbody>
              </Table>
            )}
            {nextCursor && (
              <Button variant="default" onClick={handleLoadMore} loading={loadingMore}>
                加载更多
              </Button>
            )}
          </Stack>
        </Card>
      )}

      <Modal
        opened={!!selected}
        onClose={() => setSelected(null)}
        title="审计事件详情"
        size="lg"
        centered
      >
        {selected && (
          <Stack gap="sm">
            <Text size="sm">请求 ID：{selected.request_id || '—'}</Text>
            <Text size="sm">Space：{selected.space_id || '—'}</Text>
            <Text size="sm" fw={600}>
              Before
            </Text>
            <Code block>{formatJson(selected.before_json)}</Code>
            <Text size="sm" fw={600}>
              After
            </Text>
            <Code block>{formatJson(selected.after_json)}</Code>
            <Text size="sm" fw={600}>
              Metadata
            </Text>
            <Code block>{formatJson(selected.metadata_json)}</Code>
          </Stack>
        )}
      </Modal>

      {/* 二次确认回滚 */}
      <Modal
        opened={!!confirmTarget}
        onClose={() => !applying && setConfirmTarget(null)}
        title="确认回滚"
        size="md"
        centered
      >
        {confirmTarget && (
          <Stack gap="sm">
            <Alert color="orange" icon={<IconAlertCircle size={16} />}>
              将对该事件执行逆操作，可能改动设置、媒体库索引或磁盘文件。请确认 before/after 摘要后继续。
            </Alert>
            <Text size="sm">
              事件 #{confirmTarget.id} · <Code>{confirmTarget.action}</Code>
            </Text>
            <Text size="sm">资源：{resourceLabel(confirmTarget)}</Text>
            <Text size="sm" fw={600}>
              Before
            </Text>
            <Code block>{formatJson(confirmTarget.before_json)}</Code>
            <Text size="sm" fw={600}>
              After
            </Text>
            <Code block>{formatJson(confirmTarget.after_json)}</Code>
            <Group justify="flex-end" gap="xs">
              <Button variant="default" disabled={applying} onClick={() => setConfirmTarget(null)}>
                取消
              </Button>
              <Button color="orange" loading={applying} onClick={handleApplyRollback}>
                确认回滚
              </Button>
            </Group>
          </Stack>
        )}
      </Modal>
    </Stack>
  );
}
