#!/usr/bin/env bash
# 计算预发布 dev 版本的「基线版本号」（FIX-3，FR-48）。
#
# 取「最新正式版 tag」与「VERSION 文件」中较高者的下一修订号作为基线，使 dev：
#   ① 始终领先于上个正式版（不会出现 0.7.0-dev < 已发布 0.7.0 的倒挂）；
#   ② 不因版本 tag 未推送到远端而偏低（解耦 tag 推送时机）——
#      只要 VERSION 文件随发版上抬，dev 号即跟随，不依赖 CI 端能否看到最新 tag。
#
# 用法：dev-version.sh [latest-tag] [version-override]
#   latest-tag       省略时用 git describe 推导最新正式版 tag；
#   version-override 省略时读仓库根 VERSION 文件（测试可传入以解耦）。
# 输出：基线版本号（如 0.17.1）。调用方自行拼接 -dev.<sha>。
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"

tag="${1:-$(git describe --tags --abbrev=0 --match 'v[0-9]*.[0-9]*.[0-9]*' 2>/dev/null || echo v0.0.0)}"
tagbase="${tag#v}"

if [ "${2:-}" != "" ]; then
  verbase="$2"
else
  verbase="$(tr -d '[:space:]' < "$repo_root/VERSION" 2>/dev/null || echo 0.0.0)"
fi

IFS=. read -r tj tn tp <<< "$tagbase"
IFS=. read -r vj vn vp <<< "$verbase"
tj=${tj:-0}; tn=${tn:-0}; tp=${tp:-0}
vj=${vj:-0}; vn=${vn:-0}; vp=${vp:-0}

# 取较高者（语义版本主.次.修订数值比较）作为基线
if (( tj > vj || (tj == vj && tn > vn) || (tj == vj && tn == vn && tp > vp) )); then
  bj=$tj; bn=$tn; bp=$tp
else
  bj=$vj; bn=$vn; bp=$vp
fi

echo "${bj}.${bn}.$((bp + 1))"
