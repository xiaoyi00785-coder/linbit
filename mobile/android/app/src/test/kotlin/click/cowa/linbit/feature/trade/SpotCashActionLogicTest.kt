package click.cowa.linbit.feature.trade

import click.cowa.linbit.core.generated.ApiConvertPairsPairsInner
import click.cowa.linbit.core.generated.ApiConvertQuote
import click.cowa.linbit.core.generated.ApiGridBot
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class StrategyGridLogicTest {
    @Test fun quoteAssetFromKnownSuffix() {
        assertEquals("USDT", StrategyGridLogic.quoteAssetFromSymbol("BTCUSDT"))
        assertEquals("USDC", StrategyGridLogic.quoteAssetFromSymbol("ethusdc"))
        assertEquals("BTC", StrategyGridLogic.quoteAssetFromSymbol("ETHBTC"))
        assertNull(StrategyGridLogic.quoteAssetFromSymbol("UNKNOWN"))
        assertNull(StrategyGridLogic.quoteAssetFromSymbol(null))
    }

    @Test fun stopOnlyWhenRunning() {
        assertTrue(StrategyGridLogic.canStop(ApiGridBot.Status.RUNNING))
        assertFalse(StrategyGridLogic.canStop(ApiGridBot.Status.STOPPING))
        assertFalse(StrategyGridLogic.canStop(ApiGridBot.Status.STOPPED))
        assertFalse(StrategyGridLogic.canStop(null))
        assertTrue(StrategyGridLogic.isStopping(ApiGridBot.Status.STOPPING))
    }

    @Test fun fullGridFormIsValidatedAndPreviewBoundToEveryInput() {
        val config = click.cowa.linbit.core.generated.ApiStrategyConfig200Response(minGrids = 4, maxGrids = 20, minInvestment = "10")
        val form = StrategyGridLogic.GridForm("BTCUSDT", click.cowa.linbit.core.generated.ApiGridCreateRequest.Mode.ARITHMETIC, "60000", "70000", 10, "100", true)
        val preview = StrategyGridLogic.GridPreview("BTCUSDT", form.mode, "65000", listOf("60000", "70000"), "0.001", 5, 5, "65", "0.1", "0.2", form)
        assertNull(StrategyGridLogic.validateForm(form, config))
        assertTrue(StrategyGridLogic.previewMatches(preview, form))
        assertFalse(StrategyGridLogic.previewMatches(preview, form.copy(sellBaseOnStop = false)))
        assertFalse(StrategyGridLogic.previewMatches(preview, form.copy(investment = "101")))
        assertFalse(StrategyGridLogic.previewMatches(preview, form.copy(grids = 11)))
        assertFalse(StrategyGridLogic.previewMatches(preview, form.copy(mode = click.cowa.linbit.core.generated.ApiGridCreateRequest.Mode.GEOMETRIC)))
        assertTrue(StrategyGridLogic.validateForm(form.copy(lower = "70000", upper = "60000"), config)!!.contains("上限"))
        assertTrue(StrategyGridLogic.validateForm(form.copy(grids = 3), config)!!.contains("网格数"))
        assertTrue(StrategyGridLogic.validateForm(form.copy(grids = 21), config)!!.contains("网格数"))
        assertTrue(StrategyGridLogic.validateForm(form.copy(investment = ""), config)!!.contains("投资"))
    }

    @Test fun investmentMinAndPositive() {
        assertEquals("请输入投资额", StrategyGridLogic.validateInvestment("", null))
        assertEquals("请输入投资额", StrategyGridLogic.validateInvestment("0", "10"))
        assertEquals("不低于最小投资额 100", StrategyGridLogic.validateInvestment("50", "100"))
        assertNull(StrategyGridLogic.validateInvestment("100", "100"))
        assertNull(StrategyGridLogic.validateInvestment("200", null))
    }
}

class ConvertQuoteLogicTest {
    private fun q(
        from: String = "USDT",
        to: String = "BTC",
        fromAmount: String = "100",
        quoteId: String? = "q1",
        expiresAt: Long? = System.currentTimeMillis() + 10_000,
    ) = ApiConvertQuote(
        quoteId = quoteId,
        from = from,
        to = to,
        fromAmount = fromAmount,
        toAmount = "0.001",
        rate = "0.00001",
        expiresAt = expiresAt,
    )

    @Test fun clearQuoteWhenFormDiverges() {
        val form = ConvertQuoteLogic.Form("USDT", "BTC", "100")
        assertTrue(ConvertQuoteLogic.matchesForm(q(), form))
        assertFalse(ConvertQuoteLogic.matchesForm(q(fromAmount = "50"), form))
        assertFalse(ConvertQuoteLogic.matchesForm(q(to = "ETH"), form))
        assertFalse(ConvertQuoteLogic.canAccept(q(fromAmount = "50"), form))
    }

    @Test fun expiredBlocksAccept() {
        val form = ConvertQuoteLogic.Form("USDT", "BTC", "100")
        val expired = q(expiresAt = System.currentTimeMillis() - 1)
        assertTrue(ConvertQuoteLogic.isExpired(expired))
        assertFalse(ConvertQuoteLogic.canAccept(expired, form))
    }

    @Test fun staleRevisionMustNotApply() {
        val form = ConvertQuoteLogic.Form("USDT", "BTC", "100")
        assertFalse(ConvertQuoteLogic.shouldApplyQuote(q(), requestRev = 1, currentRev = 2, form = form))
        assertTrue(ConvertQuoteLogic.shouldApplyQuote(q(), requestRev = 3, currentRev = 3, form = form))
        assertFalse(
            ConvertQuoteLogic.shouldApplyQuote(
                q(fromAmount = "100"),
                requestRev = 3,
                currentRev = 3,
                form = ConvertQuoteLogic.Form("USDT", "BTC", "200"),
            ),
        )
    }

    @Test fun errorCodesClearQuote() {
        assertTrue(ConvertQuoteLogic.shouldClearQuoteOnError("QUOTE_EXPIRED"))
        assertTrue(ConvertQuoteLogic.shouldClearQuoteOnError("QUOTE_NOT_FOUND"))
        assertFalse(ConvertQuoteLogic.shouldClearQuoteOnError("INSUFFICIENT_BALANCE"))
        assertFalse(ConvertQuoteLogic.shouldClearQuoteOnError(null))
    }

    @Test fun pairsNotAssetsFallbackAndPause() {
        val pairs = listOf(
            ApiConvertPairsPairsInner(from = "USDT", to = "BTC"),
            ApiConvertPairsPairsInner(from = "USDT", to = "ETH"),
            ApiConvertPairsPairsInner(
                from = "USDC",
                to = "USDT",
                paused = true,
                pauseReason = ApiConvertPairsPairsInner.PauseReason.DEPEG,
            ),
        )
        assertEquals(listOf("USDC", "USDT"), ConvertQuoteLogic.fromAssets(pairs))
        assertEquals(listOf("BTC", "ETH"), ConvertQuoteLogic.toAssets(pairs, "USDT"))
        val paused = ConvertQuoteLogic.findPair(pairs, "USDC", "USDT")
        assertTrue(ConvertQuoteLogic.isPairPaused(paused))
        assertEquals("该币对因脱锚保护已暂停", ConvertQuoteLogic.pauseHint(paused))
    }

    @Test fun expiresAtSecondsNormalized() {
        val sec = 1_700_000_000L
        assertEquals(sec * 1000, ConvertQuoteLogic.normalizeExpiresAtMs(sec))
        assertEquals(1_700_000_000_000L, ConvertQuoteLogic.normalizeExpiresAtMs(1_700_000_000_000L))
    }
}
