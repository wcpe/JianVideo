import { Breadcrumbs, Anchor, Text } from '@mantine/core'
import { IconHome } from '@tabler/icons-react'
import type { BreadcrumbItem } from '@/types'

interface DirectoryBreadcrumbProps {
  items: BreadcrumbItem[]
  onNavigate: (path: string) => void
}

/** 面包屑导航组件，支持点击跳转到任意上级目录 */
export default function DirectoryBreadcrumb({ items, onNavigate }: DirectoryBreadcrumbProps) {
  const breadcrumbItems = items.map((item, index) => {
    const isLast = index === items.length - 1
    return (
      <Anchor
        key={item.path}
        onClick={() => onNavigate(item.path)}
        underline="never"
        style={{ cursor: isLast ? 'default' : 'pointer' }}
      >
        <Text size="sm" c={isLast ? 'white' : 'dimmed'}>
          {index === 0 ? <IconHome size={14} style={{ verticalAlign: 'middle', marginRight: 2 }} /> : null}
          {item.name}
        </Text>
      </Anchor>
    )
  })

  return (
    <Breadcrumbs separator="›" separatorMargin="xs">
      {breadcrumbItems}
    </Breadcrumbs>
  )
}
