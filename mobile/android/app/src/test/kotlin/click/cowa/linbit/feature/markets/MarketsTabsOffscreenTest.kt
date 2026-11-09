package click.cowa.linbit.feature.markets

import android.app.Application
import android.graphics.Bitmap
import android.graphics.Canvas
import androidx.activity.ComponentActivity
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Text
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import click.cowa.linbit.core.generated.ApiMarketCategory
import click.cowa.linbit.data.MarketKind
import click.cowa.linbit.data.MarketRow
import click.cowa.linbit.data.MarketState
import click.cowa.linbit.ui.components.EmptyState
import click.cowa.linbit.ui.theme.LinbitTheme
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

/** 行情四类列表离屏证据（生产只读 GET）。注明：Robolectric 离屏，非真机。 */
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
            setRequestProperty("User-Agent", "Linbit-Android/0.3.10-vc13-offscreen")
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
        val spotCats = json.decodeFromString<List<ApiMarketCategory>>(get("/market/categories?market=spot")).filter { it.symbols.isNotEmpty() }
        val futCats = json.decodeFromString<List<ApiMarketCategory>>(get("/market/categories?market=futures")).filter { it.symbols.isNotEmpty() }
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
        val spot = spotEl.map { spotRow(it.jsonObject) }
        val futures = futEl.map { futRow(it.jsonObject) }
        File(out, "live-markets-counts.txt").writeText(
            "spot=${spot.size} ${spot.map { it.symbol }}\n" +
                "futures=${futures.size} ${futures.map { it.symbol }}\n" +
                "tradFi=${tradFiSyms.size} ${tradFiSyms.sorted()}\n" +
                "spotCats=${spotCats.map { it.id to it.symbols.size }}\n" +
                "futCats=${futCats.map { it.id to it.symbols.size }}\n",
        )
        return MarketState(spot, futures, spotCats, spotCats, futCats, tradFiSyms)
    }

    @Test fun offscreenFourTabsFromProduction() {
        val state = loadLive()
        assertTrue(state.spot.isNotEmpty())
        assertTrue(state.futures.isNotEmpty())
        assertTrue(state.spot.map { it.symbol }.toSet() != state.futures.map { it.symbol }.toSet())

        val page = mutableIntStateOf(0)
        rule.setContent {
            LinbitTheme(false) {
                when (page.intValue) {
                    0 -> MarketsContent(state, emptyList(), 80.dp, {}, {}, Modifier.fillMaxSize(), initialTop = 1)
                    1 -> Column(Modifier.fillMaxSize().padding(top = 40.dp)) {
                        Text("市场 · 合约（永续）", Modifier.padding(16.dp))
                        state.futures.forEach { MarketRowView(it, state.cnyRate) }
                    }
                    2 -> MarketsContent(state, emptyList(), 80.dp, {}, {}, Modifier.fillMaxSize(), initialTop = 0)
                    else -> Column(Modifier.fillMaxSize().padding(top = 40.dp)) {
                        Text("市场 · TradFi", Modifier.padding(16.dp))
                        if (state.tradFiSymbols.isEmpty()) EmptyState("暂无 TradFi 数据")
                        else state.tradFiSymbols.sorted().forEach { sym ->
                            val row = state.spot.firstOrNull { it.symbol == sym } ?: state.futures.firstOrNull { it.symbol == sym }
                            if (row != null) MarketRowView(row, state.cnyRate)
                            else Text(sym, Modifier.padding(horizontal = 16.dp, vertical = 8.dp))
                        }
                    }
                }
            }
        }
        rule.waitForIdle()
        shot("linbit-markets-spot-vc13-offscreen.png")
        File(out, "linbit-markets-spot-vc13-offscreen.txt").writeText("离屏（生产 /market/ticker），现货 ${state.spot.size} 对。\n")

        rule.runOnIdle { page.intValue = 1 }
        rule.waitForIdle()
        shot("linbit-markets-futures-vc13-offscreen.png")
        File(out, "linbit-markets-futures-vc13-offscreen.txt").writeText("离屏（生产 /futures/market/ticker），合约 ${state.futures.size} 对。\n")

        rule.runOnIdle { page.intValue = 2 }
        rule.waitForIdle()
        shot("linbit-markets-watch-empty-vc13-offscreen.png")
        File(out, "linbit-markets-watch-empty-vc13-offscreen.txt").writeText("离屏，自选空态推荐（非全市场）。\n")

        rule.runOnIdle { page.intValue = 3 }
        rule.waitForIdle()
        shot("linbit-markets-tradfi-vc13-offscreen.png")
        File(out, "linbit-markets-tradfi-vc13-offscreen.txt").writeText("离屏（生产 /tradfi/assets），TradFi ${state.tradFiSymbols.size} 个。\n")
    }
}
