#!/usr/bin/env bash
# 抽取 CHANGELOG 的未发布段或指定版本段；目标标题必须唯一且正文非空。
set -euo pipefail

version="${1:?用法：changelog-extract.sh <版本|unreleased> [CHANGELOG 路径]}"
changelog="${2:-CHANGELOG.md}"
[ -f "$changelog" ] || { echo "错误：找不到 CHANGELOG 文件：$changelog" >&2; exit 1; }

if [ "$version" != "unreleased" ]; then
  [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-rc\.[1-9][0-9]*)?$ ]] || { echo "错误：版本格式无效：$version" >&2; exit 2; }
fi

awk -v version="$version" '
  {
    is_fence = $0 ~ /^```/
    prefix = "## " version "（"
    suffix = substr($0, length(prefix) + 1)
    is_target = version == "unreleased" ? $0 ~ /^## 未发布[[:space:]]*$/ : index($0, prefix) == 1 && suffix ~ /^[^）]+）[[:space:]]*$/
    if (!fence && is_target) {
      found++
      active = found == 1
      next
    }
    if (active && !fence && $0 ~ /^## /) active = 0
    if (active) lines[++count] = $0
    if (is_fence) fence = !fence
  }
  END {
    if (found == 0) {
      print "错误：找不到目标版本标题。" > "/dev/stderr"
      exit 3
    }
    if (found != 1) {
      print "错误：目标版本标题必须唯一。" > "/dev/stderr"
      exit 4
    }
    start = 1
    end = count
    while (start <= end && lines[start] ~ /^[[:space:]]*$/) start++
    while (end >= start && lines[end] ~ /^[[:space:]]*$/) end--
    if (start > end) {
      print "错误：目标版本正文不能为空。" > "/dev/stderr"
      exit 5
    }
    for (i = start; i <= end; i++) print lines[i]
  }
' "$changelog"
