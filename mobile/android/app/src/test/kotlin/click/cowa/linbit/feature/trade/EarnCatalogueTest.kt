package click.cowa.linbit.feature.trade

import click.cowa.linbit.core.generated.ApiEarnProduct
import click.cowa.linbit.core.network.NetworkFactory
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class EarnCatalogueTest {
    @Test fun itemsFromEvidenceFixture() {
        val raw = NetworkFactory.json.parseToJsonElement(
            """{"items":[{"id":"evidence-earn","kind":"simple","asset":"USDT","title":"USDT 活期理财","apr":"0.052","bonusApr":"0.06","aprMax":"0.06","minAmount":"10","perUserMax":"5000","status":"ONLINE","hot":true,"sort":10},{"id":"evidence-btc","kind":"fixed","asset":"BTC","title":"BTC 定期","apr":"0.087","termDays":14,"minAmount":"0.01","status":"ONLINE","hot":true,"sort":20},{"id":"evidence-sol","kind":"onchain","asset":"SOL","title":"SOL 质押","apr":"0.064","unlockDays":2,"minAmount":"0.01","status":"ONLINE","hot":false,"sort":30}]}"""
        )
        val items = EarnCatalogue.itemsFrom(raw)
        assertEquals(3, items.size)
        assertEquals(ApiEarnProduct.Kind.SIMPLE, items[0].kind)
        assertEquals("USDT", items[0].asset)
    }
}
