#!/usr/bin/env bash
# 原子发布六个 ZIP，并只回滚本次创建且仍为草稿的 release/tag。
set -euo pipefail

dist="${1:?用法：release.sh <产物目录> <固定 tag>}"
tag="${2:?用法：release.sh <产物目录> <固定 tag>}"
: "${GH_TOKEN:?缺少 GH_TOKEN}"
: "${REPOSITORY:?缺少 REPOSITORY}"
: "${TARGET_SHA:?缺少 TARGET_SHA}"

api="https://api.github.com/repos/$REPOSITORY"
auth=(-H "Authorization: Bearer $GH_TOKEN" -H "X-GitHub-Api-Version: 2022-11-28")
json_accept=(-H "Accept: application/vnd.github+json")
asset_accept=(-H "Accept: application/octet-stream")
release_id=""
release_created=0
api_output="${RUNNER_TEMP:-/tmp}/jianvideo-release-api.json"

api_status() {
  local url="$1"
  curl -sS -o "$api_output" -w '%{http_code}' "${auth[@]}" "${json_accept[@]}" "$url"
}

require_absent() {
  local kind="$1" url="$2" status
  status="$(api_status "$url")"
  case "$status" in
    404) return 0 ;;
    200) echo "错误：固定 tag 的既有 $kind 已存在，拒绝覆盖：$tag" >&2; return 1 ;;
    *) echo "错误：检查既有 $kind 失败，HTTP $status" >&2; return 1 ;;
  esac
}

owned_draft_exists() {
  local status
  status="$(api_status "$api/releases/$release_id")"
  [ "$status" = "200" ] || return 1
  python -c 'import json,sys; release=json.load(sys.stdin); raise SystemExit(0 if release.get("draft") is True and release.get("tag_name") == sys.argv[1] else 1)' "$tag" <"$api_output"
}

owned_tag_exists() {
  local status
  status="$(api_status "$api/git/ref/tags/$tag")"
  [ "$status" = "200" ] || return 1
  python -c 'import json,sys; reference=json.load(sys.stdin); raise SystemExit(0 if reference.get("object", {}).get("sha") == sys.argv[1] else 1)' "$TARGET_SHA" <"$api_output"
}

delete_owned_draft() {
  owned_draft_exists || return 1
  curl -fsS -X DELETE "${auth[@]}" "$api/releases/$release_id" >/dev/null
}

cleanup() {
  local status=$?
  if [ "$status" -ne 0 ] && [ "$release_created" = "1" ] && delete_owned_draft; then
    if owned_tag_exists; then
      curl -fsS -X DELETE "${auth[@]}" "$api/git/refs/tags/$tag" >/dev/null || true
    fi
  fi
  exit "$status"
}
trap cleanup EXIT

require_absent "release" "$api/releases/tags/$tag"
require_absent "tag" "$api/git/ref/tags/$tag"
mapfile -d '' zip_files < <(find "$dist" -maxdepth 1 -type f -name '*.zip' -print0 | sort -z)
[ "${#zip_files[@]}" -eq 6 ] || { echo "错误：发布必须恰好包含六个 ZIP" >&2; exit 1; }
(
  cd "$dist"
  for file in *.zip; do sha256sum "$file"; done | sort -k2 > SHA256SUMS
)

response="$(curl -fsS -X POST "${auth[@]}" "$api/releases" -d "$(TAG="$tag" python - <<'PY'
import json, os
print(json.dumps({
    "tag_name": os.environ["TAG"],
    "target_commitish": os.environ["TARGET_SHA"],
    "name": f"JianVideo 工具镜像 {os.environ['TAG']}",
    "draft": True,
    "prerelease": False,
}))
PY
)")"
release_id="$(python -c 'import json,sys; print(json.load(sys.stdin)["id"])' <<<"$response")"
upload_url="$(python -c 'import json,sys; print(json.load(sys.stdin)["upload_url"].split("{")[0])' <<<"$response")"
release_created=1

for file in "${zip_files[@]}" "$dist/SHA256SUMS"; do
  name="$(basename "$file")"
  curl -fsS -X POST "${auth[@]}" -H "Content-Type: application/octet-stream" --data-binary "@$file" "$upload_url?name=$name" >/dev/null
done

rm -rf redownload
mkdir redownload
assets="$(curl -fsS "${auth[@]}" "${json_accept[@]}" "$api/releases/$release_id/assets")"
while IFS=$'\t' read -r asset_id name; do
  case "$name" in */*|*\\*) echo "错误：release asset 名称越界：$name" >&2; exit 1 ;; esac
  curl -fsSL "${auth[@]}" "${asset_accept[@]}" "$api/releases/assets/$asset_id" -o "redownload/$name"
done < <(python scripts/tool-mirror/release_assets.py <<<"$assets")

[ "$(find redownload -maxdepth 1 -type f -name '*.zip' | wc -l | tr -d ' ')" = "6" ] || { echo "错误：回下载 ZIP 数量不是六个" >&2; exit 1; }
(cd redownload && sha256sum -c SHA256SUMS)
for file in redownload/*.zip; do
  gh attestation verify "$file" -R "$REPOSITORY"
done
curl -fsS -X PATCH "${auth[@]}" "$api/releases/$release_id" -d '{"draft":false}' >/dev/null
trap - EXIT
echo "Release $tag 已正式发布"
