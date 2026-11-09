package click.cowa.linbit.feature.trade

import click.cowa.linbit.core.generated.ApiEarnProduct
import click.cowa.linbit.core.util.Dec
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import java.util.TimeZone

/** 申购是否开放：按 status / 额度 / 可选起止时间区分，避免「已结束」却提示未来开始时间。 */
enum class EarnSubscribePhase {
    OPEN,
    NOT_STARTED,
    ENDED,
    SOLD_OUT,
}

object EarnProductAvailability {
    fun phase(
        product: ApiEarnProduct,
        nowMs: Long = System.currentTimeMillis(),
        startAtMs: Long? = null,
        endAtMs: Long? = null,
    ): EarnSubscribePhase {
        when (product.status?.trim()?.lowercase()) {
            "offline", "ended", "closed", "expired", "finished" -> return EarnSubscribePhase.ENDED
            "upcoming", "scheduled", "pending", "not_started" -> return EarnSubscribePhase.NOT_STARTED
        }
        if (startAtMs != null && nowMs < startAtMs) return EarnSubscribePhase.NOT_STARTED
        if (endAtMs != null && nowMs >= endAtMs) return EarnSubscribePhase.ENDED
        if (isSoldOut(product)) return EarnSubscribePhase.SOLD_OUT
        return EarnSubscribePhase.OPEN
    }

    fun isSoldOut(product: ApiEarnProduct): Boolean {
        if (product.remaining != null && Dec.parse(product.remaining).signum() <= 0) return true
        val quota = Dec.parse(product.totalQuota)
        if (quota.signum() > 0 && Dec.parse(product.sold) >= quota) return true
        return false
    }

    /** 置灰「立即申购」被点时的 toast；开放时返回 null。 */
    fun blockedToast(
        product: ApiEarnProduct,
        nowMs: Long = System.currentTimeMillis(),
        startAtMs: Long? = null,
        endAtMs: Long? = null,
    ): String? = when (phase(product, nowMs, startAtMs, endAtMs)) {
        EarnSubscribePhase.OPEN -> null
        EarnSubscribePhase.NOT_STARTED ->
            if (startAtMs != null) "当前产品在${formatLocal(startAtMs)} 开始申购" else "申购尚未开始"
        EarnSubscribePhase.ENDED -> "申购已结束"
        EarnSubscribePhase.SOLD_OUT -> "已售罄"
    }

    fun ctaLabel(
        product: ApiEarnProduct,
        nowMs: Long = System.currentTimeMillis(),
        startAtMs: Long? = null,
        endAtMs: Long? = null,
    ): String = when (phase(product, nowMs, startAtMs, endAtMs)) {
        EarnSubscribePhase.OPEN -> "立即申购"
        EarnSubscribePhase.NOT_STARTED -> "未开始"
        EarnSubscribePhase.ENDED -> "已结束"
        EarnSubscribePhase.SOLD_OUT -> "已售罄"
    }

    fun timelineLabel(
        product: ApiEarnProduct,
        nowMs: Long = System.currentTimeMillis(),
        startAtMs: Long? = null,
        endAtMs: Long? = null,
    ): String = when (phase(product, nowMs, startAtMs, endAtMs)) {
        EarnSubscribePhase.OPEN -> "申购中"
        EarnSubscribePhase.NOT_STARTED -> "未开始"
        EarnSubscribePhase.ENDED -> "已结束"
        EarnSubscribePhase.SOLD_OUT -> "已售罄"
    }

    private fun formatLocal(ms: Long): String {
        val fmt = SimpleDateFormat("yyyy-MM-dd HH:mm:ss", Locale.CHINA)
        fmt.timeZone = TimeZone.getDefault()
        return fmt.format(Date(ms))
    }
}
