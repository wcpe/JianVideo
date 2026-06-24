import { Breadcrumbs, Anchor, Text } from '@mantine/core'
import { Link } from 'react-router-dom'

/**
 * 单级面包屑：label 为显示文字。
 * - to：路由路径，渲染为路由链接；
 * - onClick：页内回调（如返回上级视图），渲染为可点击锚点、不切路由；
 * - 二者皆缺省（末项当前页）则渲染为纯文本。
 */
export interface PageBreadcrumbItem {
  label: string
  to?: string
  onClick?: () => void
}

interface PageBreadcrumbsProps {
  items: PageBreadcrumbItem[]
}

/**
 * 页面级路由面包屑（FR-95 深层页面包屑）。
 * 指明深层页在导航中的路径层级（如「首页 › 目录浏览」），与 DirectoryBreadcrumb（页内目录树）不同：
 * 本组件按路由跳转、末项为当前页（纯文本不可点）。供目录浏览 / 播放页 / 相册等深层页顶部复用。
 */
export default function PageBreadcrumbs({ items }: PageBreadcrumbsProps) {
  return (
    <Breadcrumbs separator="›" separatorMargin="xs">
      {items.map((item, index) => {
        const isLast = index === items.length - 1
        // 末项（当前页）或既无 to 又无 onClick 的项渲染为纯文本
        if (isLast || (!item.to && !item.onClick)) {
          return (
            <Text key={item.label} size="sm" c="dimmed">
              {item.label}
            </Text>
          )
        }
        // 页内回调项：可点击锚点、不切路由（如返回上级视图）
        if (item.onClick) {
          return (
            <Anchor key={item.label} component="button" type="button" onClick={item.onClick} size="sm" underline="hover">
              {item.label}
            </Anchor>
          )
        }
        // 路由项：渲染为路由链接
        return (
          <Anchor key={item.label} component={Link} to={item.to!} size="sm" underline="hover">
            {item.label}
          </Anchor>
        )
      })}
    </Breadcrumbs>
  )
}
