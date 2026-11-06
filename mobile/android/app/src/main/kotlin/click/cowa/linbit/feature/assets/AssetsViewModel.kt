package click.cowa.linbit.feature.assets

import androidx.lifecycle.ViewModel
import androidx.compose.runtime.getValue
import androidx.compose.runtime.setValue
import androidx.lifecycle.viewModelScope
import click.cowa.linbit.core.generated.ApiBalance
import click.cowa.linbit.core.generated.ApiCreateWithdrawAddressRequest
import click.cowa.linbit.core.generated.ApiDepositAddress
import click.cowa.linbit.core.generated.ApiFuturesAccount
import click.cowa.linbit.core.generated.ApiFuturesTransferRequest
import click.cowa.linbit.core.generated.ApiInternalTransferRequest
import click.cowa.linbit.core.generated.ApiSetWhitelistRequest
import click.cowa.linbit.core.generated.ApiUpdateWithdrawAddressRequest
import click.cowa.linbit.core.generated.ApiWalletNetwork
import click.cowa.linbit.core.generated.ApiWithdrawAddress
import click.cowa.linbit.core.generated.ApiWithdrawNetwork
import click.cowa.linbit.core.generated.ApiWithdrawRequest
import click.cowa.linbit.core.generated.ApiSendCodeAuthedRequest
import click.cowa.linbit.core.generated.ApiWithdrawal
import click.cowa.linbit.core.network.LinbitApi
import click.cowa.linbit.core.network.TokenStore
import click.cowa.linbit.core.network.apiCall
import click.cowa.linbit.core.network.lbMessage
import click.cowa.linbit.core.util.Dec
import click.cowa.linbit.data.MarketStore
import click.cowa.linbit.ui.components.ToastCenter
import dagger.hilt.android.lifecycle.HiltViewModel
import java.math.BigDecimal
import javax.inject.Inject
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch

data class AssetsUiState(
    val loading: Boolean = false,
    val error: String? = null,
    val spot: List<ApiBalance> = emptyList(),
    val funding: List<ApiBalance> = emptyList(),
    val futures: ApiFuturesAccount? = null,
    val hideAmount: Boolean = false,
    val hideSmall: Boolean = false,
    val query: String = "",
    val tab: Int = 0,
    val cnyRate: BigDecimal? = null,
)

@HiltViewModel
class AssetsViewModel @Inject constructor(
    private val api: LinbitApi,
    private val tokens: TokenStore,
    private val market: MarketStore,
    val toast: ToastCenter,
) : ViewModel() {
    private val _s = MutableStateFlow(AssetsUiState())
    val state: StateFlow<AssetsUiState> = _s.asStateFlow()
    private var withdrawJob: kotlinx.coroutines.Job? = null
    private var addressJob: kotlinx.coroutines.Job? = null
    private var addressGeneration = 0L
    private val withdrawEpoch = kotlinx.coroutines.flow.MutableStateFlow(0L)

    val loggedIn: StateFlow<Boolean> = tokens.loggedIn
    val sessionVersion: StateFlow<Long> = tokens.sessionVersion
    val marketState = market.state.stateIn(viewModelScope, SharingStarted.Eagerly, market.state.value)

    fun setTab(i: Int) { _s.value = _s.value.copy(tab = i) }
    fun toggleHide() { _s.value = _s.value.copy(hideAmount = !_s.value.hideAmount) }
    fun setHideSmall(v: Boolean) { _s.value = _s.value.copy(hideSmall = v) }
    fun setQuery(q: String) { _s.value = _s.value.copy(query = q) }

    fun usd(b: ApiBalance): BigDecimal {
        val last = { sym: String ->
            market.state.value.spot.firstOrNull { it.symbol == sym }?.last ?: BigDecimal.ZERO
        }
        return AssetsLogic.usd(b, last)
    }

    fun rows(): List<ApiBalance> {
        val s = _s.value
        val src = when (s.tab) {
            1 -> s.funding
            3 -> s.spot
            else -> s.spot // 总览 / 现货共用现货余额列表；合约走 futures 卡片
        }
        return AssetsLogic.filterBalances(src, s.query, s.hideSmall, ::usd)
    }

    fun totalUsdt(): BigDecimal {
        val s = _s.value
        return when (s.tab) {
            1 -> s.funding.fold(BigDecimal.ZERO) { a, b -> a + usd(b) }
            2 -> Dec.parse(s.futures?.marginBalance ?: s.futures?.walletBalance)
            3 -> s.spot.fold(BigDecimal.ZERO) { a, b -> a + usd(b) }
            else -> s.spot.fold(BigDecimal.ZERO) { a, b -> a + usd(b) } +
                Dec.parse(s.futures?.marginBalance ?: s.futures?.walletBalance)
        }
    }

    fun spotAvailUsdt(): BigDecimal =
        Dec.parse(_s.value.spot.firstOrNull { it.asset.equals("USDT", true) }?.free)

    fun fundingAvailUsdt(): BigDecimal =
        Dec.parse(_s.value.funding.firstOrNull { it.asset.equals("USDT", true) }?.free)

    fun futuresAvail(): BigDecimal = Dec.parse(_s.value.futures?.availableBalance)

    fun load(silent: Boolean = false) {
        if (!tokens.loggedIn.value) {
            _s.value = AssetsUiState(cnyRate = market.state.value.cnyRate ?: _s.value.cnyRate)
            return
        }
        viewModelScope.launch {
            if (!silent) _s.value = _s.value.copy(loading = true, error = null)
            try {
                val spot = apiCall { api.balances(account = "spot", pageSize = 200) }.items
                val funding = runCatching { apiCall { api.balances(account = "funding", pageSize = 200) }.items }.getOrDefault(emptyList())
                val futures = runCatching { apiCall { api.futuresAccount() } }.getOrNull()
                val rate = market.state.value.cnyRate ?: _s.value.cnyRate
                _s.value = _s.value.copy(
                    loading = false, error = null, spot = spot, funding = funding, futures = futures, cnyRate = rate,
                )
            } catch (e: Exception) {
                _s.value = _s.value.copy(loading = false, error = e.lbMessage())
            }
        }
    }

    // ---- 充值 ----
    suspend fun loadDepositNetworks(asset: String): Result<List<ApiWalletNetwork>> = runCatching {
        val raw = apiCall { api.walletNetworks(asset) }.networks
        AssetsLogic.depositNetworks(asset, raw)
    }

    suspend fun loadDepositAddress(asset: String, network: String): Result<ApiDepositAddress> = runCatching {
        apiCall { api.depositAddress(asset, network) }
    }

    suspend fun withdrawDebitAccount(): String = apiCall { api.assetsOverview() }.withdrawAccount?.value ?: "funding"

    // ---- 提现网络（min 来自后端 WALLET_NETWORKS_JSON）----
    /** 离屏证据夹具：本地示例网络，非生产。 */
    internal var evidenceWithdrawNets: List<ApiWithdrawNetwork>? = null
    internal var evidenceWalletNets: List<ApiWalletNetwork>? = null
    internal var evidenceTwoFactor: Boolean? = null

    /** 离屏证据：写入示例余额（非生产）。 */
    internal fun seedEvidenceBalances(funding: List<ApiBalance> = emptyList(), spot: List<ApiBalance> = emptyList()) {
        _s.value = _s.value.copy(funding = funding, spot = spot, loading = false, error = null)
    }

    suspend fun loadWithdrawNetworks(asset: String): Result<List<ApiWithdrawNetwork>> {
        evidenceWithdrawNets?.let { return Result.success(AssetsLogic.withdrawEnabledNetworks(it)) }
        return runCatching {
            AssetsLogic.withdrawEnabledNetworks(apiCall { api.withdrawNetworks(asset) }.networks)
        }
    }

    suspend fun loadWalletNetworksForWithdraw(asset: String): Result<List<ApiWalletNetwork>> {
        evidenceWalletNets?.let { return Result.success(AssetsLogic.withdrawEnabledNetworks(it)) }
        return runCatching {
            AssetsLogic.withdrawEnabledNetworks(apiCall { api.walletNetworks(asset) }.networks)
        }
    }


    /** 未开 2FA 时向本人发 purpose=withdraw 验证码（对照 OpenAPI /account/code/send）。 */
    suspend fun sendWithdrawCode(): Result<*> = runCatching {
        apiCall { api.sendAccountCode(ApiSendCodeAuthedRequest(purpose = ApiSendCodeAuthedRequest.Purpose.WITHDRAW)) }
    }

    /** 是否已启用谷歌验证器（决定 totpCode vs code）。 */
    suspend fun twoFactorEnabled(): Boolean {
        evidenceTwoFactor?.let { return it }
        return runCatching {
            apiCall { api.securityOverview() }.twoFactor?.enabled == true
        }.getOrDefault(false)
    }

    /** 提交链上提现。调用方负责二次确认；单测不得调用真实资金。 */
    suspend fun submitWithdraw(req: ApiWithdrawRequest): Result<ApiWithdrawal> = runCatching {
        apiCall { api.withdraw(req) }
    }

    suspend fun submitInternal(req: ApiInternalTransferRequest): Result<*> = runCatching {
        apiCall { api.internalTransfer(req) }
    }

    data class WithdrawReceipt(val submission: ApiWithdrawal, val readback: ApiWithdrawal, val account: String, val balance: BigDecimal)
    data class AddressReceipt(val created: ApiWithdrawAddress, val readback: ApiWithdrawAddress)
    var addressBusy by androidx.compose.runtime.mutableStateOf(false); private set
    var addressError by androidx.compose.runtime.mutableStateOf<String?>(null); private set
    var addressReceipt by androidx.compose.runtime.mutableStateOf<AddressReceipt?>(null); private set
    private var addressAccepted = false
    fun submitCreateAddressConfirmed(req: ApiCreateWithdrawAddressRequest): Boolean {
        if (addressBusy || addressAccepted || !tokens.loggedIn.value) return false
        addressBusy=true; addressError=null; addressReceipt=null
        val generation=++addressGeneration; val epoch=withdrawEpoch.value
        addressJob=viewModelScope.launch {
            try {
                val created=apiCall{api.createWithdrawAddress(req)}
                addressAccepted=true
                val items=apiCall{api.withdrawAddresses()}.items
                val actual=items.firstOrNull{it.id==created.id}
                if(epoch!=withdrawEpoch.value||generation!=addressGeneration)return@launch
                require(actual!=null){"地址已提交，但列表读回未找到该地址；请稍后查看地址簿。"}
                addressReceipt=AddressReceipt(created,actual)
            } catch(e:kotlinx.coroutines.CancellationException){throw e}
            catch(e:Throwable){if(epoch==withdrawEpoch.value&&generation==addressGeneration)addressError=e.lbMessage()}
            finally{if(epoch==withdrawEpoch.value&&generation==addressGeneration)addressBusy=false}
        };return true
    }
    data class AddressEditReceipt(val updated: ApiWithdrawAddress, val readback: ApiWithdrawAddress)
    var addressEditBusy by androidx.compose.runtime.mutableStateOf(false); private set
    var addressEditError by androidx.compose.runtime.mutableStateOf<String?>(null); private set
    var addressEditReceipt by androidx.compose.runtime.mutableStateOf<AddressEditReceipt?>(null); private set
    private var addressEditAccepted = false
    fun submitEditAddressConfirmed(id: String, label: String, memo: String, totp: String): Boolean {
        if (addressEditBusy || addressEditAccepted || addressBusy || !tokens.loggedIn.value) return false
        addressEditBusy=true; addressEditError=null; addressEditReceipt=null
        val generation=++addressGeneration; val epoch=withdrawEpoch.value
        addressJob=viewModelScope.launch {
            try {
                val updated=apiCall{api.updateWithdrawAddress(id,ApiUpdateWithdrawAddressRequest(label=label,memo=memo,totpCode=totp))}
                addressEditAccepted=true
                val actual=apiCall{api.withdrawAddresses()}.items.firstOrNull{it.id==id}
                if(epoch!=withdrawEpoch.value||generation!=addressGeneration)return@launch
                require(actual!=null&&actual.label==label&&actual.memo==memo){"地址修改已返回，但列表读回未确认同一记录的标签/Memo；请稍后查看地址簿。"}
                addressEditReceipt=AddressEditReceipt(updated,actual)
            }catch(e:kotlinx.coroutines.CancellationException){throw e}
            catch(e:Throwable){if(epoch==withdrawEpoch.value&&generation==addressGeneration)addressEditError=e.lbMessage()}
            finally{if(epoch==withdrawEpoch.value&&generation==addressGeneration)addressEditBusy=false}
        };return true
    }

    fun acknowledgeAddressEdit(){if(addressEditReceipt!=null){addressEditReceipt=null;addressEditError=null}}

    fun clearAddressFeedback(){if(!addressBusy&&!addressAccepted){addressError=null;addressReceipt=null}}

    var withdrawBusy by androidx.compose.runtime.mutableStateOf(false); private set
    var withdrawError by androidx.compose.runtime.mutableStateOf<String?>(null); private set
    var withdrawAwaitingReadback by androidx.compose.runtime.mutableStateOf(false); private set
    var withdrawReceipt by androidx.compose.runtime.mutableStateOf<WithdrawReceipt?>(null); private set
    private var withdrawTxGeneration = 0L
    fun submitWithdrawConfirmed(req: ApiWithdrawRequest): Boolean {
        if (withdrawBusy || withdrawAwaitingReadback || !tokens.loggedIn.value) return false
        withdrawBusy = true; withdrawError = null; withdrawReceipt = null
        val epoch=withdrawEpoch.value; val generation=++withdrawTxGeneration
        withdrawJob=viewModelScope.launch {
            try {
                val submitted=apiCall{api.withdraw(req)}
                withdrawAwaitingReadback = true
                val account=apiCall{api.assetsOverview()}.withdrawAccount?.value?:"funding"
                check(account=="funding"||account=="spot"){"未知提现账户"}
                val balance=runCatching{apiCall{api.balances(account=account,pageSize=200)}}.getOrNull()
                val records=runCatching{apiCall{api.withdrawRecords(page=1,pageSize=20)}}.getOrNull()
                if(epoch!=withdrawEpoch.value||generation!=withdrawTxGeneration)return@launch
                val free=balance?.items?.firstOrNull{it.asset.equals(req.asset,true)}?.free
                val actual=records?.items?.firstOrNull{it.id==submitted.id}
                if(free==null||actual==null){withdrawError="申请已返回，但余额/提现记录读回未完成，请稍后查看记录。";return@launch}
                _s.value=_s.value.copy(spot=if(account=="spot")balance!!.items else _s.value.spot,funding=if(account=="funding")balance!!.items else _s.value.funding,loading=false,error=null)
                withdrawReceipt=WithdrawReceipt(submitted,actual,account,Dec.parse(free)); withdrawAwaitingReadback=false
            }catch(e:kotlinx.coroutines.CancellationException){throw e}
            catch(e:Throwable){if(epoch==withdrawEpoch.value&&generation==withdrawTxGeneration)withdrawError=e.lbMessage()}
            finally{if(epoch==withdrawEpoch.value&&generation==withdrawTxGeneration)withdrawBusy=false}
        }
        return true
    }
    fun clearWithdrawFeedback(){if(!withdrawBusy&&!withdrawAwaitingReadback){withdrawError=null;withdrawReceipt=null}}

    data class WithdrawCancelReceipt(val readback: ApiWithdrawal, val account: String, val balance: BigDecimal)
    var withdrawCancelBusy by androidx.compose.runtime.mutableStateOf(false); private set
    var withdrawCancelError by androidx.compose.runtime.mutableStateOf<String?>(null); private set
    var withdrawCancelConflict by androidx.compose.runtime.mutableStateOf<ApiWithdrawal?>(null); private set
    var withdrawCancelReceipt by androidx.compose.runtime.mutableStateOf<WithdrawCancelReceipt?>(null); private set
    private var withdrawCancelAccepted = false
    private var withdrawCancelGeneration = 0L
    private var withdrawCancelJob: kotlinx.coroutines.Job? = null
    fun submitWithdrawCancelConfirmed(target: ApiWithdrawal): Boolean {
        if(target.status!=ApiWithdrawal.Status.PENDING || withdrawCancelBusy || withdrawCancelAccepted || withdrawBusy || withdrawAwaitingReadback || !tokens.loggedIn.value) return false
        withdrawCancelBusy=true;withdrawCancelError=null;withdrawCancelReceipt=null;withdrawCancelConflict=null
        val generation=++withdrawCancelGeneration;val epoch=tokens.sessionVersion.value
        withdrawCancelJob=viewModelScope.launch {
            try {
                val submitted=apiCall{api.cancelWithdraw(target.id)};withdrawCancelAccepted=true
                check(submitted.id==target.id&&submitted.status==ApiWithdrawal.Status.CANCELED){"撤销已受理，但返回记录不是同 id 已撤销状态；请查看提现记录。"}
                val account=apiCall{api.assetsOverview()}.withdrawAccount?.value?:"funding"
                check(account=="funding"||account=="spot"){"未知提现原扣款账户"}
                val balance=apiCall{api.balances(account=account,pageSize=200)}
                val actual=apiCall{api.withdrawRecords(page=1,pageSize=20)}.items.firstOrNull{it.id==target.id}
                val assetBalance=balance.items.firstOrNull{it.asset.equals(target.asset,true)}
                if(epoch!=tokens.sessionVersion.value||generation!=withdrawCancelGeneration)return@launch
                require(actual!=null&&actual.status==ApiWithdrawal.Status.CANCELED&&assetBalance!=null){"撤销已受理，但记录或原扣款账户余额读回未完成；不得重复撤销，请刷新查看。"}
                _s.value=_s.value.copy(spot=if(account=="spot")balance.items else _s.value.spot,funding=if(account=="funding")balance.items else _s.value.funding)
                withdrawCancelReceipt=WithdrawCancelReceipt(actual,account,Dec.parse(assetBalance.free))
            }catch(e:kotlinx.coroutines.CancellationException){throw e}
            catch(e:Throwable){
                if(epoch!=tokens.sessionVersion.value||generation!=withdrawCancelGeneration)return@launch
                withdrawCancelError=e.lbMessage()
                if(!withdrawCancelAccepted && e is click.cowa.linbit.core.network.ApiFailure.Server && e.status==409){
                    runCatching {
                        val actual=apiCall{api.withdrawRecords(page=1,pageSize=20)}.items.firstOrNull{it.id==target.id}
                        if(epoch==tokens.sessionVersion.value&&generation==withdrawCancelGeneration)withdrawCancelConflict=actual
                    }
                }
            }finally{if(epoch==tokens.sessionVersion.value&&generation==withdrawCancelGeneration)withdrawCancelBusy=false}
        };return true
    }

    // ---- 划转：现货(资金) ↔ 合约，仅 USDT ----
    data class TransferReceipt(val ref: String?, val status: String, val balanceRefreshed: Boolean = false, val spotFree: String? = null, val futuresAvailable: String? = null)
    var transferBusy by androidx.compose.runtime.mutableStateOf(false); private set
    var transferError by androidx.compose.runtime.mutableStateOf<String?>(null); private set
    var transferReceipt by androidx.compose.runtime.mutableStateOf<TransferReceipt?>(null); private set
    private var transferJob: kotlinx.coroutines.Job? = null
    private var transferGeneration = 0L
    private var transferDirection: Boolean? = null
    var transferAccepted by androidx.compose.runtime.mutableStateOf(false); private set
    init { viewModelScope.launch { tokens.sessionVersion.collect {
        transferGeneration++; transferJob?.cancel(); transferBusy = false; transferError = null; transferReceipt = null; transferAccepted=false;transferDirection=null
        withdrawEpoch.value = tokens.sessionVersion.value; withdrawJob?.cancel(); withdrawBusy = false; withdrawError = null; withdrawReceipt = null; withdrawAwaitingReadback = false; withdrawCancelGeneration++;withdrawCancelJob?.cancel();withdrawCancelBusy=false;withdrawCancelError=null;withdrawCancelConflict=null;withdrawCancelReceipt=null;withdrawCancelAccepted=false; addressGeneration++; addressJob?.cancel(); addressBusy=false; addressError=null; addressReceipt=null; addressAccepted=false; addressEditAccepted=false; addressEditBusy=false; addressEditError=null; addressEditReceipt=null
    } } }
    fun transferFutures(toFutures: Boolean, amount: String): Boolean {
        if (transferBusy || transferAccepted || !tokens.loggedIn.value) return false
        if(transferDirection==null)transferDirection=toFutures
        if(transferDirection!=toFutures)return false
        transferBusy=true;transferError=null
        val generation=++transferGeneration;val epoch=tokens.sessionVersion.value
        transferJob = viewModelScope.launch {
            try {
                val dir = if (toFutures) ApiFuturesTransferRequest.Direction.IN else ApiFuturesTransferRequest.Direction.OUT
                val r=apiCall { api.futuresTransfer(ApiFuturesTransferRequest(dir, amount)) }
                transferAccepted=true
                if (transferGeneration==generation && tokens.sessionVersion.value==epoch) {
                    transferReceipt=TransferReceipt(r.ref,r.status?:"pending")
                    refreshBalances(epoch,generation)
                }
            } catch(e:kotlinx.coroutines.CancellationException){throw e}
            catch(e:Throwable){if(transferGeneration==generation&&tokens.sessionVersion.value==epoch)transferError=e.lbMessage()}
            finally{if(transferGeneration==generation&&tokens.sessionVersion.value==epoch)transferBusy=false}
        }
        return true
    }
    private suspend fun refreshBalances(epoch:Long,generation:Long) {
        val spot=runCatching{apiCall{api.balances(account="spot",pageSize=200)}}.getOrNull()
        val futures=runCatching{apiCall{api.futuresAccount()}}.getOrNull()
        if (transferGeneration!=generation || tokens.sessionVersion.value!=epoch) return
        val current=transferReceipt ?: return
        transferReceipt=current.copy(balanceRefreshed=spot!=null&&futures!=null,spotFree=spot?.items?.firstOrNull{it.asset.equals("USDT",true)}?.free,futuresAvailable=futures?.availableBalance)
        if(spot!=null&&futures!=null)_s.value=_s.value.copy(spot=spot.items,futures=futures,loading=false,error=null)
    }
    /** Accepted transfers can only read balances again; never recreate a mutation. */
    fun refreshAcceptedTransferBalances():Boolean {
        if(!transferAccepted||transferBusy)return false
        transferBusy=true
        val generation=transferGeneration;val epoch=tokens.sessionVersion.value
        transferJob=viewModelScope.launch {
            try{refreshBalances(epoch,generation)}
            finally{if(transferGeneration==generation&&tokens.sessionVersion.value==epoch)transferBusy=false}
        }
        return true
    }
    fun clearTransferFeedback(){if(!transferBusy&&!transferAccepted){transferError=null;transferReceipt=null;transferDirection=null}}
    override fun onCleared(){transferGeneration++;transferJob?.cancel();withdrawEpoch.value++;withdrawJob?.cancel();withdrawCancelGeneration++;withdrawCancelJob?.cancel();addressGeneration++;addressJob?.cancel();super.onCleared()}

    // ---- 地址簿 ----
    /** 离屏证据：地址簿列表/白名单开关（非生产）。 */
    internal var evidenceAddresses: List<ApiWithdrawAddress>? = null
    internal var evidenceWhitelist: Boolean? = null

    suspend fun addresses(): Result<List<ApiWithdrawAddress>> {
        evidenceAddresses?.let { return Result.success(it) }
        return runCatching { apiCall { api.withdrawAddresses() }.items }
    }

    suspend fun whitelist(): Result<Boolean> {
        evidenceWhitelist?.let { return Result.success(it) }
        return runCatching { apiCall { api.withdrawWhitelist() }.enabled }
    }

    suspend fun setWhitelist(enabled: Boolean, totp: String): Result<Boolean> = runCatching {
        apiCall { api.setWithdrawWhitelist(ApiSetWhitelistRequest(enabled = enabled, totpCode = totp)) }.enabled
    }

    suspend fun createAddress(req: ApiCreateWithdrawAddressRequest): Result<ApiWithdrawAddress> = runCatching {
        apiCall { api.createWithdrawAddress(req) }
    }

    suspend fun updateAddressWhitelist(id: String, whitelisted: Boolean, totp: String): Result<*> = runCatching {
        apiCall { api.updateWithdrawAddress(id, ApiUpdateWithdrawAddressRequest(whitelisted = whitelisted, totpCode = totp)) }
    }

    suspend fun withdrawRecords(): Result<List<ApiWithdrawal>> {
        evidenceWithdrawRecords?.let { return Result.success(it) }
        return runCatching { apiCall { api.withdrawRecords() }.items }
    }

    /** 离屏证据：提现记录示例（非生产）。 */
    internal var evidenceWithdrawRecords: List<ApiWithdrawal>? = null

    /** 撤销 pending 提现。调用方二次确认；测试/生产均不得静默连点。 */
    suspend fun cancelWithdraw(id: String): Result<ApiWithdrawal> = runCatching {
        require(id.isNotBlank()) { "无效提现单" }
        apiCall { api.cancelWithdraw(id) }
    }

    fun coinUniverse(): List<String> {
        val fromSpot = market.state.value.spot.map { it.base }.filter { it.isNotBlank() }
        val fromBal = (_s.value.spot + _s.value.funding).map { it.asset }
        return (fromSpot + fromBal + listOf("USDT", "BTC", "ETH", "BNB", "SOL", "TRX", "DOGE")).toSet().sorted()
    }
}
