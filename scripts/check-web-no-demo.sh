#!/bin/sh
# 主网规则：web / partner 前端只用真实接口，不得残留任何演示 / Mock 回退。
# 检查 apps/web、apps/partner 源码与 packages/chart：tryApi、mockFlag/MOCK_ENABLED、mock 数据导入、
# source "mock"/"demo"、useRest/useApi/rget 的演示回退参数、DemoToggle、“演示数据”字样、模拟 K 线等。
# 用法: scripts/check-web-no-demo.sh [仓库根目录]，命中即退出码 1。
set -u  # POSIX sh (dash in node:*-slim): no pipefail, no pipes used
root=${1:-"$(cd "$(dirname "$0")/.." && pwd)"}
dirs=""
for d in apps/web/app apps/web/components apps/web/features apps/web/legacy apps/web/lib apps/partner/app apps/partner/lib packages/chart/src; do
  [ -d "$root/$d" ] || { echo "check-web-no-demo: $root/$d not found"; exit 1; }
  dirs="$dirs $root/$d"
done
inc="--include=*.ts --include=*.tsx --include=*.js --include=*.jsx --include=*.mjs --exclude=i18n*"
# shellcheck disable=SC2086
hits=$( {
  find $dirs \( -name 'mock*' -o -name 'mockFlag.*' -o -name 'demo.ts' -o -name 'demo.tsx' \) -not -path '*/node_modules/*'
  grep -rnE "\btryApi\b|MOCK_ENABLED|mockFlag|NEXT_PUBLIC_ENABLE_MOCK|NEXT_PUBLIC_DEMO|from ['\"][^'\"]*/(demo|mock[A-Za-z]*)['\"]|source: *['\"](mock|demo)['\"]|source *[!=]== *['\"](mock|demo)['\"]|['\"]api['\"] *\| *['\"](mock|demo)['\"]|\b(useRest|useApi|rget|rest)\([^;]*, *\(\) *=>|DemoToggle|演示数据|演示回退|回退演示|MOCK_TOKENS|mock-access-token|wd_mock|\bmock[A-Z][A-Za-z]* *[=(]|\bconst DEMO\b|\bconst demo[A-Za-z]* *= *\(\)|\.demo\b|\bcandles\(|Math\.random" $dirs $inc --exclude-dir=node_modules --exclude-dir=.next --exclude-dir=i18n --exclude='i18n*'
} 2>/dev/null )
# Math.random 只在图表包与币种元数据里视为伪造行情；其他位置（如请求 id）放行
filtered=""
nl='
'
IFS_OLD=$IFS; IFS=$nl
for h in $hits; do
  case "$h" in
    *Math.random*) case "$h" in "$root/packages/chart/"*|"$root/apps/web/legacy/data."*) ;; *) continue ;; esac ;;
  esac
  filtered="$filtered$h$nl"
done
IFS=$IFS_OLD
if [ -n "$filtered" ]; then printf 'web/partner demo/mock references found:\n%s' "$filtered"; exit 1; fi
echo "check-web-no-demo: clean"
