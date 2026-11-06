package click.cowa.linbit.ui.evidence

import click.cowa.linbit.feature.trade.SpotSubpagesViewModel
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers

internal fun earnRoutes(api: MockEvidenceApi, populated: Boolean = true) {
    api.route("GET", "/earn/products", if (populated) """{"items":[{"id":"evidence-earn","kind":"flexible","asset":"USDT","title":"USDT 活期理财","apr":"0.052","minAmount":"10","perUserMax":"5000","status":"ONLINE"}]}""" else """{"items":[]}""")
    api.route("GET", "/earn/summary", """{"totalValueUsdt":"1250","totalInterestUsdt":"2.5"}""")
    api.route("GET", "/earn/positions", if (populated) """{"items":[{"productId":"evidence-earn","kind":"flexible","asset":"USDT","title":"USDT 活期理财","amount":"1250","totalInterest":"2.5"}]}""" else """{"items":[]}""")
    api.route("GET", "/earn/orders", """{"items":[]}"""); api.route("GET", "/earn/records", """{"items":[]}""")
}
internal fun strategyRoutes(api: MockEvidenceApi, populated: Boolean = true) {
    api.route("GET", "/strategy/config", """{"minGrids":2,"maxGrids":100,"minInvestment":"10","sellBaseOnStop":false}""")
    api.route("GET", "/strategy/grid/square?page=1&pageSize=20", if (populated) """{"items":[{"id":"evidence-copy","symbol":"BTCUSDT","mode":"ARITHMETIC","lowerPrice":"60000","upperPrice":"70000","gridCount":10,"profitRate":"0.01"}]}""" else """{"items":[]}""")
    api.route("GET", "/strategy/grid", if (populated) """{"items":[{"id":"evidence-grid","symbol":"BTCUSDT","mode":"ARITHMETIC","status":"running","sellBaseOnStop":false,"totalProfit":"1.2","investment":"100"}]}""" else """{"items":[]}""")
}
internal fun evidenceVm(api: MockEvidenceApi): SpotSubpagesViewModel {
    val f = ProductionEvidenceFixtures(api, CoroutineScope(Dispatchers.Main)); f.tokens.accessToken = "INVALID_LAYOUT_TOKEN"
    return f.spot()
}
