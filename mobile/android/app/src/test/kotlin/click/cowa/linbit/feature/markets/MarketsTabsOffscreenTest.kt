package click.cowa.linbit.feature.markets

import android.app.Application
import android.graphics.Bitmap
import android.graphics.Canvas
import androidx.activity.ComponentActivity
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Text
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import click.cowa.linbit.core.generated.ApiMarketCategory
import click.cowa.linbit.data.MarketKind
import click.cowa.linbit.data.MarketRow
import click.cowa.linbit.data.MarketState
import click.cowa.linbit.ui.components.ChipBar
import click.cowa.linbit.ui.components.EmptyState
import click.cowa.linbit.ui.components.PrimaryTabs
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.LinbitTheme
import click.cowa.linbit.ui.theme.Spacing
import click.cowa.linbit.ui.theme.lb
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.io.File
import java.io.FileOutputStream
import java.math.BigDecimal
import java.net.HttpURLConnection
import java.net.URL

/** 四 tab 完整页离屏（顶栏+二级分类可见）。生产只读 GET；注明离屏非真机。 */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w390dp-h844dp-xxhdpi")
class MarketsTabsOffscreenTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val out = File("/workspace/linbit-verify/batch15").also { it.mkdirs() }
    private val json = Json { ignoreUnknownKeys = true; isLenient = true }

    private fun get(path: String): String {
        val c = (URL("https://api.cowa.click$path").openConnection() as HttpURLConnection).apply {
            requestMethod = "GET"
            setRequestProperty("User-Agent", "Linbit-Android/0.3.11-vc14-offscreen")
            connectTimeout = 15000
            readTimeout = 20000
        }
        return c.inputStream.bufferedReader().readText()
    }

    private fun shot(name: String) {
        rule.waitForIdle()
        rule.runOnIdle {
            val decor = rule.activity.window.decorView
            val bmp = Bitmap.createBitmap(decor.width.coerceAtLeast(1), decor.height.coerceAtLeast(1), Bitmap.Config.ARGB_8888)
            decor.draw(Canvas(bmp))
            FileOutputStream(File(out, name)).use { bmp.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
    }

    private fun loadLive(): MarketState {
        val spotEl = json.parseToJsonElement(get("/market/ticker")).jsonArray
        val futEl = json.parseToJsonElement(get("/futures/market/ticker")).jsonObject["tickers"]!!.jsonArray
        val spotCatsRaw = json.decodeFromString<List<ApiMarketCategory>>(get("/market/categories?market=spot"))
        val futCatsRaw = json.decodeFromString<List<ApiMarketCategory>>(get("/market/categories?market=futures"))
        val tradFiSyms = json.parseToJsonElement(get("/tradfi/assets")).jsonObject["items"]!!.jsonArray.mapNotNull {
            it.jsonObject["symbol"]?.jsonPrimitive?.content
        }.toSet()
        fun spotRow(o: kotlinx.serialization.json.JsonObject): MarketRow {
            val sym = o["symbol"]!!.jsonPrimitive.content
            return MarketRow(
                sym, sym.removeSuffix("USDT"), "USDT",
                BigDecimal(o["lastPrice"]?.jsonPrimitive?.content ?: "0"),
                BigDecimal(o["priceChangePercent"]?.jsonPrimitive?.content ?: "0"),
                BigDecimal(o["quoteVolume"]?.jsonPrimitive?.content ?: "0"),
                2, kind = MarketKind.SPOT,
            )
        }
        fun futRow(o: kotlinx.serialization.json.JsonObject): MarketRow {
            val sym = o["symbol"]!!.jsonPrimitive.content
            val last = sequenceOf("lastPrice", "markPrice", "indexPrice").mapNotNull { o[it]?.jsonPrimitive?.content }
                .firstOrNull { it.toBigDecimalOrNull()?.signum() == 1 } ?: "0"
            val ch = o["priceChangePercent"]?.jsonPrimitive?.content
                ?: o["priceChangePercent24h"]?.jsonPrimitive?.content?.toBigDecimalOrNull()?.multiply(BigDecimal(100))?.toPlainString()
                ?: "0"
            val vol = o["quoteVolume"]?.jsonPrimitive?.content ?: o["turnover24h"]?.jsonPrimitive?.content ?: "0"
            return MarketRow(sym, sym.removeSuffix("USDT"), "USDT", BigDecimal(last), BigDecimal(ch), BigDecimal(vol), null, kind = MarketKind.FUTURES)
        }
        val part = MarketsTabCatalog.partition(spotEl.map { spotRow(it.jsonObject) }, futEl.map { futRow(it.jsonObject) }, tradFiSyms)
        val spotCats = MarketsTabCatalog.sanitizeCategories(spotCatsRaw, tradFiSyms, part.spot.map { it.symbol }.toSet())
        val futCats = MarketsTabCatalog.sanitizeCategories(futCatsRaw, tradFiSyms, part.futures.map { it.symbol }.toSet())
        File(out, "live-markets-counts-vc14.txt").writeText(
            "BACKEND_NOTE: /market/ticker and /futures/market/ticker mix TradFi symbols; client excludes entire /tradfi/assets set from 现货/合约.\n" +
                "spot=${part.spot.size} ${part.spot.map { it.symbol }}\n" +
                "futures=${part.futures.size} ${part.futures.map { it.symbol }}\n" +
                "tradFi=${tradFiSyms.size} ${tradFiSyms.sorted()}\n" +
                "excludedFromSpot=${part.tradFiSpotRows.map { it.symbol }}\n" +
                "excludedFromFutures=${part.tradFiFuturesRows.map { it.symbol }}\n" +
                "tradFiLeaks=${MarketsTabCatalog.tradFiLeaksIntoSpotOrFutures(part.spot.map { it.symbol }, part.futures.map { it.symbol }, tradFiSyms)}\n" +
                "spotFuturesOverlap(allowed)=${MarketsTabCatalog.spotFuturesSymbolOverlap(part.spot.map { it.symbol }, part.futures.map { it.symbol })}\n",
        )
        return MarketState(
            spot = part.spot, futures = part.futures,
            categories = spotCats, spotCategories = spotCats, futuresCategories = futCats,
            tradFiSymbols = tradFiSyms,
            tradFiSpotRows = part.tradFiSpotRows, tradFiFuturesRows = part.tradFiFuturesRows,
        )
    }

    @Test fun offscreenFullPagesFourTabsNoOverlap() {
        val state = loadLive()
        assertTrue(state.spot.isNotEmpty())
        assertTrue(state.futures.isNotEmpty())
        assertTrue(state.tradFiSymbols.isNotEmpty())
        assertTrue(MarketsTabCatalog.tradFiLeaksIntoSpotOrFutures(state.spot.map { it.symbol }, state.futures.map { it.symbol }, state.tradFiSymbols).isEmpty())

        data class Page(val mode: Int, val file: String, val note: String)
        // mode: 0 spot MarketsContent, 1 futures MarketsContent, 2 watch MarketsContent, 3 tradfi chrome+list
        val pages = listOf(
            Page(0, "linbit-markets-spot-vc14-offscreen.png", "现货完整页；已剔除 TradFi"),
            Page(1, "linbit-markets-futures-vc14-offscreen.png", "合约完整页；已剔除 TradFi 永续"),
            Page(2, "linbit-markets-watch-empty-vc14-offscreen.png", "自选空态完整页"),
            Page(3, "linbit-markets-tradfi-vc14-offscreen.png", "TradFi 完整页（同顶栏+二级；列表来自 /tradfi/assets）"),
        )
        val page = mutableIntStateOf(0)
        rule.setContent {
            LinbitTheme(false) {
                val p = LB.p
                key(page.intValue) {
                    when (pages[page.intValue].mode) {
                        0 -> MarketsContent(state, emptyList(), 80.dp, {}, {}, Modifier.fillMaxSize(), initialTop = 1, initialKind = 0)
                        1 -> MarketsContent(state, emptyList(), 80.dp, {}, {}, Modifier.fillMaxSize(), initialTop = 1, initialKind = 1)
                        2 -> MarketsContent(state, emptyList(), 80.dp, {}, {}, Modifier.fillMaxSize(), initialTop = 0, initialKind = 0)
                        else -> Column(Modifier.fillMaxSize().background(p.bg)) {
                            Row(
                                Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(vertical = 10.dp),
                                verticalAlignment = Alignment.CenterVertically,
                            ) {
                                PrimaryTabs(listOf("自选", "市场", "聊天室"), 1, {})
                            }
                            HorizontalDivider(color = p.divider)
                            Row(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(top = 10.dp)) {
                                PrimaryTabs(listOf("现货", "合约", "TradFi"), 2, {}, size = FontSize.tabSecondary, underline = true)
                            }
                            HorizontalDivider(color = p.divider)
                            ChipBar(listOf("全部", "股票", "贵金属", "商品"), 0, {}, Modifier.padding(vertical = 10.dp))
                            val rows = (state.tradFiSpotRows + state.tradFiFuturesRows).sortedBy { it.symbol }
                            if (rows.isEmpty()) EmptyState("暂无 TradFi 数据")
                            else Column(Modifier.verticalScroll(rememberScrollState()).padding(bottom = 80.dp)) {
                                Text("资产", Modifier.padding(horizontal = Spacing.pageH, vertical = 8.dp), style = lb(FontSize.small), color = p.textTertiary)
                                rows.forEach { MarketRowView(it, state.cnyRate) }
                            }
                        }
                    }
                }
            }
        }
        pages.forEachIndexed { i, cur ->
            rule.runOnIdle { page.intValue = i }
            rule.waitForIdle()
            Thread.sleep(250)
            rule.waitForIdle()
            shot(cur.file)
            File(out, cur.file.removeSuffix(".png") + ".txt").writeText(
                "离屏完整页（Robolectric+生产只读 GET），非真机。${cur.note}\n",
            )
        }
    }
}
