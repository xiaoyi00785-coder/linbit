package click.cowa.linbit.feature.trade

import click.cowa.linbit.core.util.Dec
import click.cowa.linbit.core.generated.ApiEarnProduct
import java.math.BigDecimal

/**
 * Earn APR 字段语义（对照 services/go/earn/savings.go + docs/api/earn.md）：
 * - `apr`：基础年化（十进制，如 0.01 = 1%）。固定产品计息用此值。
 * - `bonusApr`：活期档位利率，仅对本金中不超过 `bonusQuota` 的部分计息；**不是**在 apr 上再加一截，也**不是**整笔总 APR。
 * - `aprMax`：服务端写出的展示上限，通常 = max(apr, bonusApr)（有更高档位时）。
 *
 * 日息：yearly = min(p, bonusQuota)×bonusApr + rest×apr，再 /365。
 */
object EarnApr {
    fun base(p: ApiEarnProduct): BigDecimal = Dec.parse(p.apr)

    /** 展示用上限：优先服务端 aprMax；否则若 bonusApr>apr 则 bonus 仅作「档位上限」；绝不把 bonusApr 当成唯一 APR。 */
    fun ceiling(p: ApiEarnProduct): BigDecimal {
        val base = base(p)
        val maxField = p.aprMax?.takeIf { it.isNotBlank() }?.let { Dec.parse(it) }
        if (maxField != null && maxField > BigDecimal.ZERO) return maxField.max(base)
        val bonus = p.bonusApr?.takeIf { it.isNotBlank() }?.let { Dec.parse(it) }
        return if (bonus != null && bonus > base) bonus else base
    }

    fun hasTier(p: ApiEarnProduct): Boolean = ceiling(p).compareTo(base(p)) > 0
}
