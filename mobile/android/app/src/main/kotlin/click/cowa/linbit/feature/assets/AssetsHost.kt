package click.cowa.linbit.feature.assets

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import click.cowa.linbit.data.assets.AssetsNav
import click.cowa.linbit.data.assets.AssetsRoute
import click.cowa.linbit.data.assets.WithdrawAddressPrefill
import click.cowa.linbit.ui.theme.LB

/** 资产子页全屏覆盖层（充值 / 提现 / 划转 / 地址簿）。 */
@Composable
fun AssetsHost(assetsNav: AssetsNav, vm: AssetsViewModel = hiltViewModel()) {
    val route by assetsNav.route.collectAsStateWithLifecycle()
    val r = route ?: return
    val p = LB.p
    Box(Modifier.fillMaxSize().background(p.bg).statusBarsPadding()) {
        when (r) {
            is AssetsRoute.Transfer -> TransferScreen(vm, onBack = assetsNav::clear)
            is AssetsRoute.CoinPicker -> CoinPickerScreen(
                mode = r.mode,
                vm = vm,
                onBack = assetsNav::clear,
                onPick = { asset ->
                    when (r.mode) {
                        AssetsRoute.CoinPicker.Mode.Deposit -> assetsNav.open(AssetsRoute.Deposit(asset))
                        AssetsRoute.CoinPicker.Mode.Withdraw -> assetsNav.open(AssetsRoute.Withdraw(asset))
                    }
                },
            )
            is AssetsRoute.Deposit -> DepositScreen(
                asset = r.asset ?: "USDT",
                vm = vm,
                onBack = assetsNav::clear,
                onChangeAsset = { assetsNav.open(AssetsRoute.CoinPicker(AssetsRoute.CoinPicker.Mode.Deposit)) },
            )
            is AssetsRoute.Withdraw -> WithdrawScreen(
                assetInit = r.asset,
                vm = vm,
                assetsNav = assetsNav,
                onBack = assetsNav::clear,
                onChangeAsset = { assetsNav.open(AssetsRoute.CoinPicker(AssetsRoute.CoinPicker.Mode.Withdraw)) },
            )
            is AssetsRoute.AddressBook -> AddressBookScreen(
                vm = vm,
                onBack = {
                    if (assetsNav.addressBookForWithdraw) {
                        assetsNav.addressBookForWithdraw = false
                        assetsNav.openWithdraw()
                    } else assetsNav.clear()
                },
                onAdd = { assetsNav.openAddAddress(it) },
                onPickForWithdraw = if (assetsNav.addressBookForWithdraw) { { a ->
                    assetsNav.pickWithdrawAddress(
                        WithdrawAddressPrefill(
                            asset = a.asset,
                            network = a.network,
                            address = a.address,
                            memo = a.memo,
                        ),
                    )
                } } else null,
            )
            is AssetsRoute.AddAddress -> AddAddressScreen(
                assetInit = r.asset,
                vm = vm,
                onBack = { assetsNav.openAddressBook() },
                onDone = { assetsNav.openAddressBook() },
            )
            is AssetsRoute.WithdrawRecords -> WithdrawRecordsScreen(vm, onBack = assetsNav::clear)
        }
    }
}
