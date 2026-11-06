package click.cowa.linbit.feature.assets

import click.cowa.linbit.core.generated.ApiBalance
import click.cowa.linbit.core.generated.ApiWalletNetwork
import click.cowa.linbit.core.generated.ApiWithdrawNetwork
import click.cowa.linbit.core.generated.ApiWithdrawal
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

    /**
     * 链上提现数量校验（对照 OpenAPI：amount > gas、≥ min、≤ max/可用）。
     * 返回用户可见错误；null 表示通过。
     */
    fun chainAmountError(
        amount: String,
        free: BigDecimal,
        minWithdraw: String?,
        maxWithdraw: String?,
        gasFee: String?,
        asset: String,
    ): String? {
        if (amount.isBlank()) return null
        val value = Dec.parse(amount)
        if (value.signum() <= 0) return "请输入提现数量"
        if (value > free) return "可用余额不足"
        positiveAmount(minWithdraw)?.let { if (value < Dec.parse(it)) return "最小提现数量 $it $asset" }
        positiveAmount(maxWithdraw)?.let { if (value > Dec.parse(it)) return "单笔最大提现 $it $asset" }
        positiveAmount(gasFee)?.let { if (value <= Dec.parse(it)) return "提现数量需大于链上网络费（Gas） $it $asset" }
        return null
    }

    /** 二次验证：已开 2FA 用 totp；未开 2FA 用邮箱/手机码（purpose=withdraw）。 */
    fun withdrawVerifyReady(twoFactorEnabled: Boolean, totp: String, smsOrEmailCode: String): Boolean =
        if (twoFactorEnabled) totp.filter(Char::isDigit).length == 6
        else smsOrEmailCode.filter(Char::isDigit).length >= 4

    data class WithdrawAuthCodes(val totpCode: String?, val code: String?)

    fun withdrawAuthCodes(twoFactorEnabled: Boolean, totp: String, smsOrEmailCode: String): WithdrawAuthCodes {
        val t = totp.filter(Char::isDigit).take(6)
        val c = smsOrEmailCode.filter(Char::isDigit).take(8)
        return if (twoFactorEnabled) WithdrawAuthCodes(totpCode = t.takeIf { it.length == 6 }, code = null)
        else WithdrawAuthCodes(totpCode = null, code = c.takeIf { it.length >= 4 })
    }


    /** OpenAPI：仅 pending（待人工审核）可撤；processing/completed/failed/canceled 不可撤。 */
    fun canCancelWithdrawal(status: ApiWithdrawal.Status?): Boolean =
        status == ApiWithdrawal.Status.PENDING

    /** 撤销成功后用服务端回写状态更新列表（竞态：已非 pending 则本地同步）。 */
    fun applyWithdrawalUpdate(list: List<ApiWithdrawal>, updated: ApiWithdrawal): List<ApiWithdrawal> =
        list.map { if (it.id == updated.id) updated else it }


    /** 提现记录时间展示：ISO 尽量落到本地可读；解析失败则原样。 */
    fun displayWithdrawTime(raw: String?): String {
        val s = raw?.trim().orEmpty()
        if (s.isEmpty()) return "--"
        return try {
            val instant = java.time.Instant.parse(s)
            java.time.format.DateTimeFormatter.ofPattern("yyyy-MM-dd HH:mm")
                .withZone(java.time.ZoneId.systemDefault())
                .format(instant)
        } catch (_: Throwable) {
            s.replace('T', ' ').removeSuffix("Z")
        }
    }

}
