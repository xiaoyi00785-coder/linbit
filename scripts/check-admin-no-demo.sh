#!/usr/bin/env bash
# 后台不得残留演示/Mock 数据：mock.js、mock 导入、DemoBadge、“演示数据”字样。
# 用法: scripts/check-admin-no-demo.sh [admin 源码目录]，命中即退出码 1。
set -uo pipefail
dir=${1:-"$(cd "$(dirname "$0")/.." && pwd)/prototypes/admin/src"}
[ -d "$dir" ] || { echo "check-admin-no-demo: $dir not found"; exit 1; }
hits=$( { find "$dir" -name 'mock.js' -o -name 'mock.jsx' -o -name 'mock.ts'; grep -rnE "from ['\"][./]*mock(\.jsx?)?['\"]|DemoBadge|演示数据|[A-Za-z]+Demo\(\)" "$dir" --include='*.js' --include='*.jsx' --include='*.ts' --include='*.tsx'; } 2>/dev/null)
if [ -n "$hits" ]; then echo "admin demo/mock references found:"; echo "$hits"; exit 1; fi
echo "check-admin-no-demo: clean"
