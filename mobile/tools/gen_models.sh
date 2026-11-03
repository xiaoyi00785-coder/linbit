#!/usr/bin/env bash
# 由 openapi.yaml（docs/mobile-api 分支）生成 Swift 模型，并修正 swift5 生成器的已知问题。
# 依赖：Java 17，openapi-generator-cli 7.10.0 jar（OAG_JAR）。
set -euo pipefail
cd "$(dirname "$0")/.."
OAG_JAR=${OAG_JAR:-/tmp/oag.jar}
SPEC=${1:-openapi.yaml}
G=ios/Linbit/Core/Generated
TMP=$(mktemp -d)
java -jar "$OAG_JAR" generate -i "$SPEC" -g swift5 -o "$TMP/m" --skip-validate-spec \
  --global-property models,modelDocs=false,modelTests=false --additional-properties projectName=LinbitAPI,modelNamePrefix=API >/dev/null
java -jar "$OAG_JAR" generate -i "$SPEC" -g swift5 -o "$TMP/s" --skip-validate-spec \
  --global-property supportingFiles --additional-properties projectName=LinbitAPI,modelNamePrefix=API >/dev/null
find "$G" -name 'API*.swift' -delete
cp "$TMP"/m/LinbitAPI/Classes/OpenAPIs/Models/*.swift "$G"/
for f in JSONDataEncoding Configuration Extensions Models CodableHelper OpenISO8601DateFormatter JSONEncodingHelper APIHelper Validation; do
  cp "$TMP/s/LinbitAPI/Classes/OpenAPIs/$f.swift" "$G/_$f.swift"; done
# 修正：去掉 AnyCodable 依赖（用 _AnyCodable.swift 代替）、[Dictionary]、oneOf 数组 case 名
sed -i '/^import AnyCodable/d;/^#if canImport(AnyCodable)/,/^#endif/d' "$G"/*.swift
sed -i 's/\[Dictionary\]/[[String: AnyCodable]]/g' "$G"/*.swift
sed -i -E 's/\.?type\[(API[A-Za-z0-9]+)\]/\0/; s/case type\[(API[A-Za-z0-9]+)\]/case typeArrayOf\1/; s/\.type\[(API[A-Za-z0-9]+)\]/.typeArrayOf\1/g' "$G"/*.swift
rm -rf "$TMP"
echo "生成完成：$(ls "$G" | wc -l) 个文件"
