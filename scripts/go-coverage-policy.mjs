// 显式下限：组合根/平台边界包无法在单元测中稳定抬到默认 60%。
// db/models 为薄 SQL/模型层，行为由 api/library/auth 等集成测覆盖；仅 content_rating 有纯函数单测。
const packageMinimums = new Map([
  ["github.com/wcpe/JianVideo", 5],
  ["github.com/wcpe/JianVideo/internal/smb", 25],
  ["github.com/wcpe/JianVideo/internal/db/models", 5],
]);

export function minimumForPackage(pkg, defaultMinimum) {
  return packageMinimums.get(pkg) ?? defaultMinimum;
}
