#!/usr/bin/env bash
# scripts/dev-version.sh 的测试（FIX-1）：
# 校验 dev 版本号 = <最新正式版 tag 基线>-dev.<提交距离>.g<短SHA>，提交距离为 0 时退出码非 0。
set -uo pipefail
DIR="$(cd "$(dirname "$0")" && pwd)"
S="$DIR/dev-version.sh"

pass=0; fail=0
check() { # 描述 期望 实得
  if [ "$2" = "$3" ]; then echo "PASS: $1"; pass=$((pass+1)); else echo "FAIL: $1 期望 '$2' 实得 '$3'"; fail=$((fail+1)); fi
}

# 第 2 参覆盖提交距离、第 3 参覆盖短 SHA，使测试与仓库实际 git 状态解耦、稳定。

# 基线取 tag 版本号本身（不再 +1），序号为提交距离，附短 SHA。
check "基线为 tag 版本号、序号为提交距离" "0.17.1-dev.3.gabc1234" "$(bash "$S" v0.17.1 3 abc1234)"
# 提交距离更大 → 序号随之增大（用于区分主干新提交）。
check "提交距离更大序号更大" "0.17.1-dev.5.gdef5678" "$(bash "$S" v0.17.1 5 def5678)"
# 不同基线 tag → 基线随之变化。
check "不同基线 tag" "0.18.0-dev.2.g0011223" "$(bash "$S" v0.18.0 2 0011223)"

# 提交距离为 0：应退出码非 0 且不输出版本号（调用方据此跳过发布）。
out="$(bash "$S" v0.17.1 0 abc1234 2>/dev/null || true)"
rc=0; bash "$S" v0.17.1 0 abc1234 >/dev/null 2>&1 || rc=$?
check "提交距离为 0 不输出版本号" "" "$out"
if [ "$rc" -ne 0 ]; then echo "PASS: 提交距离为 0 退出码非 0（实得 $rc）"; pass=$((pass+1)); else echo "FAIL: 提交距离为 0 应退出码非 0，实得 $rc"; fail=$((fail+1)); fi

echo ""
echo "通过 $pass 项，失败 $fail 项。"
[ "$fail" -eq 0 ]
