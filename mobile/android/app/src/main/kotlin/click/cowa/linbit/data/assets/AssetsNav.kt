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

class AssetsNav {
    private val _route = MutableStateFlow<AssetsRoute?>(null)
    val route: StateFlow<AssetsRoute?> = _route.asStateFlow()
    /** 首页「充值」等：切到资产 Tab 后由 AppRoot 消费。 */
    private val _pendingTab = MutableStateFlow(false)
    val pendingAssetsTab: StateFlow<Boolean> = _pendingTab.asStateFlow()

    fun open(r: AssetsRoute, switchTab: Boolean = false) {
        if (switchTab) _pendingTab.value = true
        _route.value = r
    }
    fun openDeposit(asset: String? = null, switchTab: Boolean = false) =
        open(if (asset.isNullOrBlank()) AssetsRoute.CoinPicker(AssetsRoute.CoinPicker.Mode.Deposit) else AssetsRoute.Deposit(asset), switchTab)
    fun openTransfer(switchTab: Boolean = false) = open(AssetsRoute.Transfer, switchTab)
    fun openWithdraw(asset: String? = null, switchTab: Boolean = false) =
        open(if (asset.isNullOrBlank()) AssetsRoute.CoinPicker(AssetsRoute.CoinPicker.Mode.Withdraw) else AssetsRoute.Withdraw(asset), switchTab)
    fun openAddressBook() = open(AssetsRoute.AddressBook)
    fun openAddAddress(asset: String? = null) = open(AssetsRoute.AddAddress(asset))
    fun clear() { _route.value = null }
    fun consumeTab(): Boolean {
        val v = _pendingTab.value
        _pendingTab.value = false
        return v
    }
}
