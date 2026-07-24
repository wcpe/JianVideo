#!/usr/bin/env bash
# 从最近稳定 tag、提交距离与短 SHA 生成实验版本号。
set -euo pipefail

latest_stable_tag() {
  local candidate
  while IFS= read -r candidate; do
    if [[ "$candidate" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
      printf '%s\n' "$candidate"
      return
    fi
  done < <(git tag --merged HEAD --list 'v*' --sort=-version:refname)
  printf '%s\n' v0.0.0
}

tag="${1:-$(latest_stable_tag)}"
[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "错误：稳定 tag 格式无效：$tag" >&2; exit 2; }
base="${tag#v}"

if [ "${2:-}" != "" ]; then
  count="$2"
elif git rev-parse -q --verify "refs/tags/${tag}" >/dev/null 2>&1; then
  count="$(git rev-list --count "${tag}..HEAD")"
else
  count="$(git rev-list --count HEAD)"
fi
[[ "$count" =~ ^[0-9]+$ ]] || { echo "错误：提交距离必须是非负整数。" >&2; exit 2; }
# 稳定 tag 本身上没有新提交时，不是故障：直接跳过实验版本号生成。
# 调用方（experimental.yml）应把 exit 3 当作「无实验可构建」并成功结束。
if [ "$count" -eq 0 ]; then
  echo "提示：稳定 tag 之后没有新提交，跳过实验构建。" >&2
  exit 3
fi

sha="${3:-$(git rev-parse --short=7 HEAD)}"
[[ "$sha" =~ ^[0-9a-fA-F]{7,40}$ ]] || { echo "错误：提交 SHA 格式无效。" >&2; exit 2; }
printf '%s\n' "${base}-dev.${count}.g${sha,,}"
