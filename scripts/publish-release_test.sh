#!/usr/bin/env bash
# publish-release.sh 的 RC 序号、最终 RC、回下载信任根与失败清理归属测试。
set -uo pipefail

script_dir="$(cd "$(dirname "$0")" && pwd)"
script="$script_dir/publish-release.sh"
temp_dir="$(mktemp -d)"
trap 'rm -rf "$temp_dir"' EXIT
passed=0
failed=0

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

assert_succeeds() {
  local name="$1"
  shift
  if "$@" >/dev/null 2>&1; then
    echo "通过：$name"
    passed=$((passed + 1))
  else
    echo "失败：$name，执行未成功"
    failed=$((failed + 1))
  fi
}

assert_fails_with_output() {
  local name="$1" expected="$2" output status
  shift 2
  output="$("$@" 2>&1)"
  status=$?
  if [ "$status" -ne 0 ] && [[ "$output" == *"$expected"* ]]; then
    echo "通过：$name"
    passed=$((passed + 1))
  else
    echo "失败：$name，应失败并输出指定告警"
    failed=$((failed + 1))
  fi
}

assert_file_exists() {
  local name="$1" path="$2"
  if [ -f "$path" ]; then
    echo "通过：$name"
    passed=$((passed + 1))
  else
    echo "失败：$name，文件不存在"
    failed=$((failed + 1))
  fi
}

assert_file_absent() {
  local name="$1" path="$2"
  if [ ! -e "$path" ]; then
    echo "通过：$name"
    passed=$((passed + 1))
  else
    echo "失败：$name，不应创建文件"
    failed=$((failed + 1))
  fi
}

assert_file_equals() {
  local name="$1" path="$2" expected="$3" actual=""
  [ ! -f "$path" ] || IFS= read -r actual < "$path"
  if [ "$actual" = "$expected" ]; then
    echo "通过：$name"
    passed=$((passed + 1))
  else
    echo "失败：$name，文件内容不符合预期"
    failed=$((failed + 1))
  fi
}

assert_patch_contract() {
  local name="$1" path="$2" expected_latest="$3" expected_body="$4"
  if PATCH_FILE="$path" EXPECTED_LATEST="$expected_latest" EXPECTED_BODY_FILE="$expected_body" python - <<'PY'
import json
import os

with open(os.environ["PATCH_FILE"], encoding="utf-8") as source:
    patch = json.load(source)
with open(os.environ["EXPECTED_BODY_FILE"], encoding="utf-8") as source:
    expected_body = source.read()
valid = (
    patch.get("draft") is False
    and patch.get("make_latest") == os.environ["EXPECTED_LATEST"]
    and patch.get("body") == expected_body
    and "jianvideo-release-owner" not in patch.get("body", "")
)
raise SystemExit(0 if valid else 1)
PY
  then
    echo "通过：$name"
    passed=$((passed + 1))
  else
    echo "失败：$name，公开参数或正文不符合契约"
    failed=$((failed + 1))
  fi
}

mkdir -p "$temp_dir/assets"
printf 'linux\n' > "$temp_dir/assets/jianvideo-linux-amd64"
printf 'windows\n' > "$temp_dir/assets/jianvideo-windows-amd64.exe"
(
  cd "$temp_dir/assets"
  sha256sum jianvideo-linux-amd64 > jianvideo-linux-amd64.sha256
  sha256sum jianvideo-windows-amd64.exe > jianvideo-windows-amd64.sha256
)
printf '说明\n' > "$temp_dir/notes.md"
printf '越界\n' > "$temp_dir/assets/extra.txt"
assert_fails "拒绝包含额外文件的发布目录" env GH_TOKEN=test REPOSITORY=test/repo bash "$script" publish "$temp_dir/assets" v1.0.0 aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa ga "$temp_dir/notes.md"
rm "$temp_dir/assets/extra.txt"
printf '错误校验\n' > "$temp_dir/assets/jianvideo-linux-amd64.sha256"
assert_fails "拒绝校验和不匹配的构建产物" env GH_TOKEN=test REPOSITORY=test/repo bash "$script" publish "$temp_dir/assets" v1.0.0 aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa ga "$temp_dir/notes.md"
(
  cd "$temp_dir/assets"
  sha256sum jianvideo-linux-amd64 > jianvideo-linux-amd64.sha256
)
: > "$temp_dir/empty-notes.md"
assert_fails "拒绝空发布说明" env GH_TOKEN=test REPOSITORY=test/repo bash "$script" publish "$temp_dir/assets" v1.0.0 aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa ga "$temp_dir/empty-notes.md"

mkdir -p "$temp_dir/sequence-bin"
cat > "$temp_dir/sequence-bin/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
method=GET
output=""
write_status=0
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -X) method="$2"; shift 2 ;;
    -o) output="$2"; shift 2 ;;
    -w) write_status=1; shift 2 ;;
    -H|-d|--data-binary) shift 2 ;;
    -sS|-fsS|-fsSL) shift ;;
    http*) url="$1"; shift ;;
    *) shift ;;
  esac
done
status=200
body='{}'
case "$method $url" in
  "GET "*/git/matching-refs/tags/*)
    calls=0
    [ ! -f "${MOCK_STATE:?}/matching-ref-calls" ] || IFS= read -r calls < "$MOCK_STATE/matching-ref-calls"
    printf '%s\n' "$((calls + 1))" > "$MOCK_STATE/matching-ref-calls"
    printf '%s\n' "$url" > "$MOCK_STATE/matching-ref-url"
    body="${MOCK_MATCHING_REFS:-[]}"
    ;;
  "GET "*/releases/tags/*|"GET "*/git/ref/tags/*)
    status=404
    ;;
  "POST "*/git/refs)
    touch "${MOCK_STATE:?}/tag-posted"
    ;;
  "POST "*/releases)
    touch "${MOCK_STATE:?}/release-posted"
    body='{"id":123}'
    ;;
esac
if [ -n "$output" ]; then
  printf '%s' "$body" > "$output"
else
  printf '%s' "$body"
fi
if [ "$write_status" = "1" ]; then
  printf '%s' "$status"
fi
EOF
chmod +x "$temp_dir/sequence-bin/curl"

empty_refs='[]'
two_refs='[{"ref":"refs/tags/v1.0.0-rc.1"},{"ref":"refs/tags/v1.0.0-rc.2"}]'
full_page_refs="$(python - <<'PY'
import json

print(json.dumps([{"ref": f"refs/tags/v1.0.0-rc.{number}"} for number in range(1, 101)]))
PY
)"
for state in sequence-empty sequence-next sequence-publish sequence-full-page; do
  mkdir -p "$temp_dir/$state/runner"
done
assert_succeeds "无既有 RC 时只允许 rc.1" env PATH="$temp_dir/sequence-bin:$PATH" MOCK_STATE="$temp_dir/sequence-empty" MOCK_MATCHING_REFS="$empty_refs" RUNNER_TEMP="$temp_dir/sequence-empty/runner" GH_TOKEN=test REPOSITORY=test/repo GITHUB_API_URL=https://example.invalid bash "$script" preflight-rc v1.0.0-rc.1
assert_fails "无既有 RC 时拒绝从 rc.2 起步" env PATH="$temp_dir/sequence-bin:$PATH" MOCK_STATE="$temp_dir/sequence-empty" MOCK_MATCHING_REFS="$empty_refs" RUNNER_TEMP="$temp_dir/sequence-empty/runner" GH_TOKEN=test REPOSITORY=test/repo GITHUB_API_URL=https://example.invalid bash "$script" preflight-rc v1.0.0-rc.2
assert_succeeds "既有 rc.1 与 rc.2 时只允许 rc.3" env PATH="$temp_dir/sequence-bin:$PATH" MOCK_STATE="$temp_dir/sequence-next" MOCK_MATCHING_REFS="$two_refs" RUNNER_TEMP="$temp_dir/sequence-next/runner" GH_TOKEN=test REPOSITORY=test/repo GITHUB_API_URL=https://example.invalid bash "$script" preflight-rc v1.0.0-rc.3
assert_fails "拒绝跳过既有 RC 的严格下一序号" env PATH="$temp_dir/sequence-bin:$PATH" MOCK_STATE="$temp_dir/sequence-next" MOCK_MATCHING_REFS="$two_refs" RUNNER_TEMP="$temp_dir/sequence-next/runner" GH_TOKEN=test REPOSITORY=test/repo GITHUB_API_URL=https://example.invalid bash "$script" preflight-rc v1.0.0-rc.4
assert_fails "publish 阶段复检并拒绝过期 RC 序号" env PATH="$temp_dir/sequence-bin:$PATH" MOCK_STATE="$temp_dir/sequence-publish" MOCK_MATCHING_REFS="$empty_refs" RUNNER_TEMP="$temp_dir/sequence-publish/runner" GH_TOKEN=test REPOSITORY=test/repo GITHUB_API_URL=https://example.invalid bash "$script" publish "$temp_dir/assets" v1.0.0-rc.2 aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa rc "$temp_dir/notes.md"
assert_file_absent "RC 序号复检失败时不创建 tag" "$temp_dir/sequence-publish/tag-posted"
assert_succeeds "matching-refs 返回 100 条时单次请求正常结束" timeout 5s env PATH="$temp_dir/sequence-bin:$PATH" MOCK_STATE="$temp_dir/sequence-full-page" MOCK_MATCHING_REFS="$full_page_refs" RUNNER_TEMP="$temp_dir/sequence-full-page/runner" GH_TOKEN=test REPOSITORY=test/repo GITHUB_API_URL=https://example.invalid bash "$script" preflight-rc v1.0.0-rc.101
assert_file_equals "matching-refs 只请求一次" "$temp_dir/sequence-full-page/matching-ref-calls" 1
assert_file_equals "matching-refs 请求不携带分页参数" "$temp_dir/sequence-full-page/matching-ref-url" "https://example.invalid/repos/test/repo/git/matching-refs/tags/v1.0.0-rc."
assert_file_absent "满页预检不创建 tag" "$temp_dir/sequence-full-page/tag-posted"

mkdir -p "$temp_dir/verify-bin" "$temp_dir/repo"
cat > "$temp_dir/verify-bin/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
output=""
write_status=0
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) output="$2"; shift 2 ;;
    -w) write_status=1; shift 2 ;;
    -H) shift 2 ;;
    -sS|-fsS|-fsSL) shift ;;
    http*) url="$1"; shift ;;
    *) shift ;;
  esac
done
status=200
body='{}'
wrote_output=0
case "$url" in
  */git/matching-refs/tags/*)
    body="${MOCK_MATCHING_REFS:?}"
    ;;
  */git/ref/tags/*)
    body="{\"object\":{\"type\":\"${MOCK_REF_TYPE:-commit}\",\"sha\":\"${MOCK_TAG_SHA:?}\"}}"
    ;;
  */releases/tags/*)
    tag="${url##*/}"
    if [ -n "${MOCK_RELEASE_ASSETS:-}" ]; then
      assets="$MOCK_RELEASE_ASSETS"
    else
      assets='[{"id":11,"name":"jianvideo-linux-amd64","state":"uploaded","size":6},{"id":12,"name":"jianvideo-windows-amd64.exe","state":"uploaded","size":8},{"id":13,"name":"checksums.txt","state":"uploaded","size":170}]'
    fi
    body="{\"tag_name\":\"$tag\",\"draft\":false,\"prerelease\":true,\"assets\":$assets}"
    ;;
  */releases/assets/11)
    if [ "${MOCK_BAD_DOWNLOAD:-0}" = "1" ]; then
      printf '被替换的 linux\n' > "$output"
    else
      cp "${RC_ASSET_DIR:?}/jianvideo-linux-amd64" "$output"
    fi
    wrote_output=1
    ;;
  */releases/assets/12)
    cp "${RC_ASSET_DIR:?}/jianvideo-windows-amd64.exe" "$output"
    wrote_output=1
    ;;
  */releases/assets/13)
    cp "${RC_ASSET_DIR:?}/checksums.txt" "$output"
    wrote_output=1
    ;;
esac
if [ "$wrote_output" = "0" ]; then
  if [ -n "$output" ]; then
    printf '%s' "$body" > "$output"
  else
    printf '%s' "$body"
  fi
fi
if [ "$write_status" = "1" ]; then
  printf '%s' "$status"
fi
EOF
chmod +x "$temp_dir/verify-bin/curl"

printf '1.0.0-rc.1\n' > "$temp_dir/repo/VERSION"
printf '## 1.0.0-rc.1\n' > "$temp_dir/repo/CHANGELOG.md"
git -C "$temp_dir/repo" init -q
git -C "$temp_dir/repo" add VERSION CHANGELOG.md
GIT_AUTHOR_NAME=测试 GIT_AUTHOR_EMAIL=test@example.invalid GIT_COMMITTER_NAME=测试 GIT_COMMITTER_EMAIL=test@example.invalid git -C "$temp_dir/repo" commit -q -m "候选版本一"
rc1_sha="$(git -C "$temp_dir/repo" rev-parse HEAD)"
git -C "$temp_dir/repo" tag v1.0.0-rc.1
git -C "$temp_dir/repo" tag v1.0.0-rc.3
printf '1.0.0-rc.2\n' > "$temp_dir/repo/VERSION"
printf '## 1.0.0-rc.2\n' >> "$temp_dir/repo/CHANGELOG.md"
git -C "$temp_dir/repo" add VERSION CHANGELOG.md
GIT_AUTHOR_NAME=测试 GIT_AUTHOR_EMAIL=test@example.invalid GIT_COMMITTER_NAME=测试 GIT_COMMITTER_EMAIL=test@example.invalid git -C "$temp_dir/repo" commit -q -m "候选版本二"
rc2_sha="$(git -C "$temp_dir/repo" rev-parse HEAD)"
git -C "$temp_dir/repo" tag v1.0.0-rc.2
printf '1.0.0\n' > "$temp_dir/repo/VERSION"
printf '## 1.0.0（2026-07-18）\n' >> "$temp_dir/repo/CHANGELOG.md"
printf '正式说明\n' > "$temp_dir/repo/README.md"
mkdir -p "$temp_dir/repo/docs" "$temp_dir/repo/.claude/rules"
printf '发布文档\n' > "$temp_dir/repo/docs/release.md"
printf '范围纪律\n' > "$temp_dir/repo/.claude/rules/scope-discipline.md"
git -C "$temp_dir/repo" add VERSION CHANGELOG.md README.md docs/release.md .claude/rules/scope-discipline.md
GIT_AUTHOR_NAME=测试 GIT_AUTHOR_EMAIL=test@example.invalid GIT_COMMITTER_NAME=测试 GIT_COMMITTER_EMAIL=test@example.invalid git -C "$temp_dir/repo" commit -q -m "稳定版本正式文档收口"
ga_sha="$(git -C "$temp_dir/repo" rev-parse HEAD)"

mkdir -p "$temp_dir/rc-assets"
printf 'linux\n' > "$temp_dir/rc-assets/jianvideo-linux-amd64"
printf 'windows\n' > "$temp_dir/rc-assets/jianvideo-windows-amd64.exe"
(
  cd "$temp_dir/rc-assets"
  sha256sum jianvideo-linux-amd64 jianvideo-windows-amd64.exe | sort -k2 > checksums.txt
)
refs12='[{"ref":"refs/tags/v1.0.0-rc.1"},{"ref":"refs/tags/v1.0.0-rc.2"}]'
refs123='[{"ref":"refs/tags/v1.0.0-rc.1"},{"ref":"refs/tags/v1.0.0-rc.2"},{"ref":"refs/tags/v1.0.0-rc.3"}]'
verify_env=(PATH="$temp_dir/verify-bin:$PATH" RC_ASSET_DIR="$temp_dir/rc-assets" GH_TOKEN=test REPOSITORY=test/repo GITHUB_API_URL=https://example.invalid)
assert_succeeds "允许最终 RC 后仅做正式文档收口" env "${verify_env[@]}" MOCK_MATCHING_REFS="$refs12" MOCK_TAG_SHA="$rc2_sha" bash -c 'cd "$1" && bash "$2" verify-final-rc v1.0.0-rc.2 1.0.0 "$3"' _ "$temp_dir/repo" "$script" "$ga_sha"
assert_fails "拒绝指定同基线但不是最高序号的 RC" env "${verify_env[@]}" MOCK_MATCHING_REFS="$refs12" MOCK_TAG_SHA="$rc1_sha" bash -c 'cd "$1" && bash "$2" verify-final-rc v1.0.0-rc.1 1.0.0 "$3"' _ "$temp_dir/repo" "$script" "$ga_sha"
assert_fails "拒绝 API tag ref 不是 commit 类型" env "${verify_env[@]}" MOCK_MATCHING_REFS="$refs12" MOCK_TAG_SHA="$rc2_sha" MOCK_REF_TYPE=tag bash -c 'cd "$1" && bash "$2" verify-final-rc v1.0.0-rc.2 1.0.0 "$3"' _ "$temp_dir/repo" "$script" "$ga_sha"
assert_fails "拒绝 RC commit 的 VERSION 与 tag 不一致" env "${verify_env[@]}" MOCK_MATCHING_REFS="$refs123" MOCK_TAG_SHA="$rc1_sha" bash -c 'cd "$1" && bash "$2" verify-final-rc v1.0.0-rc.3 1.0.0 "$3"' _ "$temp_dir/repo" "$script" "$ga_sha"
missing_id_assets='[{"name":"jianvideo-linux-amd64","state":"uploaded","size":6},{"id":12,"name":"jianvideo-windows-amd64.exe","state":"uploaded","size":8},{"id":13,"name":"checksums.txt","state":"uploaded","size":170}]'
assert_fails "拒绝缺少资产 id 的最终 RC" env "${verify_env[@]}" MOCK_MATCHING_REFS="$refs12" MOCK_TAG_SHA="$rc2_sha" MOCK_RELEASE_ASSETS="$missing_id_assets" bash -c 'cd "$1" && bash "$2" verify-final-rc v1.0.0-rc.2 1.0.0 "$3"' _ "$temp_dir/repo" "$script" "$ga_sha"
assert_fails "拒绝最终 RC 实际下载后二进制校验失败" env "${verify_env[@]}" MOCK_MATCHING_REFS="$refs12" MOCK_TAG_SHA="$rc2_sha" MOCK_BAD_DOWNLOAD=1 bash -c 'cd "$1" && bash "$2" verify-final-rc v1.0.0-rc.2 1.0.0 "$3"' _ "$temp_dir/repo" "$script" "$ga_sha"

mkdir -p "$temp_dir/repo/.github/workflows"
printf 'name: 越界\n' > "$temp_dir/repo/.github/workflows/changed.yml"
git -C "$temp_dir/repo" add .github/workflows/changed.yml
GIT_AUTHOR_NAME=测试 GIT_AUTHOR_EMAIL=test@example.invalid GIT_COMMITTER_NAME=测试 GIT_COMMITTER_EMAIL=test@example.invalid git -C "$temp_dir/repo" commit -q -m "发布自动化越界变化"
bad_sha="$(git -C "$temp_dir/repo" rev-parse HEAD)"
assert_fails "拒绝最终 RC 后修改工作流" env "${verify_env[@]}" MOCK_MATCHING_REFS="$refs12" MOCK_TAG_SHA="$rc2_sha" bash -c 'cd "$1" && bash "$2" verify-final-rc v1.0.0-rc.2 1.0.0 "$3"' _ "$temp_dir/repo" "$script" "$bad_sha"

mkdir -p "$temp_dir/publish-bin"
cat > "$temp_dir/publish-bin/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
method=GET
output=""
write_status=0
data=""
binary_source=""
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -X) method="$2"; shift 2 ;;
    -o) output="$2"; shift 2 ;;
    -w) write_status=1; shift 2 ;;
    -H) shift 2 ;;
    -d) data="$2"; shift 2 ;;
    --data-binary) binary_source="$2"; shift 2 ;;
    -sS|-fsS|-fsSL) shift ;;
    http*) url="$1"; shift ;;
    *) shift ;;
  esac
done
state="${MOCK_STATE:?}"
status=200
body='{}'
wrote_output=0
case "$method $url" in
  "GET "*/git/matching-refs/tags/*)
    body="${MOCK_MATCHING_REFS:-[]}"
    ;;
  "GET "*/releases/tags/*)
    if [ "${MOCK_NO_RELEASE:-0}" = "1" ]; then
      status=404
    elif [ -f "$state/release-request.json" ]; then
      body="$(REQUEST_FILE="$state/release-request.json" FOREIGN_MARKER="${MOCK_FOREIGN_MARKER:-0}" RELEASE_DRAFT="${MOCK_RELEASE_DRAFT:-true}" python - <<'PY'
import json
import os

with open(os.environ["REQUEST_FILE"], encoding="utf-8") as source:
    release = json.load(source)
release["id"] = 123
release["draft"] = os.environ["RELEASE_DRAFT"] == "true"
if os.environ["FOREIGN_MARKER"] == "1":
    release["body"] = "<!-- jianvideo-release-owner:foreign -->"
print(json.dumps(release))
PY
)"
    else
      status=404
    fi
    ;;
  "GET "*/git/ref/tags/*)
    if [ -f "$state/tag-created" ]; then
      body="{\"object\":{\"type\":\"commit\",\"sha\":\"${MOCK_TAG_SHA:-${TARGET_SHA:?}}\"}}"
    else
      status=404
    fi
    ;;
  "POST "*/git/refs)
    touch "$state/tag-created"
    ;;
  "POST "*/releases)
    printf '%s' "$data" > "$state/release-request.json"
    if [ "${MOCK_RESPONSE_BROKEN:-0}" = "1" ]; then
      body='{"id":123}'
    else
      body='{"id":123,"upload_url":"https://uploads.example.invalid/releases/123/assets{?name,label}"}'
    fi
    ;;
  "POST https://uploads.example.invalid"*)
    name="${url##*name=}"
    cp "${binary_source#@}" "$state/upload-$name"
    ;;
  "GET "*/releases/123/assets)
    body='[{"id":11,"name":"jianvideo-linux-amd64","state":"uploaded","size":6},{"id":12,"name":"jianvideo-windows-amd64.exe","state":"uploaded","size":8},{"id":13,"name":"checksums.txt","state":"uploaded","size":170}]'
    ;;
  "GET "*/releases/assets/11)
    if [ "${MOCK_TAMPER_DOWNLOADS:-0}" = "1" ]; then
      printf '替换后的 linux\n' > "$output"
    else
      cp "$state/upload-jianvideo-linux-amd64" "$output"
    fi
    wrote_output=1
    ;;
  "GET "*/releases/assets/12)
    if [ "${MOCK_TAMPER_DOWNLOADS:-0}" = "1" ]; then
      printf '替换后的 windows\n' > "$output"
    else
      cp "$state/upload-jianvideo-windows-amd64.exe" "$output"
    fi
    wrote_output=1
    ;;
  "GET "*/releases/assets/13)
    if [ "${MOCK_TAMPER_DOWNLOADS:-0}" = "1" ]; then
      mkdir -p "$state/tampered"
      printf '替换后的 linux\n' > "$state/tampered/jianvideo-linux-amd64"
      printf '替换后的 windows\n' > "$state/tampered/jianvideo-windows-amd64.exe"
      (cd "$state/tampered" && sha256sum jianvideo-linux-amd64 jianvideo-windows-amd64.exe | sort -k2) > "$output"
    else
      cp "$state/upload-checksums.txt" "$output"
    fi
    wrote_output=1
    ;;
  "PATCH "*/releases/123)
    printf '%s' "$data" > "$state/publish-patch.json"
    touch "$state/published"
    body="$(REQUEST_FILE="$state/release-request.json" PATCH_FILE="$state/publish-patch.json" python - <<'PY'
import json
import os

with open(os.environ["REQUEST_FILE"], encoding="utf-8") as source:
    release = json.load(source)
with open(os.environ["PATCH_FILE"], encoding="utf-8") as source:
    release.update(json.load(source))
release["id"] = 123
print(json.dumps(release))
PY
)"
    ;;
  "DELETE "*/releases/123)
    touch "$state/release-deleted"
    ;;
  "DELETE "*/git/refs/tags/*)
    touch "$state/tag-delete-attempted"
    if [ "${MOCK_DELETE_TAG_FAIL:-0}" = "1" ]; then
      exit 22
    fi
    touch "$state/tag-deleted"
    ;;
  "DELETE "*)
    touch "$state/unexpected-delete"
    ;;
esac
if [ "$wrote_output" = "0" ]; then
  if [ -n "$output" ]; then
    printf '%s' "$body" > "$output"
  else
    printf '%s' "$body"
  fi
fi
if [ "$write_status" = "1" ]; then
  printf '%s' "$status"
fi
EOF
chmod +x "$temp_dir/publish-bin/curl"

target_sha=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
publish_env=(PATH="$temp_dir/publish-bin:$PATH" TARGET_SHA="$target_sha" GH_TOKEN=test REPOSITORY=test/repo GITHUB_API_URL=https://example.invalid)
for state in publish-ga publish-rc publish-tampered cleanup-owned cleanup-tag-delete-fails cleanup-no-release cleanup-foreign cleanup-sha cleanup-published; do
  mkdir -p "$temp_dir/$state/runner"
done
assert_succeeds "GA 使用本地校验和回验并公开" env "${publish_env[@]}" MOCK_STATE="$temp_dir/publish-ga" RUNNER_TEMP="$temp_dir/publish-ga/runner" bash "$script" publish "$temp_dir/assets" v1.0.0 "$target_sha" ga "$temp_dir/notes.md"
assert_patch_contract "GA 显式设为 latest 且公开正文移除 ownership marker" "$temp_dir/publish-ga/publish-patch.json" true "$temp_dir/notes.md"
assert_succeeds "RC rc.1 在 publish 阶段复检后公开" env "${publish_env[@]}" MOCK_STATE="$temp_dir/publish-rc" MOCK_MATCHING_REFS='[]' RUNNER_TEMP="$temp_dir/publish-rc/runner" bash "$script" publish "$temp_dir/assets" v1.0.0-rc.1 "$target_sha" rc "$temp_dir/notes.md"
assert_patch_contract "RC 显式设为非 latest 且公开正文移除 ownership marker" "$temp_dir/publish-rc/publish-patch.json" false "$temp_dir/notes.md"
assert_fails "拒绝二进制与下载校验和同时被替换" env "${publish_env[@]}" MOCK_STATE="$temp_dir/publish-tampered" MOCK_TAMPER_DOWNLOADS=1 RUNNER_TEMP="$temp_dir/publish-tampered/runner" bash "$script" publish "$temp_dir/assets" v1.0.0 "$target_sha" ga "$temp_dir/notes.md"
assert_file_absent "回下载信任根校验失败时不公开 Release" "$temp_dir/publish-tampered/published"

assert_fails "响应解析失败时发布流程保持失败" env "${publish_env[@]}" MOCK_STATE="$temp_dir/cleanup-owned" MOCK_RESPONSE_BROKEN=1 RUNNER_TEMP="$temp_dir/cleanup-owned/runner" bash "$script" publish "$temp_dir/assets" v1.0.0 "$target_sha" ga "$temp_dir/notes.md"
assert_file_exists "清理 marker、tag、SHA、draft 均匹配的 Release" "$temp_dir/cleanup-owned/release-deleted"
assert_file_exists "清理同一归属下的 tag" "$temp_dir/cleanup-owned/tag-deleted"

assert_fails_with_output "tag 删除失败时保留原失败并输出中文告警" "警告：清理本次发布 tag 失败" env "${publish_env[@]}" MOCK_STATE="$temp_dir/cleanup-tag-delete-fails" MOCK_RESPONSE_BROKEN=1 MOCK_DELETE_TAG_FAIL=1 RUNNER_TEMP="$temp_dir/cleanup-tag-delete-fails/runner" bash "$script" publish "$temp_dir/assets" v1.0.0 "$target_sha" ga "$temp_dir/notes.md"
assert_file_exists "tag 删除失败前仍清理本次归属草稿" "$temp_dir/cleanup-tag-delete-fails/release-deleted"
assert_file_exists "tag 删除失败时记录删除尝试" "$temp_dir/cleanup-tag-delete-fails/tag-delete-attempted"
assert_file_absent "tag 删除失败时不伪报删除成功" "$temp_dir/cleanup-tag-delete-fails/tag-deleted"
assert_file_absent "tag 删除失败时不误删其他资源" "$temp_dir/cleanup-tag-delete-fails/unexpected-delete"

assert_fails "draft 未创建时发布流程保持失败" env "${publish_env[@]}" MOCK_STATE="$temp_dir/cleanup-no-release" MOCK_RESPONSE_BROKEN=1 MOCK_NO_RELEASE=1 RUNNER_TEMP="$temp_dir/cleanup-no-release/runner" bash "$script" publish "$temp_dir/assets" v1.0.0 "$target_sha" ga "$temp_dir/notes.md"
assert_file_absent "draft 未创建时无需删除 Release" "$temp_dir/cleanup-no-release/release-deleted"
assert_file_exists "draft 未创建时仍清理本次 tag" "$temp_dir/cleanup-no-release/tag-deleted"

assert_fails "foreign draft 存在时发布流程保持失败" env "${publish_env[@]}" MOCK_STATE="$temp_dir/cleanup-foreign" MOCK_RESPONSE_BROKEN=1 MOCK_FOREIGN_MARKER=1 RUNNER_TEMP="$temp_dir/cleanup-foreign/runner" bash "$script" publish "$temp_dir/assets" v1.0.0 "$target_sha" ga "$temp_dir/notes.md"
assert_file_absent "不删除 marker 不匹配的 foreign draft" "$temp_dir/cleanup-foreign/release-deleted"
assert_file_absent "foreign draft 存在时不删除 tag" "$temp_dir/cleanup-foreign/tag-deleted"

assert_fails "tag 归属变化时发布流程保持失败" env "${publish_env[@]}" MOCK_STATE="$temp_dir/cleanup-sha" MOCK_RESPONSE_BROKEN=1 MOCK_TAG_SHA=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb RUNNER_TEMP="$temp_dir/cleanup-sha/runner" bash "$script" publish "$temp_dir/assets" v1.0.0 "$target_sha" ga "$temp_dir/notes.md"
assert_file_absent "不删除已改指向的草稿 Release" "$temp_dir/cleanup-sha/release-deleted"
assert_file_absent "不删除已改指向的 tag" "$temp_dir/cleanup-sha/tag-deleted"

assert_fails "Release 已非草稿时发布流程保持失败" env "${publish_env[@]}" MOCK_STATE="$temp_dir/cleanup-published" MOCK_RESPONSE_BROKEN=1 MOCK_RELEASE_DRAFT=false RUNNER_TEMP="$temp_dir/cleanup-published/runner" bash "$script" publish "$temp_dir/assets" v1.0.0 "$target_sha" ga "$temp_dir/notes.md"
assert_file_absent "不删除已公开的 Release" "$temp_dir/cleanup-published/release-deleted"
assert_file_absent "存在已公开 Release 时不删除 tag" "$temp_dir/cleanup-published/tag-deleted"

printf '\n通过 %d 项，失败 %d 项。\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
