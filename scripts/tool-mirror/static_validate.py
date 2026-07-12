#!/usr/bin/env python3
"""对锁文件和工作流执行无需联网的静态安全检查。"""
import re
import sys
from pathlib import Path

root = Path(__file__).resolve().parents[2]
workflow = (root / ".github/workflows/tool-mirror.yml").read_text(encoding="utf-8")
release = (root / "scripts/tool-mirror/release.sh").read_text(encoding="utf-8")
release_assets = root / "scripts/tool-mirror/release_assets.py"
if not release_assets.is_file():
    raise SystemExit("错误：缺少发布资产解析脚本")
required = {
    "ubuntu-24.04", "ubuntu-24.04-arm", "windows-2025",
    "windows-11-arm", "macos-15-intel", "macos-15",
}
missing = sorted(label for label in required if label not in workflow)
if missing:
    raise SystemExit("错误：工作流缺少 runner：" + "、".join(missing))
for line in workflow.splitlines():
    if "uses:" not in line:
        continue
    reference = line.split("uses:", 1)[1].strip().split()[0]
    if not re.fullmatch(r"[^@]+@[0-9a-f]{40}", reference):
        raise SystemExit(f"错误：Action 未锁定完整 SHA：{reference}")
workflow_tokens = (
    "discovery:",
    "python scripts/tool-mirror/static_validate.py",
    "expected_arch: ARM64",
    "jianvideo-tools-${{ matrix.id }}.zip",
    "actions/attest-build-provenance@",
    "attestations: read",
    "group: tool-mirror-release-tools-v1.0.0",
)
release_tokens = (
    '"draft": True',
    "sha256sum -c SHA256SUMS",
    "gh attestation verify",
    "trap cleanup EXIT",
    "release_created=1",
    "require_absent \"tag\"",
    "python scripts/tool-mirror/release_assets.py",
)
for token in workflow_tokens:
    if token not in workflow:
        raise SystemExit(f"错误：工作流缺少可信构建步骤：{token}")
for token in release_tokens:
    if token not in release:
        raise SystemExit(f"错误：发布脚本缺少可信发布步骤：{token}")
print("静态验证通过：runner、Action SHA、发现证据、来源证明和安全发布回滚均已检查")
