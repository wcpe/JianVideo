#!/usr/bin/env bash
# 校验全部 runner、源码、摘要、签名和许可证锁值是否达到可信构建门槛。
set -euo pipefail
python "$(dirname "$0")/tool_mirror.py" preflight "$@"
