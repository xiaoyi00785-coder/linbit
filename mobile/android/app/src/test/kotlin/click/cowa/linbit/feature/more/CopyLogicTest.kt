package click.cowa.linbit.feature.more

import click.cowa.linbit.core.generated.ApiCopyFollowRequest
import org.junit.Assert.*
import org.junit.Test
import java.math.BigDecimal

class CopyLogicTest {
    private val base = CopyLogic.Input("100", false, "1", "50")

    @Test fun `最低金额使用服务端返回值`() {
        assertNotNull(CopyLogic.error(base.copy(amount = "49.99"), BigDecimal(50)))
        assertNull(CopyLogic.error(base.copy(amount = "50"), BigDecimal(50)))
    }

    @Test fun `比例和止损范围校验`() {
        assertNotNull(CopyLogic.error(base.copy(ratio = "0"), BigDecimal(50)))
        assertNotNull(CopyLogic.error(base.copy(ratio = "10.01"), BigDecimal(50)))
        assertNotNull(CopyLogic.error(base.copy(stopLoss = "101"), BigDecimal(50)))
        assertNull(CopyLogic.error(base.copy(takeProfit = "150", stopLoss = "100"), BigDecimal(50)))
    }

    @Test fun `固定保证金不携带比例字段`() {
        val request = CopyLogic.request("T1", base.copy(fixed = true, fixedMargin = "25"))
        assertEquals(ApiCopyFollowRequest.Mode.FIXED, request.mode)
        assertEquals("25", request.fixedMargin)
        assertNull(request.ratio)
    }

    @Test fun `止盈止损百分数转服务端小数`() {
        val request = CopyLogic.request("T1", base.copy(takeProfit = "12.5", stopLoss = "20"))
        assertEquals("0.125", request.tpRatio)
        assertEquals("0.2", request.slRatio)
    }
}
