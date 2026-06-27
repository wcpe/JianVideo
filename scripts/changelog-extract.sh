#!/usr/bin/env bash
# 从 CHANGELOG.md 抽取指定版本段的正文，输出到 stdout（供发布工作流生成 Release notes）。
# 用法：changelog-extract.sh <版本> [CHANGELOG 路径]
#   <版本>：正式版号（如 0.16.1，匹配标题「## 0.16.1（日期）」）或字面量 unreleased（匹配「## 未发布」）。
#   [CHANGELOG 路径]：默认仓库根的 CHANGELOG.md。
# 抽取范围：目标 ## 标题的下一行起，到下一个 ## 标题之前（不含两端 ## 标题行），并去除首尾空行。
# fence 状态机：代码块（``` 围栏）内的 ## 行不计为段落结束（参照 prerelease.yml 既有处理）。
set -euo pipefail

version="${1:?用法：changelog-extract.sh <版本|unreleased> [CHANGELOG 路径]}"
changelog="${2:-CHANGELOG.md}"

if [ ! -f "$changelog" ]; then
  echo "错误：找不到 CHANGELOG 文件：$changelog" >&2
  exit 1
fi

if [ "$version" = "unreleased" ]; then
  # 未发布段标题固定为「## 未发布」（与 CHANGELOG 实际一致）。
  header_re='^## 未发布[[:space:]]*$'
else
  # 正式版段标题形如「## 0.16.1（2026-06-26）」：精确匹配版本号后紧跟「（」。
  header_re="^## ${version}（"
fi

awk -v header_re="$header_re" '
  # 遇到 ``` 围栏切换 fence 状态。
  /^```/ { fence = !fence }
  # 命中目标版本标题：开始抽取，跳过标题行本身。
  $0 ~ header_re { f = 1; next }
  # 非围栏内遇到下一个 ## 标题：抽取结束。
  f && !fence && /^## / { f = 0 }
  f { print }
' "$changelog" | awk '
  # 去除首尾空行：缓冲非空内容，遇内容时先补齐之前累积的空行。
  { lines[NR] = $0 }
  END {
    start = 1; end = NR
    while (start <= end && lines[start] ~ /^[[:space:]]*$/) start++
    while (end >= start && lines[end] ~ /^[[:space:]]*$/) end--
    for (i = start; i <= end; i++) print lines[i]
  }
'
