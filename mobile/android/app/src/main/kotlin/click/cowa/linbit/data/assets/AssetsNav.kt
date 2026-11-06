package click.cowa.linbit.data.assets

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/** 资产子页路由（全屏覆盖，对应 iOS Router.assetsPath）。 */
sealed class AssetsRoute {
    data object Transfer : AssetsRoute()
    data class Deposit(val asset: String? = null) : AssetsRoute()
    data class CoinPicker(val mode: Mode = Mode.Deposit) : AssetsRoute() {
        enum class Mode { Deposit, Withdraw }
    }
    data class Withdraw(val asset: String? = null) : AssetsRoute()
    data object AddressBook : AssetsRoute()
    data class AddAddress(val asset: String? = null) : AssetsRoute()
    data object WithdrawRecords : AssetsRoute()
}

/** 从地址簿选回提现表单的预填（本地状态，非生产资金）。 */
data class WithdrawAddressPrefill(
    val asset: String,
    val network: String,
    val address: String,
    val memo: String? = null,
)

class AssetsNav {
    private val _route = MutableStateFlow<AssetsRoute?>(null)
    val route: StateFlow<AssetsRoute?> = _route.asStateFlow()
    /** 首页「充值」等：切到资产 Tab 后由 AppRoot 消费。 */
    private val _pendingTab = MutableStateFlow(false)
    val pendingAssetsTab: StateFlow<Boolean> = _pendingTab.asStateFlow()

    /** 提现页打开地址簿时置 true；选中地址后写 prefill 并回到 Withdraw。 */
    var addressBookForWithdraw: Boolean = false
    private val _withdrawPrefill = MutableStateFlow<WithdrawAddressPrefill?>(null)
    val withdrawPrefill: StateFlow<WithdrawAddressPrefill?> = _withdrawPrefill.asStateFlow()

    fun open(r: AssetsRoute, switchTab: Boolean = false) {
        if (switchTab) _pendingTab.value = true
        _route.value = r
    }
    fun openDeposit(asset: String? = null, switchTab: Boolean = false) =
        open(if (asset.isNullOrBlank()) AssetsRoute.CoinPicker(AssetsRoute.CoinPicker.Mode.Deposit) else AssetsRoute.Deposit(asset), switchTab)
    fun openTransfer(switchTab: Boolean = false) = open(AssetsRoute.Transfer, switchTab)
    fun openWithdraw(asset: String? = null, switchTab: Boolean = false) =
        open(if (asset.isNullOrBlank()) AssetsRoute.CoinPicker(AssetsRoute.CoinPicker.Mode.Withdraw) else AssetsRoute.Withdraw(asset), switchTab)
    fun openAddressBook(forWithdraw: Boolean = false) {
        addressBookForWithdraw = forWithdraw
        open(AssetsRoute.AddressBook)
    }
    fun openAddAddress(asset: String? = null) = open(AssetsRoute.AddAddress(asset))
    fun pickWithdrawAddress(prefill: WithdrawAddressPrefill) {
        _withdrawPrefill.value = prefill
        addressBookForWithdraw = false
        open(AssetsRoute.Withdraw(prefill.asset))
    }
    fun consumeWithdrawPrefill(): WithdrawAddressPrefill? {
        val v = _withdrawPrefill.value
        _withdrawPrefill.value = null
        return v
    }
    fun clear() {
        _route.value = null
        addressBookForWithdraw = false
    }
    fun consumeTab(): Boolean {
        val v = _pendingTab.value
        _pendingTab.value = false
        return v
    }
}
