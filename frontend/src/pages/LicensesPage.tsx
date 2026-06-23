import { useMemo, useState } from 'react'
import {
  Stack, Title, Card, Text, Group, Badge, TextInput, Accordion, Anchor, Code, Box,
} from '@mantine/core'
import { IconSearch, IconExternalLink } from '@tabler/icons-react'
import licensesData from '@/data/licenses'
import type { FrontendLicense, BackendLicense } from '@/types'

// 协议全文统一以等宽块展示（协议是纯文本，非 markdown）
function LicenseText({ text }: { text: string }) {
  return (
    <Code block style={{ whiteSpace: 'pre-wrap', maxHeight: 360, overflowY: 'auto' }}>
      {text}
    </Code>
  )
}

// 单条前端依赖：标题行（包名 + 版本 + 协议 + 作者），展开见全文
function FrontendItem({ pkg }: { pkg: FrontendLicense }) {
  return (
    <Accordion.Item value={`fe:${pkg.name}@${pkg.version}`}>
      <Accordion.Control>
        <Group gap="xs" wrap="wrap">
          <Text fw={600} size="sm">{pkg.name}</Text>
          <Text size="xs" c="dimmed">{pkg.version}</Text>
          {pkg.license && <Badge size="sm" variant="light">{pkg.license}</Badge>}
          {pkg.author && <Text size="xs" c="dimmed">· {pkg.author}</Text>}
        </Group>
      </Accordion.Control>
      <Accordion.Panel>
        {pkg.text
          ? <LicenseText text={pkg.text} />
          : <Text size="sm" c="dimmed">未随包提供协议全文。</Text>}
      </Accordion.Panel>
    </Accordion.Item>
  )
}

// 单条后端依赖：有全文则可展开，无全文给 pkg.go.dev 外链
function BackendItem({ pkg }: { pkg: BackendLicense }) {
  // 无全文：不进 Accordion，直接一行 + 外链「查看协议」
  if (!pkg.text) {
    return (
      <Group justify="space-between" wrap="nowrap" px="md" py="xs">
        <Group gap="xs" wrap="wrap">
          <Text fw={600} size="sm">{pkg.name}</Text>
          <Text size="xs" c="dimmed">{pkg.version}</Text>
          {pkg.license && <Badge size="sm" variant="light">{pkg.license}</Badge>}
        </Group>
        {pkg.url && (
          <Anchor href={pkg.url} target="_blank" rel="noopener noreferrer" size="sm">
            <Group gap={4} wrap="nowrap">
              查看协议
              <IconExternalLink size={14} />
            </Group>
          </Anchor>
        )}
      </Group>
    )
  }
  return (
    <Accordion.Item value={`be:${pkg.name}@${pkg.version}`}>
      <Accordion.Control>
        <Group gap="xs" wrap="wrap">
          <Text fw={600} size="sm">{pkg.name}</Text>
          <Text size="xs" c="dimmed">{pkg.version}</Text>
          {pkg.license && <Badge size="sm" variant="light">{pkg.license}</Badge>}
        </Group>
      </Accordion.Control>
      <Accordion.Panel>
        <LicenseText text={pkg.text} />
      </Accordion.Panel>
    </Accordion.Item>
  )
}

/**
 * 开源协议页（FR-57）：展示项目自身 MIT 协议、前端全部生产依赖与后端 go.mod 直接依赖
 * 的协议信息。数据来自构建期生成、随 go:embed 内嵌的 licenses.json，运行时不联网。
 */
export default function LicensesPage() {
  const { project, frontend, backend } = licensesData
  const [keyword, setKeyword] = useState('')

  // 按包名（不区分大小写）过滤前后端依赖
  const trimmed = keyword.trim().toLowerCase()
  const filteredFrontend = useMemo(
    () => (trimmed ? frontend.filter((p) => p.name.toLowerCase().includes(trimmed)) : frontend),
    [frontend, trimmed],
  )
  const filteredBackend = useMemo(
    () => (trimmed ? backend.filter((p) => p.name.toLowerCase().includes(trimmed)) : backend),
    [backend, trimmed],
  )

  return (
    <Stack gap="lg">
      <Title order={2}>开源协议</Title>

      {/* 项目自身协议 */}
      <Card withBorder padding="md" radius="md">
        <Accordion variant="contained" chevronPosition="left">
          <Accordion.Item value="project">
            <Accordion.Control>
              <Group gap="xs" wrap="wrap">
                <Text fw={700}>{project.name}</Text>
                {project.license && <Badge variant="light">{project.license}</Badge>}
                {project.author && (
                  <Text size="sm" c="dimmed">Copyright (c) {project.author}</Text>
                )}
              </Group>
            </Accordion.Control>
            <Accordion.Panel>
              <LicenseText text={project.text} />
            </Accordion.Panel>
          </Accordion.Item>
        </Accordion>
      </Card>

      {/* 搜索框：按包名过滤前后端依赖 */}
      <TextInput
        placeholder="搜索软件包"
        leftSection={<IconSearch size={16} />}
        value={keyword}
        onChange={(e) => setKeyword(e.currentTarget.value)}
        aria-label="搜索软件包"
      />

      {/* 前端依赖 */}
      <Card withBorder padding="md" radius="md">
        <Title order={4} mb="sm">前端依赖（{filteredFrontend.length}）</Title>
        {filteredFrontend.length === 0
          ? <Text size="sm" c="dimmed">无匹配的前端依赖。</Text>
          : (
            <Accordion variant="separated" chevronPosition="left">
              {filteredFrontend.map((pkg) => (
                <FrontendItem key={`${pkg.name}@${pkg.version}`} pkg={pkg} />
              ))}
            </Accordion>
          )}
      </Card>

      {/* 后端依赖 */}
      <Card withBorder padding="md" radius="md">
        <Title order={4} mb="sm">后端依赖（{filteredBackend.length}）</Title>
        {filteredBackend.length === 0
          ? <Text size="sm" c="dimmed">无匹配的后端依赖。</Text>
          : (
            <Box>
              {/* 有全文者进 Accordion，无全文者单行外链；混合渲染保持顺序 */}
              <Accordion variant="separated" chevronPosition="left">
                {filteredBackend.filter((p) => p.text).map((pkg) => (
                  <BackendItem key={`${pkg.name}@${pkg.version}`} pkg={pkg} />
                ))}
              </Accordion>
              {filteredBackend.filter((p) => !p.text).map((pkg) => (
                <BackendItem key={`${pkg.name}@${pkg.version}`} pkg={pkg} />
              ))}
            </Box>
          )}
      </Card>
    </Stack>
  )
}
