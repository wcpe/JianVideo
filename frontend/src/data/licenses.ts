// 开源协议清单（FR-57）的类型化入口。
//
// licenses.json 由 scripts/gen-licenses.mjs 构建期生成（入库、随 go:embed 内嵌、运行时不联网）。
// 重新生成：在 frontend 下运行 `npm run gen:licenses`。
// 页面与测试均从此模块取数；测试可 vi.mock('@/data/licenses') 注入小 fixture，避免依赖真实大 JSON。
import type { LicensesData } from '@/types'
import data from './licenses.json'

const licensesData = data as LicensesData

export default licensesData
