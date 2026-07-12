#!/usr/bin/env python3
"""校验 GitHub release assets 并输出下载字段。"""

import json
import sys


def fail(message):
    raise SystemExit(f"错误：{message}")


def main():
    try:
        assets = json.load(sys.stdin)
    except (json.JSONDecodeError, UnicodeDecodeError) as error:
        fail(f"发布资产 JSON 无效：{error}")
    if not isinstance(assets, list):
        fail("发布资产 JSON 必须是数组")

    lines = []
    for asset in assets:
        if not isinstance(asset, dict):
            fail("发布资产条目必须是对象")
        asset_id = asset.get("id")
        name = asset.get("name")
        if not isinstance(asset_id, int) or isinstance(asset_id, bool):
            fail("发布资产 id 必须是整数")
        if not isinstance(name, str) or not name:
            fail("发布资产 name 必须是非空字符串")
        if any(character in name for character in "/\\\t\r\n"):
            fail("发布资产 name 含非法字符")
        lines.append(f"{asset_id}\t{name}")

    if lines:
        sys.stdout.write("\n".join(lines) + "\n")


if __name__ == "__main__":
    main()
