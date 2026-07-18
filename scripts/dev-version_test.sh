#!/usr/bin/env bash
# dev-version.sh 的稳定输入、边界与拒绝路径测试。
set -uo pipefail

script_dir="$(cd "$(dirname "$0")" && pwd)"
script="$script_dir/dev-version.sh"
passed=0
failed=0

assert_equal() {
  local name="$1" expected="$2" actual="$3"
  if [ "$expected" = "$actual" ]; then
    echo "通过：$name"
    passed=$((passed + 1))
  else
    echo "失败：$name，期望“$expected”，实际“$actual”"
    failed=$((failed + 1))
  fi
}

assert_fails() {
  local name="$1"
  shift
  if "$@" >/dev/null 2>&1; then
    echo "失败：$name，应拒绝但执行成功"
    failed=$((failed + 1))
  else
    echo "通过：$name"
    passed=$((passed + 1))
  fi
}

assert_equal "按稳定 tag 与提交距离生成版本" "0.24.0-dev.3.gabc1234" "$(bash "$script" v0.24.0 3 AbC1234)"
assert_equal "提交距离变化会改变实验序号" "1.0.0-dev.12.g0011223" "$(bash "$script" v1.0.0 12 0011223)"
assert_fails "提交距离为零时跳过" bash "$script" v0.24.0 0 abc1234
assert_fails "拒绝 RC tag 作为稳定基线" bash "$script" v1.0.0-rc.1 2 abc1234
assert_fails "拒绝非数字提交距离" bash "$script" v1.0.0 two abc1234
assert_fails "拒绝非法 SHA" bash "$script" v1.0.0 2 xyz

printf '\n通过 %d 项，失败 %d 项。\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
