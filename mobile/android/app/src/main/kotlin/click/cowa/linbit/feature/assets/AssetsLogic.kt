package click.cowa.linbit.feature.assets

import click.cowa.linbit.core.generated.ApiBalance
import click.cowa.linbit.core.generated.ApiWalletNetwork
import click.cowa.linbit.core.generated.ApiWithdrawNetwork
import click.cowa.linbit.core.util.Dec
import java.math.BigDecimal

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

    fun receiveAmount(amount: String?, gasFee: String?): String? {
        val a = Dec.parseOrNull(amount) ?: return null
        val fee = Dec.parse(gasFee)
        val r = a.subtract(fee)
        if (r.signum() < 0) return null
        return r.stripTrailingZeros().toPlainString()
    }

    fun newClientId(prefix: String = "and"): String =
        "$prefix-${java.util.UUID.randomUUID()}"
}
