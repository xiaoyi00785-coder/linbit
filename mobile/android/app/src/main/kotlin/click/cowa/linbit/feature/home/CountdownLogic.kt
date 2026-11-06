package click.cowa.linbit.feature.home

import java.time.Duration
import java.time.Instant

/**
 * 倒计时纯逻辑：必须由真实时间字段驱动。
 * - 新人任务：HomeNewcomer.deadline（OpenAPI date-time）
 * - 资金费率：FuturesTicker.nextFundingTime（int64，秒或毫秒）
 * 字段缺失时返回 null / 占位，不编造截止时间。
 */
object CountdownLogic {
    fun parseDeadlineInstant(deadline: String?): Instant? {
        val raw = deadline?.trim()?.takeIf { it.isNotEmpty() } ?: return null
        runCatching { Instant.parse(raw) }.getOrNull()?.let { return it }
        raw.toLongOrNull()?.let { n ->
            val ms = if (n < 1_000_000_000_000L) n * 1000 else n
            return Instant.ofEpochMilli(ms)
        }
        // 常见后端变体：去掉 Z 的本地伪 ISO
        runCatching { Instant.parse(raw + "Z") }.getOrNull()?.let { return it }
        return null
    }

    /** 格式 6D:23:46:26；无 deadline 或无法解析 → null（UI 不显示）。 */
    fun newcomerText(deadline: String?, now: Instant = Instant.now()): String? {
        val d = parseDeadlineInstant(deadline) ?: return null
        val dur = Duration.between(now, d).coerceAtLeast(Duration.ZERO)
        return "%dD:%02d:%02d:%02d".format(dur.toDays(), dur.toHours() % 24, dur.toMinutes() % 60, dur.seconds % 60)
    }

    /** 将 nextFundingTime 规范为 epoch millis；无法识别则 null。 */
    fun fundingTimeMs(nextFundingTime: Long?): Long? {
        val t = nextFundingTime ?: return null
        if (t <= 0L) return null
        return if (t < 1_000_000_000_000L) t * 1000 else t
    }

    /** HH:MM:SS；无 nextFundingTime → null（UI 可用 --:--:-- 占位，不假装有倒计时）。 */
    fun fundingText(nextFundingTime: Long?, nowMs: Long = System.currentTimeMillis()): String? {
        val exp = fundingTimeMs(nextFundingTime) ?: return null
        val s = maxOf(0L, (exp - nowMs) / 1000)
        return "%02d:%02d:%02d".format(s / 3600, (s % 3600) / 60, s % 60)
    }
}
