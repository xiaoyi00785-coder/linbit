package click.cowa.linbit.feature.assets

import click.cowa.linbit.core.generated.ApiAssetsOverview
import click.cowa.linbit.core.generated.ApiBalance
import click.cowa.linbit.core.generated.ApiWalletNetwork
import click.cowa.linbit.core.generated.ApiWithdrawNetwork
import click.cowa.linbit.core.util.Dec
import java.math.BigDecimal
import java.time.Instant

/** 资产/充提纯逻辑（无 Android 依赖，供单测）。 */
object AssetsLogic {
    private val networkNames = mapOf(
        "TRON" to "Tron (TRC20)",
        "TRC20" to "Tron (TRC20)",
        "ETH" to "Ethereum (ERC20)",
        "ERC20" to "Ethereum (ERC20)",
        "BSC" to "BNB Smart Chain (BEP20)",
        "BEP20" to "BNB Smart Chain (BEP20)",
        "ARBITRUM" to "Arbitrum One",
        "SOL" to "Solana",
        "BTC" to "Bitcoin",
    )

    fun displayNetwork(code: String?): String {
        val n = code?.trim().orEmpty()
        if (n.isEmpty()) return "--"
        return networkNames[n.uppercase()] ?: n
    }

    /** 仅当 min > 0 时返回去尾零的展示串，否则 null（不显示最小额）。 */
    fun positiveAmount(raw: String?): String? {
        val d = Dec.parseOrNull(raw) ?: return null
        if (d.signum() <= 0) return null
        return d.stripTrailingZeros().toPlainString()
    }

    fun etaText(minutes: Int?): String =
        minutes?.takeIf { it > 0 }?.let { "预计到账时间 ≈ $it 分钟" } ?: "预计到账时间 --"

    fun etaShort(minutes: Int?): String =
        minutes?.takeIf { it > 0 }?.let { "≈ $it 分钟" } ?: "--"

    /**
     * 充值网络默认：服务端已按 asset → sortOrder → network 排好序；
     * 取第一个 recommended==true 且可充值的网络；否则仅一条时取该条；否则 null（弹选择器）。
     */
    fun defaultDepositNetwork(nets: List<ApiWalletNetwork>): ApiWalletNetwork? {
        val enabled = nets.filter { it.depositEnabled != false && !it.network.isNullOrBlank() }
        enabled.firstOrNull { it.recommended == true }?.let { return it }
        return enabled.singleOrNull()
    }

    fun depositNetworks(asset: String, nets: List<ApiWalletNetwork>): List<ApiWalletNetwork> =
        nets.filter { n ->
            !n.network.isNullOrBlank() &&
                n.depositEnabled != false &&
                (n.asset.isNullOrBlank() || n.asset.equals(asset, ignoreCase = true))
        }

    fun withdrawEnabledNetworks(nets: List<ApiWalletNetwork>): List<ApiWalletNetwork> =
        nets.filter { it.withdrawEnabled != false && !it.network.isNullOrBlank() }

    @JvmName("filterWithdrawNetworkOptions")
    fun withdrawEnabledNetworks(nets: List<ApiWithdrawNetwork>): List<ApiWithdrawNetwork> =
        nets.filter { it.withdrawEnabled }

    fun withdrawMemoRequired(primary: ApiWithdrawNetwork?, wallet: ApiWalletNetwork?): Boolean =
        primary?.memoRequired == true || wallet?.memoRequired == true

    fun usd(b: ApiBalance, lastPrice: (String) -> BigDecimal): BigDecimal {
        b.valuationUsd?.let { return Dec.parse(it) }
        if (b.asset.equals("USDT", true)) return Dec.parse(b.total)
        return Dec.parse(b.total).multiply(lastPrice(b.asset + "USDT"))
    }

    fun filterBalances(
        list: List<ApiBalance>,
        query: String,
        hideSmall: Boolean,
        usdOf: (ApiBalance) -> BigDecimal,
    ): List<ApiBalance> {
        val q = query.trim()
        return list
            .filter { (q.isEmpty() || it.asset.contains(q, true)) && (!hideSmall || usdOf(it) >= BigDecimal.ONE) }
            .sortedWith(compareByDescending<ApiBalance> { usdOf(it) }.thenBy { it.asset })
    }

    /**
     * Gas 是否从提现数量中扣除：只有 Gas 以提现币种计价时才扣（后端 GasDeductMode=amount）。
     * gasFeeAsset 为空时按提现币种处理（与后端默认一致）。
     */
    fun gasDeductedFromAmount(asset: String, gasFeeAsset: String?): Boolean =
        gasFeeAsset.isNullOrBlank() || gasFeeAsset.equals(asset, ignoreCase = true)

    /** 到账数量 = 数量 − 同币种 Gas；Gas 以其他币种计价时到账数量等于提现数量。 */
    fun receiveAmount(amount: String?, gasFee: String?, asset: String? = null, gasFeeAsset: String? = null): String? {
        val a = Dec.parseOrNull(amount) ?: return null
        val deduct = asset == null || gasDeductedFromAmount(asset, gasFeeAsset)
        val fee = if (deduct) Dec.parse(gasFee) else BigDecimal.ZERO
        val r = a.subtract(fee)
        if (r.signum() < 0) return null
        return r.stripTrailingZeros().toPlainString()
    }

    /**
     * 提现数量校验，返回 null 表示通过（数量为空时不提示）。
     * 只有 Gas 以提现币种计价时，才要求数量大于 Gas。
     */
    fun withdrawAmountError(amount: String, asset: String, available: BigDecimal?, net: ApiWithdrawNetwork?): String? {
        if (amount.isBlank()) return null
        val value = Dec.parse(amount)
        if (value.signum() <= 0) return "请输入提现数量"
        if (available != null && value > available) return "可用余额不足"
        if (net == null) return null
        positiveAmount(net.minWithdraw)?.let { if (value < Dec.parse(it)) return "最小提现数量 $it $asset" }
        positiveAmount(net.maxWithdraw)?.let { if (value > Dec.parse(it)) return "单笔最大提现 $it $asset" }
        if (gasDeductedFromAmount(asset, net.gasFeeAsset)) {
            positiveAmount(net.gasFeeEstimate)?.let { if (value <= Dec.parse(it)) return "提现数量需大于链上网络费（Gas） $it $asset" }
        }
        return null
    }

    /**
     * 提现扣款账户中该币种的可用余额。account 为 /assets/overview 的 withdrawAccount（缺省 funding）；
     * 找不到账户或币种时返回 0。
     */
    fun withdrawAvailable(overview: ApiAssetsOverview?, asset: String): BigDecimal? {
        overview ?: return null
        val acct = overview.withdrawAccount?.value ?: "funding"
        val row = overview.accounts.orEmpty().firstOrNull { it.account?.value == acct }
            ?.balances.orEmpty().firstOrNull { it.asset.equals(asset, ignoreCase = true) }
        return Dec.parse(row?.free)
    }

    /** 提现保护期截止时间（ISO-8601）；已过期或无法解析时返回 null。 */
    fun activeWithdrawLock(lockedUntil: String?, now: Instant = Instant.now()): Instant? {
        val t = lockedUntil?.takeIf { it.isNotBlank() }?.let { runCatching { Instant.parse(it) }.getOrNull() } ?: return null
        return t.takeIf { it.isAfter(now) }
    }

    fun newClientId(prefix: String = "and"): String =
        "$prefix-${java.util.UUID.randomUUID()}"
}
