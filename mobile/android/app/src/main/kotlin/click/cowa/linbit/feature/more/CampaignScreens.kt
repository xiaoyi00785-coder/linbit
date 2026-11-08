package click.cowa.linbit.feature.more

import android.content.Intent
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import click.cowa.linbit.R
import click.cowa.linbit.core.generated.*
import click.cowa.linbit.core.network.*
import click.cowa.linbit.ui.components.*
import click.cowa.linbit.ui.theme.*
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.async
import kotlinx.coroutines.launch
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import java.math.BigDecimal
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import javax.inject.Inject

// ---------------- 服务端契约（接入点；字段说明见 docs/batch3-backend-fields.md） ----------------
@Serializable data class ApiCashbackTier(val id: String, val depositAmount: String, val bonusAmount: String, val dailyFuturesVolume: String, val asset: String = "USDT", val joined: Boolean = false)
@Serializable data class ApiCashbackCampaign(val configured: Boolean = false, val available: Boolean = false, val title: String? = null, val subtitle: String? = null, val startAt: Long? = null, val endAt: Long? = null, val tiers: List<ApiCashbackTier> = emptyList(), val rules: List<String> = emptyList(), val eligible: Boolean? = null)
@Serializable data class ApiLotteryPrize(val asset: String, val amount: String)
@Serializable data class ApiLotteryCampaign(val configured: Boolean = false, val available: Boolean = false, val title: String? = null, val startAt: Long? = null, val endAt: Long? = null, val prizes: List<ApiLotteryPrize> = emptyList(), val winners: List<String> = emptyList(), val rules: List<ApiRuleItem> = emptyList(), val availableDraws: Int = 0)
@Serializable data class ApiRuleItem(val title: String, val body: String)
@Serializable data class ApiLotteryEntry(val orderId: String, val symbol: String, val closedAt: Long? = null, val status: String, val prize: ApiLotteryPrize? = null)
@Serializable data class ApiLotteryEntries(val items: List<ApiLotteryEntry> = emptyList(), val configured: Boolean = false)
@Serializable data class ApiKeyItem(val id: String, val label: String, val apiKey: String, val permissions: List<String> = listOf("read"), val ipWhitelist: List<String> = emptyList(), val createdAt: Long? = null)
@Serializable data class ApiKeyRevoked(val revoked: Boolean = false)
@Serializable data class ApiKeyList(val items: List<ApiKeyItem> = emptyList())
@Serializable data class ApiKeyCreateRequest(val label: String, val permissions: List<String> = listOf("read"), val ipWhitelist: List<String> = emptyList(), val totpCode: String)
/** secret 只在创建响应出现一次；toString 屏蔽，避免进日志。 */
@Serializable data class ApiKeyCreated(val item: ApiKeyItem, @SerialName("secret") val secret: String) { override fun toString() = "ApiKeyCreated(item=$item, secret=***)" }
object ApiKeyLimits { const val MAX_KEYS = 10; const val MAX_IPS = 20 }
fun apiKeyError(t: Throwable): String = when ((t as? ApiFailure.Server)?.error?.code) {
    "TWOFA_REQUIRED" -> "请先开启谷歌验证（TOTP）后再创建 API Key"; "INVALID_TOTP" -> "谷歌验证码错误"; "PERMISSION_NOT_ALLOWED" -> "仅支持只读权限"; else -> t.lbMessage() }

/** 活动页统一的深色视觉（参考 App 的活动 H5 固定深色 + 荧光绿强调）。 */
internal object CampaignPalette {
    val bg = Color(0xFF000000); val card = Color(0xFF15161A); val line = Color(0xFF2A2C31)
    val accent = Color(0xFFC6F432); val text = Color.White; val sub = Color(0xFF9A9CA3)
}

internal fun campaignTime(ms: Long?): String = ms?.let { DateTimeFormatter.ofPattern("yyyy-MM-dd HH:mm:ss").withZone(ZoneId.of("UTC")).format(Instant.ofEpochMilli(it)) } ?: "--"
internal fun campaignPeriod(start: Long?, end: Long?) = "${campaignTime(start)} ~ ${campaignTime(end)} (UTC+0)"
private fun day(ms: Long?) = ms?.let { DateTimeFormatter.ofPattern("yyyy-MM-dd HH:mm").withZone(ZoneId.systemDefault()).format(Instant.ofEpochMilli(it)) } ?: "--"

@Composable
private fun CampaignFrame(title: String, onBack: () -> Unit, onRefresh: (() -> Unit)?, content: @Composable ColumnScope.() -> Unit) {
    val c = CampaignPalette
    Column(Modifier.fillMaxSize().background(c.bg)) {
        Row(Modifier.fillMaxWidth().height(48.dp).padding(horizontal = 4.dp), verticalAlignment = Alignment.CenterVertically) {
            Box(Modifier.size(48.dp).pressable(scale = 1f, onClick = onBack), contentAlignment = Alignment.Center) { LbIcon(R.drawable.ic_arrow_left, 22.dp, c.text) }
            Text(title, Modifier.weight(1f), style = lb(FontSize.title, FontWeight.SemiBold), color = c.text, textAlign = TextAlign.Center)
            Box(Modifier.size(48.dp).then(if (onRefresh != null) Modifier.pressable(scale = 1f, onClick = onRefresh) else Modifier), contentAlignment = Alignment.Center) {
                if (onRefresh != null) Text("↻", color = c.text, fontSize = 22.sp)
            }
        }
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(horizontal = 16.dp).padding(bottom = 32.dp), content = content)
    }
}

@Composable private fun CCard(modifier: Modifier = Modifier, content: @Composable ColumnScope.() -> Unit) =
    Column(modifier.fillMaxWidth().background(CampaignPalette.card, RoundedCornerShape(16.dp)).border(1.dp, CampaignPalette.line, RoundedCornerShape(16.dp)).padding(16.dp), content = content)

@Composable private fun CButton(title: String, modifier: Modifier = Modifier, enabled: Boolean = true, onClick: () -> Unit) =
    Box(modifier.height(44.dp).background(if (enabled) CampaignPalette.accent else CampaignPalette.line, RoundedCornerShape(22.dp)).pressable(enabled = enabled, onClick = onClick), contentAlignment = Alignment.Center) {
        Text(title, style = lb(FontSize.body, FontWeight.SemiBold), color = if (enabled) Color.Black else CampaignPalette.sub)
    }

@Composable private fun CState(title: String, sub: String? = null, action: String? = null, onAction: () -> Unit = {}) =
    Column(Modifier.fillMaxWidth().padding(vertical = 32.dp), horizontalAlignment = Alignment.CenterHorizontally) {
        LbReferenceIcon(R.drawable.ic_ref_empty_data, 72.dp)
        Spacer(Modifier.height(12.dp))
        Text(title, style = lb(FontSize.body, FontWeight.SemiBold), color = CampaignPalette.text, textAlign = TextAlign.Center)
        sub?.let { Text(it, Modifier.padding(top = 6.dp), style = lb(FontSize.caption), color = CampaignPalette.sub, textAlign = TextAlign.Center) }
        action?.let { Spacer(Modifier.height(16.dp)); CButton(it, Modifier.width(200.dp), onClick = onAction) }
    }

/** 404/501 视为服务端未开通该活动/功能（真实状态），其它错误显示原因 + 重试。 */
internal fun Throwable.serviceMissing(): Boolean = this is ApiFailure.Server && (status == 404 || status == 501)

// ======================= 邀请好友 =======================
data class ReferralState(val loading: Boolean = true, val error: String? = null, val summary: ApiPartnerSummary? = null, val links: ApiPartnerLinks? = null,
    val team: List<ApiPartnerTeamItemsInner> = emptyList(), val commissions: List<ApiPartnerCommissionsItemsInner> = emptyList())

@HiltViewModel
class ReferralViewModel @Inject constructor(private val api: LinbitApi, private val tokens: TokenStore) : ViewModel() {
    var state by mutableStateOf(ReferralState()); private set
    val loggedIn get() = tokens.loggedIn
    fun load() {
        if (!tokens.loggedIn.value) { state = ReferralState(loading = false); return }
        state = state.copy(loading = true, error = null)
        viewModelScope.launch {
            val s = async { runCatching { apiCall { api.partnerSummary() } } }
            val l = async { runCatching { apiCall { api.partnerLinks() } } }
            val t = async { runCatching { apiCall { api.partnerTeam() } } }
            val c = async { runCatching { apiCall { api.partnerCommissions(kind = "referral") } } }
            val links = l.await()
            state = ReferralState(false, links.exceptionOrNull()?.lbMessage(), s.await().getOrNull(), links.getOrNull(),
                t.await().getOrNull()?.items.orEmpty(), c.await().getOrNull()?.items.orEmpty())
        }
    }
}

object ReferralLogic {
    /** 「最高 X% 返佣」取合约/现货返佣比例较大者；服务端没给比例时不写死数字。 */
    fun maxRatePercent(s: ApiPartnerSummary?): String? = listOfNotNull(s?.futuresRate, s?.spotRate).mapNotNull { it.toBigDecimalOrNull() }.maxOrNull()
        ?.let { (if (it <= BigDecimal.ONE) it.multiply(BigDecimal(100)) else it).stripTrailingZeros().toPlainString() }
    fun traded(t: ApiPartnerTeamItemsInner) = listOfNotNull(t.futuresVolume30d, t.spotVolume30d).any { (it.toBigDecimalOrNull() ?: BigDecimal.ZERO).signum() > 0 }
    fun rebate(s: ApiPartnerSummary?, range: Int) = when (range) { 1 -> s?.yesterday; 2 -> s?.month; else -> s?.total } ?: "0"
}

@Composable
fun ReferralScreen(onBack: () -> Unit, onLogin: () -> Unit, vm: ReferralViewModel = hiltViewModel()) {
    val c = CampaignPalette; val st = vm.state
    val loggedIn by vm.loggedIn.collectAsStateWithLifecycle()
    val ctx = LocalContext.current; val clip = LocalClipboardManager.current
    var range by remember { mutableIntStateOf(0) }; var rangeMenu by remember { mutableStateOf(false) }
    LaunchedEffect(loggedIn) { vm.load() }
    val link = st.links?.registerUrl ?: st.links?.url
    fun share() { if (!loggedIn) { onLogin(); return }; if (link == null) return
        ctx.startActivity(Intent.createChooser(Intent(Intent.ACTION_SEND).setType("text/plain").putExtra(Intent.EXTRA_TEXT, link), "邀请好友")) }
    CampaignFrame("邀请好友", onBack, vm::load) {
        Spacer(Modifier.height(16.dp))
        Box(Modifier.fillMaxWidth(), contentAlignment = Alignment.Center) { LbReferenceIcon(R.drawable.ic_ref_asset_gift, 120.dp) }
        val rate = ReferralLogic.maxRatePercent(st.summary)
        Text(buildAnnotatedString { append(if (rate != null) "邀请好友，最高赚取\n" else "邀请好友\n"); withStyle(SpanStyle(color = c.accent)) { append(if (rate != null) "$rate% 返佣" else "赚取交易返佣") } },
            Modifier.fillMaxWidth().padding(vertical = 20.dp), fontSize = 24.sp, fontWeight = FontWeight.Bold, color = c.text, textAlign = TextAlign.Center, lineHeight = 32.sp)
        Row(verticalAlignment = Alignment.CenterVertically) {
            CButton("邀请好友", Modifier.weight(1f), enabled = !loggedIn || link != null) { share() }
            Spacer(Modifier.width(12.dp))
            Box(Modifier.size(44.dp).background(c.line, CircleShape).pressable { if (link != null) { clip.setText(AnnotatedString(link)) } else onLogin() }, contentAlignment = Alignment.Center) { LbIcon(R.drawable.ic_g_copy_dark, 20.dp, c.text) }
        }
        st.links?.code?.let { Text("邀请码 $it", Modifier.padding(top = 10.dp).pressable { clip.setText(AnnotatedString(it)) }, style = lb(FontSize.caption), color = c.sub) }
        st.error?.let { Text(it, Modifier.padding(top = 10.dp), style = lb(FontSize.caption), color = LB.p.down) }
        Spacer(Modifier.height(24.dp))
        CCard {
            Text("数据总览", style = lb(FontSize.title, FontWeight.SemiBold), color = c.text)
            Box { Text(listOf("全部时间", "昨日", "本月")[range] + " ▾", Modifier.padding(vertical = 12.dp).pressable { rangeMenu = true }, style = lb(FontSize.caption), color = c.text)
                androidx.compose.material3.DropdownMenu(rangeMenu, { rangeMenu = false }) { listOf("全部时间", "昨日", "本月").forEachIndexed { i, t -> androidx.compose.material3.DropdownMenuItem({ Text(t) }, { range = i; rangeMenu = false }) } } }
            val asset = st.summary?.payoutAsset ?: "USDT"
            listOf("我的返佣" to "${ReferralLogic.rebate(st.summary, range)} $asset", "交易人数" to st.team.count(ReferralLogic::traded).toString(), "注册人数" to (st.summary?.directs ?: st.team.size).toString()).forEach { (k, v) ->
                Column(Modifier.fillMaxWidth().padding(bottom = 10.dp).background(c.line, RoundedCornerShape(10.dp)).padding(12.dp)) {
                    Text(k, style = lb(FontSize.caption), color = c.sub); Text(if (loggedIn) v else "--", style = lbNum(FontSize.title, FontWeight.Bold), color = c.text)
                }
            }
        }
        Spacer(Modifier.height(16.dp))
        CCard { Text("受邀人列表", style = lb(FontSize.title, FontWeight.SemiBold), color = c.text)
            TableHeader("好友 ID", "注册时间", "返佣比例", "已交易")
            if (st.team.isEmpty()) CState("暂无数据") else st.team.forEach { t -> TableRow(t.uid ?: "--", day(t.joinedAt), t.tier ?: "--", if (ReferralLogic.traded(t)) "是" else "否") } }
        Spacer(Modifier.height(16.dp))
        CCard { Text("我的返佣", style = lb(FontSize.title, FontWeight.SemiBold), color = c.text)
            TableHeader("账户类型", "返佣金额", "交易时间", "返佣时间")
            if (st.commissions.isEmpty()) CState("暂无记录") else st.commissions.forEach { r -> TableRow(when (r.kind?.value) { "futures" -> "合约"; "spot" -> "现货"; else -> "邀请" }, "${r.amount ?: "0"} ${r.asset ?: ""}", r.day ?: "--", day(r.settledAt)) } }
        Spacer(Modifier.height(24.dp))
        Text("邀请步骤", Modifier.fillMaxWidth(), style = lb(FontSize.title, FontWeight.Bold), color = c.text, textAlign = TextAlign.Center)
        Spacer(Modifier.height(12.dp))
        listOf("邀请好友注册" to "使用您的邀请码或链接邀请好友注册", "交易" to "受邀好友进行合约交易", "返佣" to "高额返佣直接发放至您的账户").forEachIndexed { i, (t, d) ->
            Row(Modifier.padding(vertical = 8.dp)) {
                Box(Modifier.size(24.dp).background(c.accent, CircleShape), contentAlignment = Alignment.Center) { Text("${i + 1}", color = Color.Black, fontWeight = FontWeight.Bold) }
                Column(Modifier.padding(start = 12.dp)) { Text(t, style = lb(FontSize.body, FontWeight.SemiBold), color = c.text); Text(d, style = lb(FontSize.caption), color = c.sub) }
            }
        }
        if (!loggedIn) { Spacer(Modifier.height(16.dp)); CButton("登录 / 注册", Modifier.fillMaxWidth(), onClick = onLogin) }
    }
}

@Composable private fun TableHeader(vararg cols: String) = Row(Modifier.fillMaxWidth().padding(top = 12.dp, bottom = 6.dp)) { cols.forEach { Text(it, Modifier.weight(1f), style = lb(FontSize.small), color = CampaignPalette.sub) } }
@Composable private fun TableRow(vararg cols: String) = Row(Modifier.fillMaxWidth().padding(vertical = 6.dp)) { cols.forEach { Text(it, Modifier.weight(1f), style = lb(FontSize.small), color = CampaignPalette.text, maxLines = 2) } }

// ======================= 充值返现 / 交易抽奖 =======================
sealed interface Load<out T> { data object Loading : Load<Nothing>; data class Ok<T>(val v: T) : Load<T>; data class Missing(val msg: String) : Load<Nothing>; data class Err(val msg: String) : Load<Nothing> }
private fun <T> Result<T>.toLoad(): Load<T> = fold({ Load.Ok(it) }, { if (it.serviceMissing()) Load.Missing(it.lbMessage()) else Load.Err(it.lbMessage()) })

@HiltViewModel
class CampaignViewModel @Inject constructor(private val api: LinbitApi, private val tokens: TokenStore, val toast: ToastCenter) : ViewModel() {
    val loggedIn get() = tokens.loggedIn
    var cashback by mutableStateOf<Load<ApiCashbackCampaign>>(Load.Loading); private set
    var lottery by mutableStateOf<Load<ApiLotteryCampaign>>(Load.Loading); private set
    var entries by mutableStateOf<Load<ApiLotteryEntries>>(Load.Loading); private set
    var joining by mutableStateOf<String?>(null); private set
    fun loadCashback() { cashback = Load.Loading; viewModelScope.launch { cashback = runCatching { apiCall { api.depositCashbackCampaign() } }.toLoad().let { l -> if (l is Load.Ok && !l.v.configured) Load.Missing("未配置") else l } } }
    fun join(id: String) { if (joining != null) return; joining = id; viewModelScope.launch {
        runCatching { apiCall { api.joinCashbackTier(id) } }.onSuccess { cashback = Load.Ok(it); toast.show("报名成功") }.onFailure { if ((it as? ApiFailure.Server)?.error?.code == "CAMPAIGN_UNCONFIGURED") { cashback = Load.Missing("活动未开放"); toast.show("活动未开放，报名未提交") } else toast.show(it.lbMessage()) }; joining = null } }
    fun loadLottery() { lottery = Load.Loading; viewModelScope.launch { lottery = runCatching { apiCall { api.tradeLotteryCampaign() } }.toLoad().let { l -> if (l is Load.Ok && !l.v.configured) Load.Missing("未配置") else l } } }
    fun loadEntries(tab: Int) { if (!tokens.loggedIn.value) { entries = Load.Ok(ApiLotteryEntries()); return }; entries = Load.Loading
        viewModelScope.launch { entries = runCatching { apiCall { api.tradeLotteryEntries(if (tab == 0) "pending" else "drawn") } }.toLoad().let { l -> if (l is Load.Ok && !l.v.configured) Load.Missing("未配置") else l } } }
}

@Composable
private fun <T> LoadBox(l: Load<T>, retry: () -> Unit, missingTitle: String, ok: @Composable (T) -> Unit) = when (l) {
    Load.Loading -> MoreLoading()
    is Load.Missing -> CState(missingTitle, "当前没有进行中的活动", "刷新", retry)
    is Load.Err -> CState("加载失败", l.msg, "重试", retry)
    is Load.Ok -> ok(l.v)
}

@Composable
fun CashbackScreen(onBack: () -> Unit, onLogin: () -> Unit, vm: CampaignViewModel = hiltViewModel()) {
    val c = CampaignPalette; val loggedIn by vm.loggedIn.collectAsStateWithLifecycle()
    LaunchedEffect(loggedIn) { vm.loadCashback() }
    var all by remember { mutableStateOf(false) }
    CampaignFrame("充值返现", onBack, vm::loadCashback) {
        LoadBox(vm.cashback, vm::loadCashback, "暂无充值返现活动") { cp ->
            Spacer(Modifier.height(12.dp))
            Box(Modifier.fillMaxWidth(), contentAlignment = Alignment.Center) { LbReferenceIcon(R.drawable.ic_ref_asset_gift, 120.dp) }
            Text(cp.subtitle ?: "受邀用户充值返现", Modifier.fillMaxWidth().padding(top = 12.dp), style = lb(FontSize.caption, FontWeight.SemiBold), color = c.accent, textAlign = TextAlign.Center)
            Text(cp.title ?: "充值返现", Modifier.fillMaxWidth().padding(vertical = 12.dp), fontSize = 24.sp, fontWeight = FontWeight.Bold, color = c.text, textAlign = TextAlign.Center, lineHeight = 32.sp)
            Text(campaignPeriod(cp.startAt, cp.endAt), Modifier.fillMaxWidth(), style = lbNum(FontSize.small), color = c.text, textAlign = TextAlign.Center)
            Spacer(Modifier.height(20.dp))
            Box(Modifier.fillMaxWidth(), contentAlignment = Alignment.Center) {
                Box(Modifier.width(200.dp).height(40.dp).background(c.line, RoundedCornerShape(8.dp)).pressable(enabled = !loggedIn, onClick = onLogin), contentAlignment = Alignment.Center) {
                    Text(if (loggedIn) "已登录" else "登录参与", style = lb(FontSize.caption, FontWeight.SemiBold), color = c.text) } }
            Text("受邀用户专享", Modifier.padding(top = 32.dp, bottom = 12.dp), style = lb(FontSize.title, FontWeight.Bold), color = c.text)
            if (cp.tiers.isEmpty()) CState("暂无可报名档位")
            (if (all) cp.tiers else cp.tiers.take(3)).forEach { t ->
                CCard(Modifier.padding(bottom = 12.dp)) {
                    Text("充值 ${t.depositAmount}，得 ${t.bonusAmount} ${t.asset} 体验金", style = lb(FontSize.body, FontWeight.SemiBold), color = c.text)
                    Text("• 每日合约交易量 ≥ ${t.dailyFuturesVolume} ${t.asset}", Modifier.padding(vertical = 8.dp), style = lb(FontSize.small), color = c.sub)
                    CButton(if (t.joined) "已报名" else "报名", Modifier.fillMaxWidth(), enabled = !t.joined && vm.joining == null) { if (loggedIn) vm.join(t.id) else onLogin() }
                }
            }
            if (cp.tiers.size > 3) Text(if (all) "收起 ↑" else "更多 ↓", Modifier.fillMaxWidth().pressable { all = !all }.padding(8.dp), color = c.text, textAlign = TextAlign.Center)
            if (cp.rules.isNotEmpty()) { Text("活动规则", Modifier.padding(top = 24.dp, bottom = 12.dp), style = lb(FontSize.title, FontWeight.Bold), color = c.text)
                cp.rules.forEachIndexed { i, r -> Row(Modifier.padding(bottom = 10.dp)) { Text("${i + 1}", Modifier.width(20.dp), color = c.accent, style = lb(FontSize.caption)); Text(r, style = lb(FontSize.caption), color = c.sub) } } }
        }
    }
}

@Composable
fun LotteryScreen(onBack: () -> Unit, onLogin: () -> Unit, onFutures: () -> Unit, onReferral: () -> Unit, vm: CampaignViewModel = hiltViewModel()) {
    val c = CampaignPalette; val loggedIn by vm.loggedIn.collectAsStateWithLifecycle()
    var tab by remember { mutableIntStateOf(0) }; var open by remember { mutableStateOf<Int?>(null) }
    LaunchedEffect(loggedIn) { vm.loadLottery() }
    LaunchedEffect(loggedIn, tab) { vm.loadEntries(tab) }
    Box(Modifier.fillMaxSize()) {
    CampaignFrame("交易抽奖", onBack, { vm.loadLottery(); vm.loadEntries(tab) }) {
        LoadBox(vm.lottery, vm::loadLottery, "暂无交易抽奖活动") { lt ->
            Text(lt.title ?: "单笔抽奖，收益加码", Modifier.fillMaxWidth().padding(top = 16.dp), fontSize = 24.sp, fontWeight = FontWeight.Bold, color = c.text, textAlign = TextAlign.Center)
            Box(Modifier.fillMaxWidth().padding(top = 12.dp), contentAlignment = Alignment.Center) { Text(campaignPeriod(lt.startAt, lt.endAt), Modifier.border(1.dp, c.line, RoundedCornerShape(14.dp)).padding(horizontal = 12.dp, vertical = 6.dp), style = lbNum(FontSize.small), color = c.text) }
            CCard(Modifier.padding(top = 16.dp)) {
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceEvenly) { lt.prizes.take(6).forEach { pz -> Column(horizontalAlignment = Alignment.CenterHorizontally) { CoinIcon(pz.asset, 32.dp); Text(pz.amount, style = lbNum(FontSize.small), color = c.text) } } }
                if (lt.prizes.isEmpty()) Text("奖池未配置", color = c.sub, style = lb(FontSize.caption))
                Spacer(Modifier.height(12.dp))
                CButton(if (loggedIn) "可抽奖 ${lt.availableDraws} 次" else "立即报名", Modifier.fillMaxWidth(), enabled = !loggedIn) { onLogin() }
                lt.winners.take(5).forEach { Text("恭喜 $it", Modifier.padding(top = 4.dp), style = lb(FontSize.small), color = c.sub) }
            }
            Row(Modifier.fillMaxWidth().padding(top = 20.dp).background(c.card, RoundedCornerShape(20.dp)).padding(4.dp)) {
                listOf("待开奖订单", "累计抽奖").forEachIndexed { i, t -> Box(Modifier.weight(1f).height(36.dp).background(if (tab == i) Color.White else Color.Transparent, RoundedCornerShape(18.dp)).pressable(scale = 1f) { tab = i }, contentAlignment = Alignment.Center) {
                    Text(t, style = lb(FontSize.caption, FontWeight.SemiBold), color = if (tab == i) Color.Black else c.text) } }
            }
            CCard(Modifier.padding(top = 12.dp)) {
                when (val e = vm.entries) {
                    Load.Loading -> MoreLoading()
                    is Load.Err -> CState("加载失败", e.msg, "重试") { vm.loadEntries(tab) }
                    is Load.Missing -> CState("暂无订单", "交易合约即可获得抽奖机会", "去合约交易", onFutures)
                    is Load.Ok -> if (e.v.items.isEmpty()) CState("暂无订单", "交易合约即可获得抽奖机会", "去合约交易", onFutures)
                        else e.v.items.forEach { it2 -> TableRow(it2.symbol, day(it2.closedAt), it2.prize?.let { p -> "${p.amount} ${p.asset}" } ?: if (it2.status == "pending") "待开奖" else "未中奖") }
                }
            }
            if (lt.rules.isNotEmpty()) { Text("规则", Modifier.padding(top = 24.dp, bottom = 12.dp), style = lb(FontSize.title, FontWeight.Bold), color = c.text)
                lt.rules.forEachIndexed { i, r -> CCard(Modifier.padding(bottom = 8.dp).pressable(scale = 1f) { open = if (open == i) null else i }) {
                    Row(verticalAlignment = Alignment.CenterVertically) { Text("${i + 1}. ${r.title}", Modifier.weight(1f), style = lb(FontSize.caption, FontWeight.SemiBold), color = c.text); Text(if (open == i) "−" else "+", color = c.text, fontSize = 18.sp) }
                    if (open == i) Text(r.body, Modifier.padding(top = 8.dp), style = lb(FontSize.small), color = c.sub) } } }
        }
    }
    Column(Modifier.align(Alignment.CenterEnd).padding(end = 12.dp).background(c.card, RoundedCornerShape(8.dp)).border(1.dp, c.accent, RoundedCornerShape(8.dp)).pressable(onClick = onReferral).padding(8.dp), horizontalAlignment = Alignment.CenterHorizontally) {
        LbReferenceIcon(R.drawable.ic_ref_asset_gift, 28.dp); Text("邀请", style = lb(FontSize.small), color = c.text) }
    }
}

// ======================= API Key =======================
@HiltViewModel
class ApiKeyViewModel @Inject constructor(private val api: LinbitApi, val toast: ToastCenter) : ViewModel() {
    var keys by mutableStateOf<Load<ApiKeyList>>(Load.Loading); private set
    var created by mutableStateOf<ApiKeyCreated?>(null)
    var busy by mutableStateOf(false); private set
    fun load() { keys = Load.Loading; viewModelScope.launch { keys = runCatching { apiCall { api.apiKeys() } }.toLoad() } }
    var createError by mutableStateOf<String?>(null)
    var needTwoFa by mutableStateOf(false)
    fun create(label: String, ips: List<String>, totp: String, done: () -> Unit) { if (busy) return; busy = true; createError = null; viewModelScope.launch {
        runCatching { apiCall { api.createApiKey(ApiKeyCreateRequest(label, listOf("read"), ips, totp)) } }.onSuccess { created = it; done(); load() }
            .onFailure { if ((it as? ApiFailure.Server)?.error?.code == "TWOFA_REQUIRED") { needTwoFa = true; done() } else createError = apiKeyError(it) }; busy = false } }
    fun delete(id: String) { if (busy) return; busy = true; viewModelScope.launch {
        runCatching { apiCall { api.deleteApiKey(id) } }.onSuccess { r -> toast.show(if (r.revoked) "已删除" else "删除未生效"); load() }.onFailure { toast.show(it.lbMessage()) }; busy = false } }
}

@Composable
fun ApiKeyScreen(onBack: () -> Unit, vm: ApiKeyViewModel = hiltViewModel(), onSecurity: () -> Unit = {}) {
    val p = LB.p; val clip = LocalClipboardManager.current
    var form by remember { mutableStateOf(false) }; var del by remember { mutableStateOf<ApiKeyItem?>(null) }
    LaunchedEffect(Unit) { vm.load() }
    MoreFrame(if ((vm.keys as? Load.Ok)?.v?.items.isNullOrEmpty()) "创建只读 API Key" else "API Key", onBack) {
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(horizontal = Spacing.pageH)) {
            when (val k = vm.keys) {
                Load.Loading -> MoreLoading()
                is Load.Err -> MoreError(k.msg, vm::load)
                is Load.Missing, is Load.Ok -> {
                    val items = (k as? Load.Ok)?.v?.items.orEmpty()
                    if (items.isEmpty()) Column(Modifier.fillMaxWidth().padding(top = 60.dp), horizontalAlignment = Alignment.CenterHorizontally) {
                        LbReferenceIcon(R.drawable.ic_ref_empty_data, 120.dp)
                        Text("创建 API Key，安全管理 API 权限", Modifier.padding(vertical = 16.dp), style = lb(FontSize.body), color = p.textPrimary)
                        if (k is Load.Missing) Text("服务端暂未开放 API Key 管理", Modifier.padding(bottom = 12.dp), style = lb(FontSize.caption), color = p.textSecondary)
                        LbButton("创建只读 API Key", Modifier.width(220.dp), enabled = k is Load.Ok) { form = true }
                    } else {
                        items.forEach { it2 -> Column(Modifier.fillMaxWidth().padding(vertical = 8.dp).border(1.dp, p.border, RoundedCornerShape(Radius.lg)).padding(16.dp)) {
                            Row { Text(it2.label, Modifier.weight(1f), style = lb(FontSize.body, FontWeight.SemiBold), color = p.textPrimary); Text("删除", Modifier.pressable { del = it2 }, color = p.down, style = lb(FontSize.caption)) }
                            Text(it2.apiKey, Modifier.padding(top = 6.dp).pressable { clip.setText(AnnotatedString(it2.apiKey)); vm.toast.show("已复制") }, style = lbNum(FontSize.caption), color = p.textSecondary)
                            Text("权限：只读 · IP：" + (it2.ipWhitelist.joinToString().ifEmpty { "不限制" }) + " · " + day(it2.createdAt), Modifier.padding(top = 6.dp), style = lb(FontSize.small), color = p.textTertiary)
                        } }
                        LbButton(if (items.size >= ApiKeyLimits.MAX_KEYS) "已达上限（最多 ${ApiKeyLimits.MAX_KEYS} 个）" else "创建只读 API Key", Modifier.fillMaxWidth().padding(vertical = 16.dp), enabled = items.size < ApiKeyLimits.MAX_KEYS) { form = true }
                    }
                }
            }
        }
    }
    if (form) { var label by remember { mutableStateOf("") }; var ips by remember { mutableStateOf("") }; var totp by remember { mutableStateOf("") }
        val ipList = ips.split(',', '，', '\n').map { it.trim() }.filter { it.isNotEmpty() }
        AlertDialog(onDismissRequest = { form = false }, title = { Text("创建只读 API Key") }, text = { Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
            androidx.compose.material3.OutlinedTextField(label, { label = it.take(20) }, label = { Text("备注名") }, singleLine = true)
            androidx.compose.material3.OutlinedTextField(ips, { ips = it }, label = { Text("IP 白名单（选填，逗号分隔）") })
            if (ipList.size > ApiKeyLimits.MAX_IPS) Text("IP 白名单最多 ${ApiKeyLimits.MAX_IPS} 条", style = lb(FontSize.small), color = p.down)
            androidx.compose.material3.OutlinedTextField(totp, { totp = it.filter(Char::isDigit).take(6) }, label = { Text("谷歌验证码") }, singleLine = true, keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(keyboardType = androidx.compose.ui.text.input.KeyboardType.NumberPassword))
            vm.createError?.let { Text(it, style = lb(FontSize.small), color = p.down) }
            Text("只读 Key 仅可查询资产和订单，不能交易、划转或提现。", style = lb(FontSize.small), color = p.textSecondary) } },
            confirmButton = { TextButton(enabled = label.isNotBlank() && totp.length == 6 && ipList.size <= ApiKeyLimits.MAX_IPS && !vm.busy, onClick = { vm.create(label.trim(), ipList, totp) { form = false } }) { Text("创建") } },
            dismissButton = { TextButton(onClick = { form = false; vm.createError = null }) { Text("取消") } }) }
    if (vm.needTwoFa) AlertDialog(onDismissRequest = { vm.needTwoFa = false }, title = { Text("需要开启谷歌验证") }, text = { Text("创建 API Key 前须先开启谷歌验证（TOTP）。") },
        confirmButton = { TextButton(onClick = { vm.needTwoFa = false; onSecurity() }) { Text("去开启") } }, dismissButton = { TextButton(onClick = { vm.needTwoFa = false }) { Text("取消") } })
    vm.created?.let { cr -> AlertDialog(onDismissRequest = {}, title = { Text("请保存您的 Secret Key") }, text = { Column { Text("Secret Key 只显示这一次，关闭后无法再次查看。", color = p.down); Spacer(Modifier.height(8.dp)); Text("API Key：" + cr.item.apiKey); Text("Secret Key：" + cr.secret) } },
        confirmButton = { TextButton(onClick = { clip.setText(AnnotatedString(cr.secret)); vm.toast.show("已复制 Secret Key") }) { Text("复制") } }, dismissButton = { TextButton(onClick = { vm.created = null }) { Text("我已保存") } }) }
    del?.let { d -> AlertDialog(onDismissRequest = { del = null }, title = { Text("删除 API Key") }, text = { Text("删除「${d.label}」后，使用该 Key 的程序将立即失效。") },
        confirmButton = { TextButton(onClick = { vm.delete(d.id); del = null }) { Text("删除", color = p.down) } }, dismissButton = { TextButton(onClick = { del = null }) { Text("取消") } }) }
}

// ======================= 卡片（/cards/*，契约 §3） =======================
@Serializable data class ApiCardIssuer(val legalName: String? = null, val registrationRef: String? = null)
@Serializable data class ApiCardProductInfo(val name: String? = null, val network: String? = null, val currency: String? = null, val fees: List<String> = emptyList(), val regions: List<String> = emptyList(), val termsUrl: String? = null)
@Serializable data class ApiCardApplicationInfo(val id: String? = null, val status: String? = null, val createdAt: String? = null)
@Serializable data class ApiCardState(val configured: Boolean = false, val submitted: Boolean = false, val status: String = "unconfigured", val product: ApiCardProductInfo? = null, val issuer: ApiCardIssuer? = null, val application: ApiCardApplicationInfo? = null)
@Serializable data class ApiCardApplyRequest(val agreeTerms: Boolean = true)

@HiltViewModel
class CardViewModel @Inject constructor(private val api: LinbitApi, private val tokens: TokenStore, val toast: ToastCenter) : ViewModel() {
    val loggedIn get() = tokens.loggedIn
    var product by mutableStateOf<Load<ApiCardState>>(Load.Loading); private set
    var application by mutableStateOf<ApiCardState?>(null); private set
    var busy by mutableStateOf(false); private set
    fun load() { product = Load.Loading; viewModelScope.launch {
        product = runCatching { apiCall { api.cardProduct() } }.toLoad()
        if (tokens.loggedIn.value) application = runCatching { apiCall { api.cardApplication() } }.getOrNull() } }
    fun apply() { if (busy) return; busy = true; viewModelScope.launch {
        runCatching { apiCall { api.applyCard(ApiCardApplyRequest()) } }
            .onSuccess { application = it; toast.show(if (it.submitted) "申请已提交，等待审核" else "申请未提交") }
            .onFailure { toast.show(if (it is ApiFailure.Server && it.error?.code == "CARD_UNCONFIGURED") "卡片产品尚未开通，申请未提交" else it.lbMessage()) }
        busy = false } }
}

@Composable
fun CardScreen(onBack: () -> Unit, onLogin: () -> Unit, vm: CardViewModel = hiltViewModel()) {
    val p = LB.p; val loggedIn by vm.loggedIn.collectAsStateWithLifecycle()
    val ctx = LocalContext.current
    LaunchedEffect(loggedIn) { vm.load() }
    MoreFrame("Linbit 卡", onBack) {
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(horizontal = Spacing.pageH)) {
            when (val l = vm.product) {
                Load.Loading -> MoreLoading()
                is Load.Err -> MoreError(l.msg, vm::load)
                is Load.Missing -> EmptyState("卡片服务暂未开通", Modifier.padding(top = 60.dp))
                is Load.Ok -> {
                    val s = l.v
                    Box(Modifier.fillMaxWidth().padding(top = 16.dp).height(190.dp).background(androidx.compose.ui.graphics.Brush.linearGradient(listOf(Color(0xFF1B1D22), Color(0xFF3A3F47))), RoundedCornerShape(16.dp)).padding(20.dp)) {
                        Text("Linbit", style = lb(FontSize.title, FontWeight.Bold), color = Color.White)
                        Text(s.product?.network ?: "", Modifier.align(Alignment.BottomEnd), style = lb(FontSize.body, FontWeight.SemiBold), color = Color.White)
                        Text(s.product?.name ?: "加密货币卡", Modifier.align(Alignment.BottomStart), style = lb(FontSize.caption), color = Color(0xFFBFC3C9))
                    }
                    if (!s.configured) {
                        EmptyState("卡片产品尚未开通", Modifier.padding(top = 24.dp)) { Text("发行方信息尚未审核通过，暂不接受申请。", style = lb(FontSize.caption), color = p.textSecondary) }
                    } else {
                        Text("产品信息", Modifier.padding(top = 24.dp, bottom = 8.dp), style = lb(FontSize.title, FontWeight.SemiBold), color = p.textPrimary)
                        s.issuer?.legalName?.let { KvRow("发行方", it) }
                        s.issuer?.registrationRef?.let { KvRow("登记编号", it) }
                        s.product?.currency?.let { KvRow("结算币种", it) }
                        if (s.product?.regions?.isNotEmpty() == true) KvRow("支持地区", s.product.regions.joinToString("、"))
                        s.product?.fees?.forEach { Text("• $it", Modifier.padding(vertical = 2.dp), style = lb(FontSize.caption), color = p.textSecondary) }
                        s.product?.termsUrl?.let { u -> Text("查看卡片条款", Modifier.padding(vertical = 8.dp).pressable { runCatching { ctx.startActivity(Intent(Intent.ACTION_VIEW, android.net.Uri.parse(u))) } }, color = p.brand, style = lb(FontSize.caption)) }
                        val app = vm.application?.application
                        Spacer(Modifier.height(20.dp))
                        when {
                            !loggedIn -> LbButton("登录后申请", Modifier.fillMaxWidth(), onClick = onLogin)
                            app?.status != null -> KvRow("申请状态", when (app.status) { "submitted" -> "审核中"; "approved" -> "已通过"; "rejected" -> "未通过"; else -> app.status })
                            else -> LbButton("立即申请", Modifier.fillMaxWidth(), loading = vm.busy, onClick = vm::apply)
                        }
                    }
                }
            }
        }
    }
}
