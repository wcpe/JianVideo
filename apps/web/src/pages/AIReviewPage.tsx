import { useState, useEffect, useCallback } from 'react';
import {
  Stack,
  Group,
  Text,
  Button,
  Loader,
  Center,
  Paper,
  Badge,
  Select,
  Table,
  Checkbox,
} from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { IconCheck, IconX } from '@tabler/icons-react';
import { listAIResultsBySpace, batchConfirmAIResults, batchRejectAIResults } from '@/api/ai';
import { extractErrorCode, extractErrorMessage } from '@/utils/error';
import PageHeader from '@/components/PageHeader';
import EmptyState from '@/components/EmptyState';
import type { AIResult } from '@/types';

type StatusFilter = '' | 'pending' | 'confirmed' | 'rejected';

const TASK_TYPE_OPTIONS = [
  { value: '', label: '全部类型' },
  { value: 'ocr', label: 'OCR' },
  { value: 'object_scene', label: '对象/场景' },
  { value: 'face', label: '人脸' },
  { value: 'video_understanding', label: '视频理解' },
  { value: 'embedding', label: 'Embedding' },
];

const STATUS_OPTIONS: { value: StatusFilter; label: string }[] = [
  { value: '', label: '全部状态' },
  { value: 'pending', label: '待审' },
  { value: 'confirmed', label: '已确认' },
];

function resultStatusBadge(r: AIResult) {
  if (r.manual) return <Badge color="teal">已确认</Badge>;
  return (
    <Badge color="gray" variant="outline">
      待审
    </Badge>
  );
}

function summarizePayload(r: AIResult): string {
  try {
    const p = JSON.parse(r.payload_json || '{}');
    const parts: string[] = [];
    if (p.scene) parts.push(p.scene);
    if (p.summary) parts.push(p.summary);
    if (p.objects) parts.push(`${p.objects.length} 个对象`);
    if (p.faces) parts.push(`${p.faces.length} 张人脸`);
    if (p.segments) parts.push(`${p.segments.length} 段视频`);
    if (p.text) parts.push(p.text);
    return parts.join(' · ') || r.payload_json.slice(0, 100);
  } catch {
    return r.payload_json.slice(0, 100);
  }
}

export default function AIReviewPage() {
  const [results, setResults] = useState<AIResult[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [taskType, setTaskType] = useState('');
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('');
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [batchBusy, setBatchBusy] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const params: { task_type?: string; manual?: boolean } = {};
      if (taskType) params.task_type = taskType;
      if (statusFilter === 'pending') params.manual = false;
      if (statusFilter === 'confirmed') params.manual = true;
      const items = await listAIResultsBySpace(Object.keys(params).length > 0 ? params : undefined);
      setResults(items);
      setSelected(new Set());
    } catch (err) {
      const code = extractErrorCode(err);
      if (code === 'AI_DISABLED' || code === 'AI_UNAVAILABLE') {
        setError('AI 未启用或未配置模型，无法加载结果。');
        setResults([]);
      } else {
        setError(extractErrorMessage(err, '加载 AI 结果失败'));
        setResults([]);
      }
    } finally {
      setLoading(false);
    }
  }, [taskType, statusFilter]);

  useEffect(() => {
    void load();
  }, [load]);

  const toggle = useCallback((id: number) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  const handleBatchConfirm = useCallback(async () => {
    const ids = Array.from(selected);
    if (ids.length === 0) return;
    setBatchBusy(true);
    try {
      const { confirmed } = await batchConfirmAIResults(ids);
      notifications.show({
        color: 'green',
        message: `已确认 ${confirmed} 项`,
        autoClose: 2500,
      });
      await load();
    } catch (err) {
      notifications.show({
        color: 'red',
        title: '批量确认失败',
        message: extractErrorMessage(err, '批量确认失败'),
        autoClose: 4000,
      });
    } finally {
      setBatchBusy(false);
    }
  }, [selected, load]);

  const handleBatchReject = useCallback(async () => {
    const ids = Array.from(selected);
    if (ids.length === 0) return;
    setBatchBusy(true);
    try {
      const { rejected } = await batchRejectAIResults(ids);
      notifications.show({
        color: 'green',
        message: `已驳回 ${rejected} 项`,
        autoClose: 2500,
      });
      await load();
    } catch (err) {
      notifications.show({
        color: 'red',
        title: '批量驳回失败',
        message: extractErrorMessage(err, '批量驳回失败'),
        autoClose: 4000,
      });
    } finally {
      setBatchBusy(false);
    }
  }, [selected, load]);

  const allChecked = results.length > 0 && selected.size === results.length;
  const toggleAll = useCallback(() => {
    if (allChecked) {
      setSelected(new Set());
    } else {
      setSelected(new Set(results.map((r) => r.id)));
    }
  }, [allChecked, results]);

  return (
    <Stack gap="md">
      <PageHeader
        title="AI 审核"
        actions={
          <Group gap="sm">
            <Button
              variant="light"
              color="teal"
              leftSection={<IconCheck size={16} />}
              loading={batchBusy}
              disabled={selected.size === 0}
              onClick={handleBatchConfirm}
            >
              批量确认{selected.size > 0 ? `（${selected.size}）` : ''}
            </Button>
            <Button
              variant="light"
              color="red"
              leftSection={<IconX size={16} />}
              loading={batchBusy}
              disabled={selected.size === 0}
              onClick={handleBatchReject}
            >
              批量驳回{selected.size > 0 ? `（${selected.size}）` : ''}
            </Button>
          </Group>
        }
      />

      <Group gap="sm">
        <Select
          label="类型"
          data={TASK_TYPE_OPTIONS}
          value={taskType}
          onChange={(v) => setTaskType(v ?? '')}
          w={160}
        />
        <Select
          label="状态"
          data={STATUS_OPTIONS.map((o) => ({ value: o.value, label: o.label }))}
          value={statusFilter}
          onChange={(v) => setStatusFilter((v as StatusFilter) ?? '')}
          w={140}
        />
      </Group>

      {loading ? (
        <Center py="xl">
          <Loader color="grape" />
        </Center>
      ) : error ? (
        <Text size="sm" c="dimmed" role="status">
          {error}
        </Text>
      ) : results.length === 0 ? (
        <EmptyState
          icon={<IconCheck size={72} stroke={1.2} style={{ opacity: 0.5 }} />}
          title="暂无 AI 结果"
          description="当前没有待审核的 AI 结果。运行 AI 推理后，结果会出现在这里。"
        />
      ) : (
        <Paper withBorder radius="md" style={{ overflow: 'hidden' }}>
          <Table striped highlightOnHover>
            <Table.Thead>
              <Table.Tr>
                <Table.Th style={{ width: 36 }}>
                  <Checkbox checked={allChecked} onChange={toggleAll} aria-label="全选" />
                </Table.Th>
                <Table.Th>类型</Table.Th>
                <Table.Th>媒体</Table.Th>
                <Table.Th>摘要</Table.Th>
                <Table.Th>状态</Table.Th>
                <Table.Th>模型</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {results.map((r) => (
                <Table.Tr key={r.id}>
                  <Table.Td>
                    <Checkbox
                      checked={selected.has(r.id)}
                      onChange={() => toggle(r.id)}
                      aria-label={`选择结果 ${r.id}`}
                    />
                  </Table.Td>
                  <Table.Td>
                    <Badge size="sm" variant="light" color="grape">
                      {r.task_type}
                    </Badge>
                  </Table.Td>
                  <Table.Td>
                    <Text size="xs" c="dimmed">
                      #{r.media_id}
                    </Text>
                  </Table.Td>
                  <Table.Td>
                    <Text size="xs" lineClamp={2} style={{ maxWidth: 300 }}>
                      {summarizePayload(r)}
                    </Text>
                  </Table.Td>
                  <Table.Td>{resultStatusBadge(r)}</Table.Td>
                  <Table.Td>
                    <Text size="xs" c="dimmed">
                      {r.model_id}
                    </Text>
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        </Paper>
      )}
    </Stack>
  );
}
