#!/usr/bin/env bash
# 由 mobile/openapi.yaml 生成 Kotlin（kotlinx.serialization）模型，输出到 android/app/.../core/generated。
# 对应 iOS 的 gen_models.sh；依赖：Java 17、openapi-generator-cli 7.10.0 jar（OAG_JAR，缺省时自动下载到 /tmp）。
#
# 约定（与 iOS 一致）：
#  - 所有模型加前缀 Api（ApiTicker、ApiError…），避免和 kotlin.Error / 其它类型冲突。
#  - 金额、价格、数量在 spec 里是十进制字符串，模型里保持 String，绝不转 Double；
#    需要计算/格式化时用 core/util/Dec.kt（BigDecimal）。8 位小数字符串原样显示。
#  - 生成器对 oneOf 响应（login、market/ticker、futures/market/ticker）会生成"所有字段必填"的错误合并类，
#    这里删除，由手写的 core/network/OneOfResponses.kt 按字段判别解码。
set -euo pipefail
cd "$(dirname "$0")/.."
OAG_VERSION=7.10.0
OAG_JAR=${OAG_JAR:-/tmp/openapi-generator-cli-$OAG_VERSION.jar}
SPEC=${1:-openapi.yaml}
PKG=click.cowa.linbit.core.generated
OUT=android/app/src/main/kotlin/${PKG//./\/}
if [ ! -f "$OAG_JAR" ]; then
  curl -fsSL -o "$OAG_JAR" "https://repo1.maven.org/maven2/org/openapitools/openapi-generator-cli/$OAG_VERSION/openapi-generator-cli-$OAG_VERSION.jar"
fi
TMP=$(mktemp -d)
java -jar "$OAG_JAR" generate -i "$SPEC" -g kotlin -o "$TMP" --skip-validate-spec \
  --global-property models,modelDocs=false,modelTests=false \
  --additional-properties "library=jvm-retrofit2,serializationLibrary=kotlinx_serialization,packageName=$PKG,modelPackage=$PKG,modelNamePrefix=Api,enumPropertyNaming=UPPERCASE,dateLibrary=string" >/dev/null
mkdir -p "$OUT"
find "$OUT" -name 'Api*.kt' -delete
cp "$TMP"/src/main/kotlin/${PKG//./\/}/*.kt "$OUT"/
# oneOf 包装类（见上）：Login200Response / GetTicker200Response / FuturesTicker200ResponseOneOf
rm -f "$OUT"/ApiLogin200Response.kt "$OUT"/ApiGetTicker200Response.kt "$OUT"/ApiFuturesTicker200ResponseOneOf.kt
# 生成器对 kotlinx.serialization 的已知问题修正：
#  - 自由格式 object/any 用 JsonObject / JsonElement 表示（原为 Map<String, Any> + @Contextual，无法序列化）
#  - 去掉 "additionalProperties" 产生的 `: HashMap<String, Any>()` 父类
#  - 汇率 Map<String, BigDecimal> 用 JsonPrimitive，保留服务端数字文本（不经 Double）
python3 - "$OUT" <<'PY'
import glob, re, sys
for f in glob.glob(sys.argv[1] + "/Api*.kt"):
    s = open(f, encoding="utf-8").read(); o = s
    s = s.replace(") : kotlin.collections.HashMap<String, kotlin.Any>() {", ") {")
    s = s.replace("kotlin.collections.Map<kotlin.String, java.math.BigDecimal>", "kotlin.collections.Map<kotlin.String, kotlinx.serialization.json.JsonPrimitive>")
    s = s.replace("kotlin.collections.Map<kotlin.String, kotlin.Any>", "kotlinx.serialization.json.JsonObject")
    s = re.sub(r"\bkotlin\.Any\b", "kotlinx.serialization.json.JsonElement", s)
    s = s.replace("@Contextual ", "")
    # 枚举的 encode/decode(data: Any?) 要保持 Any?（上面的全局替换会把它改成 JsonElement，导致比较无意义）
    s = s.replace("(data: kotlinx.serialization.json.JsonElement?)", "(data: kotlin.Any?)")
    # 手工处理的 oneOf 类已删除，去掉残留 import
    s = s.replace("import click.cowa.linbit.core.generated.ApiFuturesTicker200ResponseOneOf\n", "")
    if s != o: open(f, "w", encoding="utf-8").write(s)
PY
rm -rf "$TMP"
echo "生成完成：$(ls "$OUT" | wc -l) 个文件 -> $OUT"
