#!/bin/sh
# 后台不得残留演示/Mock 数据：mock.js、mock 导入、DemoBadge、“演示数据”字样。
# 用法: scripts/check-admin-no-demo.sh [admin 源码目录]，命中即退出码 1。
set -u  # POSIX sh (dash in node:*-slim): no pipefail, no pipes used
dir=${1:-"$(cd "$(dirname "$0")/.." && pwd)/prototypes/admin/src"}
[ -d "$dir" ] || { echo "check-admin-no-demo: $dir not found"; exit 1; }
hits=$( { find "$dir" -name 'mock.js' -o -name 'mock.jsx' -o -name 'mock.ts'; grep -rnE "from ['\"][./]*mock(\.jsx?)?['\"]|DemoBadge|演示数据|[A-Za-z]+Demo\(\)" "$dir" --include='*.js' --include='*.jsx' --include='*.ts' --include='*.tsx'; } 2>/dev/null)
# 人工调账不得硬编码金额（旧版 Confirm 行写死 '+50 USDT' 之类）；调账只能走 adjustments 接口
adj=$(grep -rnE "\[['\"](金额|调账金额|数量)['\"], *['\"][+-]?[0-9]+(\.[0-9]+)? *[A-Z]{2,6}['\"]\]|调账[^'\"]*['\"][+-][0-9]+ *USDT" "$dir" --include='*.js' --include='*.jsx' --include='*.ts' --include='*.tsx' 2>/dev/null)
if [ -n "$adj" ]; then hits="$hits
$adj"; fi
if grep -rlq "人工调账" "$dir" --include='*.jsx' 2>/dev/null && ! grep -rqE "api\(['\"]adjustments" "$dir" --include='*.jsx' 2>/dev/null; then hits="$hits
人工调账 UI present but never calls the adjustments API"; fi
hits=$(printf '%s' "$hits" | sed '/^$/d')
if [ -n "$hits" ]; then echo "admin demo/mock references found:"; echo "$hits"; exit 1; fi
echo "check-admin-no-demo: clean"
