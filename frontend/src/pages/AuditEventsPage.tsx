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
} from '@mantine/core';
import { IconAlertCircle, IconSearch, IconRefresh } from '@tabler/icons-react';
import { listAuditEvents } from '@/api/audit';
import { extractErrorMessage } from '@/utils/error';
import type { AuditEvent, AuditEventQuery, AuditJsonValue } from '@/types';

const PAGE_LIMIT = 20;

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

function formatJson(value: AuditJsonValue | null): string {
  if (value === null) return 'null';
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

function resourceLabel(event: AuditEvent): string {
  return `${event.resource_type}:${event.resource_id || '—'}`;
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

  useEffect(() => {
    void loadFirstPage(activeQuery);
  }, [activeQuery, loadFirstPage]);

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

  return (
    <Stack gap="md">
      <Title order={2}>审计事件</Title>

      <Card withBorder padding="md" radius="md">
        <Stack gap="sm">
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
    </Stack>
  );
}
