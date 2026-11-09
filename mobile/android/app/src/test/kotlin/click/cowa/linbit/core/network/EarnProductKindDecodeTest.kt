package click.cowa.linbit.core.network

import click.cowa.linbit.core.generated.ApiEarnProduct
import click.cowa.linbit.core.generated.ApiEarnProducts200Response
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/** Production GET /earn/products returns kind=simple|fixed|onchain; Android must not drop the catalogue. */
class EarnProductKindDecodeTest {
    @Test fun decodesProductionKindsWithoutThrowing() {
        val json = """
            {"items":[
              {"id":"simple-USDT","kind":"simple","asset":"USDT","title":"USDT 活期","apr":"0.01","bonusApr":"0.012","aprMax":"0.012","hot":false,"status":"online"},
              {"id":"fixed-USDT-7","kind":"fixed","asset":"USDT","title":"理财宝 | 7 天","apr":"0.088","termDays":7,"hot":true,"newcomer":true,"status":"online"},
              {"id":"chain-SOL","kind":"onchain","asset":"SOL","title":"SOL 质押","apr":"0.064","unlockDays":2,"hot":true,"status":"online"},
              {"id":"legacy-flex","kind":"flexible","asset":"USDT","title":"legacy","apr":"0.01","status":"online"}
            ]}
        """.trimIndent()
        val parsed = NetworkFactory.json.decodeFromString(ApiEarnProducts200Response.serializer(), json)
        val items = parsed.items ?: emptyList()
        assertEquals(4, items.size)
        assertEquals(ApiEarnProduct.Kind.SIMPLE, items[0].kind)
        assertEquals(ApiEarnProduct.Kind.FIXED, items[1].kind)
        assertEquals(ApiEarnProduct.Kind.ONCHAIN, items[2].kind)
        assertEquals(ApiEarnProduct.Kind.FLEXIBLE, items[3].kind)
        assertTrue(items[1].hot == true)
    }
}
