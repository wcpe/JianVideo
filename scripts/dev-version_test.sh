#!/usr/bin/env bash
# scripts/dev-version.sh 的测试（FIX-3）：校验 dev 基线版本号 = max(最新 tag, VERSION) 的下一修订号。
set -uo pipefail
DIR="$(cd "$(dirname "$0")" && pwd)"
S="$DIR/dev-version.sh"

pass=0; fail=0
check() { # 描述 期望 实得
  if [ "$2" = "$3" ]; then echo "PASS: $1"; pass=$((pass+1)); else echo "FAIL: $1 期望 '$2' 实得 '$3'"; fail=$((fail+1)); fi
}

# 第 2 参覆盖 VERSION，使测试与仓库实际 VERSION 解耦、稳定
check "VERSION(0.17.0) 高于 tag(v0.15.0) → 取 VERSION 下一位" "0.17.1" "$(bash "$S" v0.15.0 0.17.0)"
check "tag 等于 VERSION → 下一位" "0.17.1" "$(bash "$S" v0.17.0 0.17.0)"
check "tag(v0.18.0) 高于 VERSION → 取 tag 下一位" "0.18.1" "$(bash "$S" v0.18.0 0.17.0)"
check "无 tag(v0.0.0) → 取 VERSION 下一位" "0.17.1" "$(bash "$S" v0.0.0 0.17.0)"
check "主次相同仅修订更高的 tag → 取 tag 下一位" "0.17.6" "$(bash "$S" v0.17.5 0.17.0)"

echo ""
echo "通过 $pass 项，失败 $fail 项。"
[ "$fail" -eq 0 ]
