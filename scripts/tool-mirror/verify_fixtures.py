#!/usr/bin/env python3
"""使用可再分发 HEIC/RAW fixture 验证 ImageMagick 真实 JPEG 转换。"""

from __future__ import annotations

import argparse
import json
import subprocess
import sys
import tempfile
from pathlib import Path

from tool_mirror import SHA256_LENGTH, MirrorError, sha256_file

ROOT = Path(__file__).resolve().parent
DEFAULT_MANIFEST = ROOT / "fixtures" / "manifest.json"
REQUIRED_KINDS = {"heic", "raw"}


def valid_sha256(value: str) -> bool:
    return len(value) == SHA256_LENGTH and all(char in "0123456789abcdef" for char in value)


def validate_manifest(data: dict) -> list[str]:
    errors: list[str] = []
    fixtures = data.get("fixtures", [])
    kinds = {item.get("kind") for item in fixtures}
    if "heic" not in kinds:
        errors.append("fixture 清单缺少 HEIC 样本")
    if "raw" not in kinds:
        errors.append("fixture 清单缺少 RAW 样本")
    for item in fixtures:
        name = item.get("id", "<未知 fixture>")
        for field in ("kind", "path", "sha256", "source_url", "license", "license_url"):
            if not item.get(field):
                errors.append(f"{name}: 缺少 {field}")
        if item.get("kind") not in REQUIRED_KINDS:
            errors.append(f"{name}: kind 必须是 heic 或 raw")
        if item.get("sha256") and not valid_sha256(item["sha256"]):
            errors.append(f"{name}: SHA-256 格式无效")
        for field in ("source_url", "license_url"):
            if item.get(field) and not item[field].startswith("https://"):
                errors.append(f"{name}: {field} 必须使用 HTTPS")
    return errors


def is_jpeg(content: bytes) -> bool:
    return len(content) >= 4 and content.startswith(b"\xff\xd8") and content.endswith(b"\xff\xd9")


def verify_fixture(magick: Path, fixture_root: Path, item: dict, output_root: Path) -> None:
    source = (fixture_root / item["path"]).resolve()
    if fixture_root.resolve() not in source.parents or not source.is_file():
        raise MirrorError(f"{item['id']}: fixture 文件缺失或路径越界")
    if sha256_file(source) != item["sha256"]:
        raise MirrorError(f"{item['id']}: fixture SHA-256 不匹配")
    output = output_root / f"{item['id']}.jpg"
    subprocess.run([str(magick), str(source), "-auto-orient", "-quality", "90", str(output)], check=True)
    if not output.is_file() or not is_jpeg(output.read_bytes()):
        raise MirrorError(f"{item['id']}: 未生成合法 JPEG")
    result = subprocess.run(
        [str(magick), "identify", "-format", "%m", str(output)],
        check=True,
        text=True,
        capture_output=True,
    )
    if result.stdout.strip() != "JPEG":
        raise MirrorError(f"{item['id']}: ImageMagick 未识别转换结果为 JPEG")


def verify_all(magick: Path, manifest: Path) -> None:
    if not manifest.is_file():
        raise MirrorError(
            f"delegate fixture 未锁定：缺少 {manifest}；必须加入带许可证和来源元数据的真实 HEIC/RAW 样本"
        )
    data = json.loads(manifest.read_text(encoding="utf-8"))
    errors = validate_manifest(data)
    if errors:
        raise MirrorError("delegate fixture 清单无效：\n- " + "\n- ".join(errors))
    with tempfile.TemporaryDirectory(prefix="jianvideo-fixture-") as temp:
        for item in data["fixtures"]:
            verify_fixture(magick, manifest.parent, item, Path(temp))


def main() -> int:
    parser = argparse.ArgumentParser(description="验证 HEIC/RAW delegate 的真实 JPEG 转换")
    parser.add_argument("--magick", type=Path, required=True)
    parser.add_argument("--manifest", type=Path, default=DEFAULT_MANIFEST)
    args = parser.parse_args()
    try:
        verify_all(args.magick, args.manifest)
    except (MirrorError, OSError, json.JSONDecodeError, subprocess.CalledProcessError) as error:
        print(f"错误：{error}", file=sys.stderr)
        return 1
    print("HEIC/RAW delegate 真实转换验证通过")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
