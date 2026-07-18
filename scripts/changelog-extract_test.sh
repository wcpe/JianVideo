#!/usr/bin/env bash
# changelog-extract.sh 的段落边界、围栏与错误路径测试。
set -uo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
extract="$script_dir/changelog-extract.sh"
repo_root="$(cd "$script_dir/.." && pwd)"
passed=0
failed=0
fixture="$(mktemp)"
trap 'rm -f "$fixture"' EXIT

assert_equal() {
  local name="$1" expected="$2" actual="$3"
  if [ "$expected" = "$actual" ]; then
    echo "通过：$name"
    passed=$((passed + 1))
  else
    echo "失败：$name"
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

cat > "$fixture" <<'EOF'
# 变更日志

## 未发布

### 新增
- 功能 A

## 1.2.0-rc.2（2026-01-03）

### 修复
- 候选修复

## 1.2.0（2026-01-02）

### 新增
- 功能 B

```md
## 围栏内标题
```

- 功能 C

## 1.1.0（2026-01-01）

### 修复
- 修复 D
EOF

expected_unreleased=$'### 新增\n- 功能 A'
expected_rc=$'### 修复\n- 候选修复'
expected_version=$'### 新增\n- 功能 B\n\n```md\n## 围栏内标题\n```\n\n- 功能 C'
assert_equal "抽取未发布段" "$expected_unreleased" "$(bash "$extract" unreleased "$fixture")"
assert_equal "抽取 RC 版本段" "$expected_rc" "$(bash "$extract" 1.2.0-rc.2 "$fixture")"
assert_equal "抽取稳定版本并保留围栏内容" "$expected_version" "$(bash "$extract" 1.2.0 "$fixture")"
assert_fails "缺少目标版本标题时失败" bash "$extract" 9.9.9 "$fixture"
assert_fails "拒绝非法版本" bash "$extract" 1.2 "$fixture"
assert_fails "缺少 CHANGELOG 文件时失败" bash "$extract" unreleased "$fixture.missing"

printf '\n## 1.2.0-rc.2（2026-01-04）\n\n- 重复段\n' >> "$fixture"
assert_fails "拒绝重复的目标版本标题" bash "$extract" 1.2.0-rc.2 "$fixture"

cat > "$fixture" <<'EOF'
# 变更日志

## 2.0.0（2026-02-01）


## 1.9.0（2026-01-01）
- 旧内容
EOF
assert_fails "拒绝正文为空的目标版本段" bash "$extract" 2.0.0 "$fixture"

if bash "$extract" unreleased "$repo_root/CHANGELOG.md" >/dev/null; then
  echo "通过：真实 CHANGELOG 未发布段存在且非空"
  passed=$((passed + 1))
else
  echo "失败：真实 CHANGELOG 缺少未发布标题"
  failed=$((failed + 1))
fi

version="$(tr -d '\r\n' < "$repo_root/VERSION")"
if bash "$extract" "$version" "$repo_root/CHANGELOG.md" >/dev/null; then
  echo "通过：真实 CHANGELOG 存在当前稳定版本标题"
  passed=$((passed + 1))
else
  echo "失败：真实 CHANGELOG 缺少当前稳定版本标题"
  failed=$((failed + 1))
fi

printf '\n通过 %d 项，失败 %d 项。\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
