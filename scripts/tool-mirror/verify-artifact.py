#!/usr/bin/env python3
"""校验发布包和同目录 manifest 中记录的 SHA-256。"""
import json
import sys
from pathlib import Path
from tool_mirror import sha256_file

manifest = Path(sys.argv[1])
root = Path(sys.argv[2])
data = json.loads(manifest.read_text(encoding="utf-8"))
for item in data.get("files", []):
    path = root / item["path"]
    if not path.is_file() or sha256_file(path) != item["sha256"]:
        raise SystemExit(f"错误：产物校验失败：{item['path']}")
print(f"产物校验通过：{len(data.get('files', []))} 个文件")
