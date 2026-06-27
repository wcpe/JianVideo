#!/usr/bin/env bash
# changelog-extract.sh 的测试集（本地与 CI 共用同一抽取实现）。
# 覆盖：① 抽某正式版段；② 抽未发布段非空；③ 段边界不串到下一段；④ 代码块内 ## 不误判；
#       ⑤ 真实仓库 CHANGELOG 抽未发布段非空。
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
EXTRACT="$SCRIPT_DIR/changelog-extract.sh"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

pass=0
fail=0

# 断言：实际输出等于期望，否则打印差异。
assert_eq() {
  local name="$1" expected="$2" actual="$3"
  if [ "$expected" = "$actual" ]; then
    pass=$((pass + 1))
    echo "PASS: $name"
  else
    fail=$((fail + 1))
    echo "FAIL: $name"
    echo "----- 期望 -----"
    printf '%s\n' "$expected"
    echo "----- 实际 -----"
    printf '%s\n' "$actual"
    echo "----------------"
  fi
}

# 断言：实际输出非空。
assert_nonempty() {
  local name="$1" actual="$2"
  if [ -n "$(printf '%s' "$actual" | tr -d '[:space:]')" ]; then
    pass=$((pass + 1))
    echo "PASS: $name"
  else
    fail=$((fail + 1))
    echo "FAIL: $name（输出为空）"
  fi
}

# 构造测试用 CHANGELOG 夹具，含代码块内的 ## 行以验证 fence 处理。
FIXTURE="$(mktemp)"
trap 'rm -f "$FIXTURE"' EXIT
cat > "$FIXTURE" <<'EOF'
# 变更日志

## 未发布

### 新增
- 新功能 A

## 1.2.0（2026-01-02）

### 新增
- 功能 B
- 示例代码块（内含 ## 不应被误判为段落结束）：

```md
## 这是代码块内的二级标题
更多内容
```

- 功能 C

## 1.1.0（2026-01-01）

### 修复
- 修复 D
EOF

# ① 抽正式版 1.2.0 段：应含功能 B/C 与代码块整体，且不串到 1.1.0。
expected_120="$(cat <<'EOF'
### 新增
- 功能 B
- 示例代码块（内含 ## 不应被误判为段落结束）：

```md
## 这是代码块内的二级标题
更多内容
```

- 功能 C
EOF
)"
actual_120="$(bash "$EXTRACT" 1.2.0 "$FIXTURE")"
assert_eq "抽正式版 1.2.0 段（含代码块、不串到下一段）" "$expected_120" "$actual_120"

# ② 抽未发布段：应为「### 新增 / - 新功能 A」，不串到 1.2.0。
expected_unrel="$(cat <<'EOF'
### 新增
- 新功能 A
EOF
)"
actual_unrel="$(bash "$EXTRACT" unreleased "$FIXTURE")"
assert_eq "抽未发布段（边界不串到 1.2.0）" "$expected_unrel" "$actual_unrel"

# ③ 抽末段 1.1.0：到文件结尾。
expected_110="$(cat <<'EOF'
### 修复
- 修复 D
EOF
)"
actual_110="$(bash "$EXTRACT" 1.1.0 "$FIXTURE")"
assert_eq "抽末段 1.1.0（到文件结尾）" "$expected_110" "$actual_110"

# ④ 代码块内的 ## 不被当作段落结束：1.2.0 段输出必须包含代码块内标题行。
if printf '%s\n' "$actual_120" | grep -q '^## 这是代码块内的二级标题$'; then
  pass=$((pass + 1))
  echo "PASS: 代码块内 ## 不误判为段落结束"
else
  fail=$((fail + 1))
  echo "FAIL: 代码块内 ## 被误判为段落结束"
fi

# ⑤ 真实仓库 CHANGELOG：未发布段非空（防回归到「暂无记录」）。
actual_repo_unrel="$(bash "$EXTRACT" unreleased "$REPO_ROOT/CHANGELOG.md")"
assert_nonempty "真实 CHANGELOG 未发布段非空" "$actual_repo_unrel"

# ⑥ 真实仓库 CHANGELOG：当前 VERSION 对应正式版段非空。
ver="$(cat "$REPO_ROOT/VERSION")"
actual_repo_ver="$(bash "$EXTRACT" "$ver" "$REPO_ROOT/CHANGELOG.md")"
assert_nonempty "真实 CHANGELOG 版本 $ver 段非空" "$actual_repo_ver"

echo ""
echo "通过 $pass 项，失败 $fail 项。"
[ "$fail" -eq 0 ]
