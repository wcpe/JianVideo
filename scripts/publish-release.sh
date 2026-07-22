#!/usr/bin/env bash
# 安全创建 GitHub tag 与 Release。失败时只清理带有本次唯一归属标记的草稿与 tag。
set -euo pipefail

: "${GH_TOKEN:?错误：缺少 GH_TOKEN}"
: "${REPOSITORY:?错误：缺少 REPOSITORY}"

api_base="${GITHUB_API_URL:-https://api.github.com}/repos/$REPOSITORY"
auth=(-H "Authorization: Bearer $GH_TOKEN" -H "X-GitHub-Api-Version: 2022-11-28")
json_headers=(-H "Accept: application/vnd.github+json" -H "Content-Type: application/json")
asset_headers=(-H "Accept: application/octet-stream")
api_output="${RUNNER_TEMP:-/tmp}/jianvideo-release-api-$$.json"
cleanup_tag=""
cleanup_target_sha=""
cleanup_payload=""
cleanup_redownload=""
cleanup_marker=""
cleanup_tag_created=0

api_status() {
  local url="$1"
  curl -sS -o "$api_output" -w '%{http_code}' "${auth[@]}" "${json_headers[@]}" "$url"
}

require_absent() {
  local kind="$1" url="$2" status
  status="$(api_status "$url")"
  case "$status" in
    404) return 0 ;;
    200) echo "错误：目标 $kind 已存在，拒绝覆盖。" >&2; return 1 ;;
    *) echo "错误：检查目标 $kind 失败，HTTP $status。" >&2; return 1 ;;
  esac
}

remote_rc_max() {
  local base="$1" status
  status="$(api_status "$api_base/git/matching-refs/tags/$base-rc.")"
  [ "$status" = "200" ] || { echo "错误：读取同基线 RC tag 失败，HTTP $status。" >&2; return 1; }
  API_OUTPUT="$api_output" RC_BASE="$base" python - <<'PY' || { echo "错误：同基线 RC tag 响应格式无效。" >&2; return 1; }
import json
import os
import re

with open(os.environ["API_OUTPUT"], encoding="utf-8") as source:
    references = json.load(source)
if not isinstance(references, list):
    raise SystemExit(1)
pattern = re.compile(r"^refs/tags/" + re.escape(os.environ["RC_BASE"]) + r"-rc\.([1-9][0-9]*)$")
numbers = []
for reference in references:
    if not isinstance(reference, dict):
        raise SystemExit(1)
    match = pattern.fullmatch(reference.get("ref", ""))
    if match:
        numbers.append(int(match.group(1)))
print(max(numbers, default=0))
PY
}

require_next_rc() {
  local tag="$1" base number max expected
  [[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+-rc\.[1-9][0-9]*$ ]] || { echo "错误：RC tag 格式无效。" >&2; return 1; }
  base="${tag%-rc.*}"
  number="${tag##*.}"
  max="$(remote_rc_max "$base")"
  expected=$((max + 1))
  [ "$number" -eq "$expected" ] || {
    echo "错误：新 RC 必须是同基线严格下一个序号，当前应为 ${base}-rc.${expected}。" >&2
    return 1
  }
}

require_highest_rc() {
  local tag="$1" base number max
  base="${tag%-rc.*}"
  number="${tag##*.}"
  max="$(remote_rc_max "$base")"
  [ "$max" -gt 0 ] && [ "$number" -eq "$max" ] || {
    echo "错误：指定 RC 不是同基线最高现有 RC。" >&2
    return 1
  }
}

preflight_release() {
  local tag="$1"
  [[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-rc\.[1-9][0-9]*)?$ ]] || { echo "错误：预检 tag 格式无效。" >&2; return 1; }
  require_absent "Release" "$api_base/releases/tags/$tag" || return 1
  require_absent "tag" "$api_base/git/ref/tags/$tag"
}

# 推送 tag 触发发布：tag 已存在，仅要求同名 Release 不存在。
preflight_existing_release() {
  local tag="$1"
  [[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-rc\.[1-9][0-9]*)?$ ]] || { echo "错误：预检 tag 格式无效。" >&2; return 1; }
  require_absent "Release" "$api_base/releases/tags/$tag"
}

preflight_rc() {
  local tag="$1"
  require_next_rc "$tag" || return 1
  preflight_release "$tag"
}

# 推送 RC tag 后的预检：Release 不存在，且该 tag 必须已是同基线最高 RC。
preflight_rc_release() {
  local tag="$1"
  require_highest_rc "$tag" || return 1
  preflight_existing_release "$tag"
}

require_tag_points_to() {
  local tag="$1" target_sha="$2" status
  status="$(api_status "$api_base/git/ref/tags/$tag")"
  [ "$status" = "200" ] || { echo "错误：找不到已推送的 tag：$tag，HTTP $status。" >&2; return 1; }
  reference_matches_commit "$target_sha" || { echo "错误：tag $tag 未指向源码提交 $target_sha。" >&2; return 1; }
}

validate_assets() {
  local dist="$1" file
  local expected=(
    jianvideo-linux-amd64
    jianvideo-linux-amd64.sha256
    jianvideo-windows-amd64.exe
    jianvideo-windows-amd64.sha256
  )
  [ -d "$dist" ] || { echo "错误：产物目录不存在：$dist" >&2; return 1; }
  for file in "${expected[@]}"; do
    [ -f "$dist/$file" ] || { echo "错误：缺少构建产物：$file" >&2; return 1; }
  done
  [ "$(find "$dist" -maxdepth 1 -type f | wc -l | tr -d ' ')" = "4" ] || {
    echo "错误：构建产物目录必须恰好包含两个二进制与两个平台校验文件。" >&2
    return 1
  }
  (cd "$dist" && sha256sum -c jianvideo-linux-amd64.sha256 && sha256sum -c jianvideo-windows-amd64.sha256)
}

prepare_payload() {
  local dist="$1" payload="$2"
  rm -rf "$payload"
  mkdir -p "$payload"
  cp "$dist/jianvideo-linux-amd64" "$payload/"
  cp "$dist/jianvideo-windows-amd64.exe" "$payload/"
  (
    cd "$payload"
    sha256sum jianvideo-linux-amd64 jianvideo-windows-amd64.exe | sort -k2 > checksums.txt
  )
}

reference_matches_commit() {
  local expected_sha="$1"
  EXPECTED_SHA="$expected_sha" API_OUTPUT="$api_output" python - <<'PY'
import json
import os

with open(os.environ["API_OUTPUT"], encoding="utf-8") as source:
    reference = json.load(source)
obj = reference.get("object", {})
valid = obj.get("type") == "commit" and obj.get("sha") == os.environ["EXPECTED_SHA"]
raise SystemExit(0 if valid else 1)
PY
}

write_public_rc_asset_rows() {
  local release_json="$1" rc_tag="$2" rows="$3"
  RELEASE_JSON="$release_json" RC_TAG="$rc_tag" ROWS="$rows" python - <<'PY'
import json
import os

with open(os.environ["RELEASE_JSON"], encoding="utf-8") as source:
    release = json.load(source)
assets = release.get("assets")
expected = {"jianvideo-linux-amd64", "jianvideo-windows-amd64.exe", "checksums.txt"}
valid_assets = (
    isinstance(assets, list)
    and len(assets) == 3
    and {asset.get("name") for asset in assets if isinstance(asset, dict)} == expected
    and all(
        isinstance(asset.get("id"), int)
        and not isinstance(asset.get("id"), bool)
        and asset["id"] > 0
        and isinstance(asset.get("size"), int)
        and not isinstance(asset.get("size"), bool)
        and asset["size"] > 0
        and asset.get("state") == "uploaded"
        for asset in assets
    )
)
valid_release = (
    release.get("tag_name") == os.environ["RC_TAG"]
    and release.get("draft") is False
    and release.get("prerelease") is True
)
if not valid_release or not valid_assets:
    raise SystemExit(1)
with open(os.environ["ROWS"], "w", encoding="utf-8", newline="\n") as target:
    for asset in sorted(assets, key=lambda item: item["name"]):
        target.write(f'{asset["id"]}\t{asset["name"]}\n')
PY
}

write_uploaded_asset_rows() {
  local assets_json="$1" rows="$2"
  ASSETS_JSON="$assets_json" ROWS="$rows" python - <<'PY'
import json
import os

with open(os.environ["ASSETS_JSON"], encoding="utf-8") as source:
    assets = json.load(source)
expected = {"jianvideo-linux-amd64", "jianvideo-windows-amd64.exe", "checksums.txt"}
valid = (
    isinstance(assets, list)
    and len(assets) == 3
    and {asset.get("name") for asset in assets if isinstance(asset, dict)} == expected
    and all(
        isinstance(asset.get("id"), int)
        and not isinstance(asset.get("id"), bool)
        and asset["id"] > 0
        and isinstance(asset.get("size"), int)
        and not isinstance(asset.get("size"), bool)
        and asset["size"] > 0
        and asset.get("state") == "uploaded"
        for asset in assets
    )
)
if not valid:
    raise SystemExit(1)
with open(os.environ["ROWS"], "w", encoding="utf-8", newline="\n") as target:
    for asset in sorted(assets, key=lambda item: item["name"]):
        target.write(f'{asset["id"]}\t{asset["name"]}\n')
PY
}

validate_checksum_manifest() {
  local checksum_file="$1"
  CHECKSUM_FILE="$checksum_file" python - <<'PY'
import os
import re

expected = {"jianvideo-linux-amd64", "jianvideo-windows-amd64.exe"}
with open(os.environ["CHECKSUM_FILE"], encoding="utf-8") as source:
    lines = source.read().splitlines()
if len(lines) != 2:
    raise SystemExit(1)
names = set()
for line in lines:
    match = re.fullmatch(r"[0-9a-fA-F]{64} [ *](.+)", line)
    if not match:
        raise SystemExit(1)
    names.add(match.group(1))
raise SystemExit(0 if names == expected else 1)
PY
}

download_asset_rows() {
  local rows="$1" destination="$2" asset_id name
  mkdir -p "$destination"
  while IFS=$'\t' read -r asset_id name; do
    curl -fsSL "${auth[@]}" "${asset_headers[@]}" "$api_base/releases/assets/$asset_id" -o "$destination/$name"
  done < "$rows"
}

verify_final_rc_assets() {
  local rc_tag="$1" verify_dir="$2" status rows="$verify_dir/assets.tsv"
  status="$(api_status "$api_base/releases/tags/$rc_tag")"
  [ "$status" = "200" ] || { echo "错误：读取最终 RC Release 失败，HTTP $status。" >&2; return 1; }
  mkdir -p "$verify_dir"
  write_public_rc_asset_rows "$api_output" "$rc_tag" "$rows" || { echo "错误：最终 RC Release 或资产元数据无效。" >&2; return 1; }
  download_asset_rows "$rows" "$verify_dir"
  validate_checksum_manifest "$verify_dir/checksums.txt" || { echo "错误：最终 RC checksums.txt 格式无效。" >&2; return 1; }
  (cd "$verify_dir" && sha256sum -c checksums.txt) || { echo "错误：最终 RC 资产校验失败。" >&2; return 1; }
}

verify_final_rc() {
  local rc_tag="$1" version="$2" ga_sha="$3" rc_commit rc_version status changed
  local verify_dir="${RUNNER_TEMP:-/tmp}/jianvideo-final-rc-$$"
  [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "错误：GA 版本格式无效。" >&2; return 1; }
  [[ "$rc_tag" =~ ^v${version//./\.}-rc\.[1-9][0-9]*$ ]] || { echo "错误：最终 RC tag 与 GA 版本基线不一致。" >&2; return 1; }
  rc_commit="$(git rev-parse "${rc_tag}^{commit}" 2>/dev/null)" || { echo "错误：找不到最终 RC tag：$rc_tag" >&2; return 1; }
  status="$(api_status "$api_base/git/ref/tags/$rc_tag")"
  [ "$status" = "200" ] && reference_matches_commit "$rc_commit" || { echo "错误：API tag ref 必须以 commit 类型指向本地 RC commit。" >&2; return 1; }
  require_highest_rc "$rc_tag"
  rc_version="$(git show "$rc_commit:VERSION" 2>/dev/null)" || { echo "错误：无法读取最终 RC 的 VERSION。" >&2; return 1; }
  rc_version="${rc_version%$'\r'}"
  [ "$rc_version" = "${rc_tag#v}" ] || { echo "错误：最终 RC commit 的 VERSION 与 tag 不一致。" >&2; return 1; }
  git merge-base --is-ancestor "$rc_commit" "$ga_sha" || { echo "错误：最终 RC 不是 GA 源码提交的祖先。" >&2; return 1; }
  while IFS= read -r changed; do
    [ -z "$changed" ] && continue
    case "$changed" in
      VERSION|CHANGELOG.md|README.md|docs/*|.claude/rules/scope-discipline.md) ;;
      *) echo "错误：最终 RC 之后存在非正式发布文档变更：$changed" >&2; return 1 ;;
    esac
  done < <(git diff --name-only "$rc_commit..$ga_sha")
  rm -rf "$verify_dir"
  verify_final_rc_assets "$rc_tag" "$verify_dir" || { rm -rf "$verify_dir"; return 1; }
  rm -rf "$verify_dir"
  echo "最终 RC 完整校验通过：$rc_tag"
}

owned_draft_id() {
  local tag="$1" marker="$2"
  EXPECTED_TAG="$tag" EXPECTED_MARKER="$marker" API_OUTPUT="$api_output" python - <<'PY'
import json
import os

with open(os.environ["API_OUTPUT"], encoding="utf-8") as source:
    release = json.load(source)
release_id = release.get("id")
valid = (
    isinstance(release_id, int)
    and not isinstance(release_id, bool)
    and release_id > 0
    and release.get("draft") is True
    and release.get("tag_name") == os.environ["EXPECTED_TAG"]
    and os.environ["EXPECTED_MARKER"] in release.get("body", "").splitlines()
)
if not valid:
    raise SystemExit(1)
print(release_id)
PY
}

cleanup_owned_release() {
  local status release_id can_delete_tag=0
  status="$(api_status "$api_base/git/ref/tags/$cleanup_tag")" || return
  [ "$status" = "200" ] && reference_matches_commit "$cleanup_target_sha" || return
  status="$(api_status "$api_base/releases/tags/$cleanup_tag")" || return
  if [ "$status" = "404" ]; then
    can_delete_tag=1
  elif [ "$status" = "200" ]; then
    release_id="$(owned_draft_id "$cleanup_tag" "$cleanup_marker")" || return
    curl -fsS -X DELETE "${auth[@]}" "$api_base/releases/$release_id" >/dev/null || return
    can_delete_tag=1
  else
    return
  fi
  [ "$can_delete_tag" = "1" ] || return
  status="$(api_status "$api_base/git/ref/tags/$cleanup_tag")" || return
  [ "$status" = "200" ] && reference_matches_commit "$cleanup_target_sha" || return
  curl -fsS -X DELETE "${auth[@]}" "$api_base/git/refs/tags/$cleanup_tag" >/dev/null || echo "警告：清理本次发布 tag 失败：$cleanup_tag，请人工核对。" >&2
}

cleanup_publish() {
  local exit_status=$?
  trap - EXIT
  if [ "$exit_status" -ne 0 ] && [ "$cleanup_tag_created" = "1" ]; then
    cleanup_owned_release
  fi
  rm -rf "$cleanup_payload" "$cleanup_redownload"
  exit "$exit_status"
}

create_tag() {
  local tag="$1" target_sha="$2"
  curl -fsS -X POST "${auth[@]}" "${json_headers[@]}" "$api_base/git/refs" -d "$(REF="refs/tags/$tag" SHA="$target_sha" python - <<'PY'
import json
import os

print(json.dumps({"ref": os.environ["REF"], "sha": os.environ["SHA"]}))
PY
)" >/dev/null
}

create_draft_release() {
  local tag="$1" target_sha="$2" prerelease="$3" make_latest="$4" notes="$5" marker="$6"
  TAG="$tag" SHA="$target_sha" PRERELEASE="$prerelease" MAKE_LATEST="$make_latest" NOTES="$notes" MARKER="$marker" python - <<'PY' > "$api_output"
import json
import os

with open(os.environ["NOTES"], encoding="utf-8") as source:
    public_body = source.read()
draft_body = public_body
if draft_body and not draft_body.endswith("\n"):
    draft_body += "\n"
draft_body += "\n" + os.environ["MARKER"]
print(json.dumps({
    "tag_name": os.environ["TAG"],
    "target_commitish": os.environ["SHA"],
    "name": os.environ["TAG"],
    "body": draft_body,
    "draft": True,
    "prerelease": os.environ["PRERELEASE"] == "true",
    "make_latest": os.environ["MAKE_LATEST"],
}))
PY
  curl -fsS -X POST "${auth[@]}" "${json_headers[@]}" "$api_base/releases" -d "$(< "$api_output")"
}

upload_release_assets() {
  local payload="$1" upload_url="$2" file name
  for file in "$payload/jianvideo-linux-amd64" "$payload/jianvideo-windows-amd64.exe" "$payload/checksums.txt"; do
    name="$(basename "$file")"
    curl -fsS -X POST "${auth[@]}" -H "Content-Type: application/octet-stream" --data-binary "@$file" "$upload_url?name=$name" >/dev/null
  done
}

verify_uploaded_assets() {
  local release_id="$1" payload="$2" redownload="$3" rows="$redownload/assets.tsv"
  mkdir -p "$redownload"
  curl -fsS "${auth[@]}" "${json_headers[@]}" "$api_base/releases/$release_id/assets" -o "$api_output"
  write_uploaded_asset_rows "$api_output" "$rows" || { echo "错误：上传后的资产集合或元数据无效。" >&2; return 1; }
  download_asset_rows "$rows" "$redownload"
  cmp -s "$payload/checksums.txt" "$redownload/checksums.txt" || { echo "错误：下载的 checksums.txt 与本地信任根不一致。" >&2; return 1; }
  (cd "$redownload" && sha256sum -c "$payload/checksums.txt") || { echo "错误：下载资产与本地校验和不一致。" >&2; return 1; }
}

publish_draft_release() {
  local release_id="$1" tag="$2" prerelease="$3" make_latest="$4" notes="$5" published
  MAKE_LATEST="$make_latest" NOTES="$notes" python - <<'PY' > "$api_output"
import json
import os

with open(os.environ["NOTES"], encoding="utf-8") as source:
    body = source.read()
print(json.dumps({"draft": False, "body": body, "make_latest": os.environ["MAKE_LATEST"]}))
PY
  published="$(curl -fsS -X PATCH "${auth[@]}" "${json_headers[@]}" "$api_base/releases/$release_id" -d "$(< "$api_output")")"
  PUBLISHED="$published" EXPECTED_TAG="$tag" EXPECTED_PRERELEASE="$prerelease" NOTES="$notes" python - <<'PY'
import json
import os

release = json.loads(os.environ["PUBLISHED"])
with open(os.environ["NOTES"], encoding="utf-8") as source:
    body = source.read()
valid = (
    release.get("tag_name") == os.environ["EXPECTED_TAG"]
    and release.get("draft") is False
    and release.get("prerelease") is (os.environ["EXPECTED_PRERELEASE"] == "true")
    and release.get("body") == body
    and "jianvideo-release-owner" not in release.get("body", "")
)
raise SystemExit(0 if valid else 1)
PY
}

publish_release() {
  local dist="$1" tag="$2" target_sha="$3" channel="$4" notes="$5"
  local prerelease make_latest payload redownload ownership_marker response release_id upload_url
  payload="${RUNNER_TEMP:-/tmp}/jianvideo-release-payload-$$"
  redownload="${RUNNER_TEMP:-/tmp}/jianvideo-release-redownload-$$"
  prerelease=false
  case "$channel" in
    rc) [[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+-rc\.[1-9][0-9]*$ ]] || { echo "错误：RC tag 格式无效。" >&2; return 1; }; prerelease=true; make_latest=false ;;
    ga) [[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "错误：GA tag 格式无效。" >&2; return 1; }; make_latest=true ;;
    *) echo "错误：发布频道必须是 rc 或 ga。" >&2; return 1 ;;
  esac
  [ -s "$notes" ] || { echo "错误：发布说明文件不存在或内容为空。" >&2; return 1; }
  validate_assets "$dist"
  prepare_payload "$dist" "$payload"
  if [ "$channel" = "rc" ]; then
    preflight_rc "$tag" || { rm -rf "$payload"; return 1; }
  else
    preflight_release "$tag" || { rm -rf "$payload"; return 1; }
  fi
  ownership_marker="<!-- jianvideo-release-owner:$(python -c 'import secrets; print(secrets.token_hex(32))') -->"
  cleanup_tag="$tag"
  cleanup_target_sha="$target_sha"
  cleanup_payload="$payload"
  cleanup_redownload="$redownload"
  cleanup_marker="$ownership_marker"
  cleanup_tag_created=0
  trap cleanup_publish EXIT
  create_tag "$tag" "$target_sha"
  cleanup_tag_created=1
  response="$(create_draft_release "$tag" "$target_sha" "$prerelease" "$make_latest" "$notes" "$ownership_marker")"
  release_id="$(python -c 'import json,sys; value=json.load(sys.stdin).get("id"); valid=isinstance(value, int) and not isinstance(value, bool) and value > 0; sys.exit(1) if not valid else print(value)' <<< "$response")"
  upload_url="$(python -c 'import json,sys; value=json.load(sys.stdin).get("upload_url", "").split("{")[0]; sys.exit(1) if not value else print(value)' <<< "$response")"
  upload_release_assets "$payload" "$upload_url"
  verify_uploaded_assets "$release_id" "$payload" "$redownload"
  publish_draft_release "$release_id" "$tag" "$prerelease" "$make_latest" "$notes"
  trap - EXIT
  rm -rf "$payload" "$redownload"
  echo "Release $tag 已安全公开。"
}

# 推送 tag 触发：不创建 tag，只创建 draft Release、回下载校验后公开。
publish_from_tag() {
  local dist="$1" tag="$2" target_sha="$3" channel="$4" notes="$5"
  local prerelease make_latest payload redownload ownership_marker response release_id upload_url
  payload="${RUNNER_TEMP:-/tmp}/jianvideo-release-payload-$$"
  redownload="${RUNNER_TEMP:-/tmp}/jianvideo-release-redownload-$$"
  prerelease=false
  case "$channel" in
    rc) [[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+-rc\.[1-9][0-9]*$ ]] || { echo "错误：RC tag 格式无效。" >&2; return 1; }; prerelease=true; make_latest=false ;;
    ga) [[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "错误：GA tag 格式无效。" >&2; return 1; }; make_latest=true ;;
    *) echo "错误：发布频道必须是 rc 或 ga。" >&2; return 1 ;;
  esac
  [ -s "$notes" ] || { echo "错误：发布说明文件不存在或内容为空。" >&2; return 1; }
  validate_assets "$dist"
  prepare_payload "$dist" "$payload"
  require_tag_points_to "$tag" "$target_sha" || { rm -rf "$payload"; return 1; }
  if [ "$channel" = "rc" ]; then
    preflight_rc_release "$tag" || { rm -rf "$payload"; return 1; }
  else
    preflight_existing_release "$tag" || { rm -rf "$payload"; return 1; }
  fi
  ownership_marker="<!-- jianvideo-release-owner:$(python -c 'import secrets; print(secrets.token_hex(32))') -->"
  cleanup_tag="$tag"
  cleanup_target_sha="$target_sha"
  cleanup_payload="$payload"
  cleanup_redownload="$redownload"
  cleanup_marker="$ownership_marker"
  # tag 由人工推送，失败时只清理本次 draft Release，不删除 tag。
  cleanup_tag_created=0
  trap cleanup_publish EXIT
  response="$(create_draft_release "$tag" "$target_sha" "$prerelease" "$make_latest" "$notes" "$ownership_marker")"
  release_id="$(python -c 'import json,sys; value=json.load(sys.stdin).get("id"); valid=isinstance(value, int) and not isinstance(value, bool) and value > 0; sys.exit(1) if not valid else print(value)' <<< "$response")"
  upload_url="$(python -c 'import json,sys; value=json.load(sys.stdin).get("upload_url", "").split("{")[0]; sys.exit(1) if not value else print(value)' <<< "$response")"
  upload_release_assets "$payload" "$upload_url"
  verify_uploaded_assets "$release_id" "$payload" "$redownload"
  publish_draft_release "$release_id" "$tag" "$prerelease" "$make_latest" "$notes"
  trap - EXIT
  rm -rf "$payload" "$redownload"
  echo "Release $tag 已安全公开。"
}

case "${1:-}" in
  preflight)
    [ "$#" -eq 2 ] || { echo "用法：publish-release.sh preflight <tag>" >&2; exit 2; }
    preflight_release "$2"
    ;;
  preflight-release)
    [ "$#" -eq 2 ] || { echo "用法：publish-release.sh preflight-release <tag>" >&2; exit 2; }
    preflight_existing_release "$2"
    ;;
  preflight-rc)
    [ "$#" -eq 2 ] || { echo "用法：publish-release.sh preflight-rc <rc-tag>" >&2; exit 2; }
    preflight_rc "$2"
    ;;
  preflight-rc-release)
    [ "$#" -eq 2 ] || { echo "用法：publish-release.sh preflight-rc-release <rc-tag>" >&2; exit 2; }
    preflight_rc_release "$2"
    ;;
  verify-final-rc)
    [ "$#" -eq 4 ] || { echo "用法：publish-release.sh verify-final-rc <rc-tag> <ga-version> <ga-sha>" >&2; exit 2; }
    verify_final_rc "$2" "$3" "$4"
    ;;
  publish)
    [ "$#" -eq 6 ] || { echo "用法：publish-release.sh publish <产物目录> <tag> <source-sha> <rc|ga> <说明文件>" >&2; exit 2; }
    publish_release "$2" "$3" "$4" "$5" "$6"
    ;;
  publish-from-tag)
    [ "$#" -eq 6 ] || { echo "用法：publish-release.sh publish-from-tag <产物目录> <tag> <source-sha> <rc|ga> <说明文件>" >&2; exit 2; }
    publish_from_tag "$2" "$3" "$4" "$5" "$6"
    ;;
  *)
    echo "错误：未知命令。" >&2
    exit 2
    ;;
esac
