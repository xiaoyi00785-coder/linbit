package click.cowa.linbit.feature.markets

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import click.cowa.linbit.R
import click.cowa.linbit.core.generated.*
import click.cowa.linbit.core.network.*
import click.cowa.linbit.core.realtime.MarketSocket
import click.cowa.linbit.ui.components.*
import click.cowa.linbit.ui.theme.*
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import kotlinx.serialization.json.*
import java.util.UUID
import javax.inject.Inject

/** 聊天室（契约 §1）：游客可读，登录可发；实时走 /ws chat:{room}。 */
@HiltViewModel
class ChatViewModel @Inject constructor(private val api: LinbitApi, private val socket: MarketSocket, private val tokens: TokenStore, val toast: ToastCenter) : ViewModel() {
    val loggedIn get() = tokens.loggedIn
    var rooms by mutableStateOf<List<ApiChatRoom>?>(null); private set
    var roomsError by mutableStateOf<String?>(null); private set
    var room by mutableStateOf<ApiChatRoom?>(null); private set
    var messages by mutableStateOf<List<ApiChatMessage>>(emptyList()); private set
    var hasMore by mutableStateOf(false); private set
    var msgError by mutableStateOf<String?>(null); private set
    var sending by mutableStateOf(false); private set
    private val mineIds = mutableSetOf<Long>()
    private var live: Job? = null

    fun loadRooms() { roomsError = null; viewModelScope.launch {
        runCatching { apiCall { api.chatRooms() } }.onSuccess { rooms = it.rooms.orEmpty() }.onFailure { roomsError = chatError(it) } } }

    fun open(r: ApiChatRoom?) {
        live?.cancel(); room = r; messages = emptyList(); msgError = null
        if (r == null) return
        viewModelScope.launch { runCatching { apiCall { api.chatMessages(r.id) } }
            .onSuccess { messages = it.items.orEmpty().sortedBy { m -> m.id }; hasMore = it.hasMore == true }.onFailure { msgError = chatError(it) } }
        live = viewModelScope.launch { socket.subscribe("chat:${r.id}").collect { f -> runCatching {
            val d = f.data.jsonObject
            when (d["type"]?.jsonPrimitive?.content) {
                "message" -> { val m = Json { ignoreUnknownKeys = true }.decodeFromJsonElement(ApiChatMessage.serializer(), d["message"]!!)
                    if (messages.none { it.id == m.id }) messages = (messages + m).sortedBy { it.id } }
                "delete" -> { val id = d["id"]!!.jsonPrimitive.long; messages = messages.filterNot { it.id == id } }
            } } } }
    }
    fun older() { val r = room ?: return; val before = messages.firstOrNull()?.id ?: return
        viewModelScope.launch { runCatching { apiCall { api.chatMessages(r.id, before = before) } }.onSuccess { messages = (it.items.orEmpty() + messages).distinctBy { m -> m.id }.sortedBy { m -> m.id }; hasMore = it.hasMore == true } } }
    fun send(text: String, done: () -> Unit) { val r = room ?: return; if (sending || text.isBlank()) return; sending = true
        viewModelScope.launch { runCatching { apiCall { api.postChatMessage(r.id, ApiPostChatMessageRequest(UUID.randomUUID().toString(), text.trim())) } }
            .onSuccess { m -> mineIds += m.id; if (messages.none { it.id == m.id }) messages = (messages + m).sortedBy { it.id }; done() }
            .onFailure { toast.show(chatError(it)) }; sending = false } }
    fun isMine(m: ApiChatMessage) = m.mine == true || m.id in mineIds
    private fun chatError(t: Throwable): String = when ((t as? ApiFailure.Server)?.error?.code) {
        "FEATURE_DISABLED" -> "聊天室暂未开放"; "CHAT_KYC_REQUIRED" -> "完成身份认证后才能发言"; "CHAT_RATE_LIMITED" -> "发言太频繁，请稍后再试"
        "CHAT_TOO_LONG" -> "消息过长"; "CHAT_LINK_BLOCKED" -> "不允许发送链接"; "CHAT_CONTACT_BLOCKED" -> "不允许发送联系方式"; "CHAT_WORD_BLOCKED" -> "消息包含敏感词"; "CHAT_UNAVAILABLE" -> "聊天室暂时不可用"
        "CHAT_MUTED" -> "您已被禁言"; "CHAT_BANNED" -> "您已被禁止使用聊天室"; else -> t.lbMessage() }
}

@Composable
fun ChatRoomTab(loggedIn: Boolean, onLogin: () -> Unit, onOverlay: (Boolean) -> Unit = {}, vm: ChatViewModel = hiltViewModel()) {
    val p = LB.p
    LaunchedEffect(loggedIn) { vm.loadRooms() }
    val room = vm.room
    LaunchedEffect(room) { onOverlay(room != null) }
    if (room != null) { androidx.activity.compose.BackHandler { vm.open(null) }; ChatRoomScreen(room, loggedIn, onLogin, vm); return }
    var q by remember { mutableStateOf("") }
    Column(Modifier.fillMaxSize()) {
        Row(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
            Row(Modifier.weight(1f).height(36.dp).background(p.fill, RoundedCornerShape(Radius.md)).padding(horizontal = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                LbIcon(R.drawable.ic_search, 16.dp, p.textTertiary); Spacer(Modifier.width(6.dp))
                Box { if (q.isEmpty()) Text("搜索群组名称", style = lb(FontSize.label), color = p.textTertiary)
                    BasicTextField(q, { q = it }, singleLine = true, textStyle = lb(FontSize.label).copy(color = p.textPrimary), cursorBrush = SolidColor(p.textPrimary)) }
            }
        }
        val rooms = vm.rooms
        when {
            vm.roomsError != null -> EmptyState(vm.roomsError!!, Modifier.padding(top = 80.dp)) { ChipButton("重试", onClick = vm::loadRooms) }
            rooms == null -> Box(Modifier.fillMaxWidth().padding(top = 80.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
            else -> { val list = rooms.filter { q.isBlank() || it.name.contains(q, true) }
                if (list.isEmpty()) EmptyState("暂无群组", Modifier.padding(top = 80.dp))
                LazyColumn { items(list, key = { it.id }) { r ->
                    Row(Modifier.fillMaxWidth().pressable(scale = 1f) { vm.open(r) }.padding(horizontal = Spacing.pageH, vertical = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                        Box(Modifier.size(40.dp).background(p.brand.copy(alpha = 0.15f), CircleShape), contentAlignment = Alignment.Center) {
                            if (r.symbol != null) CoinIcon(r.symbol.removeSuffix("USDT"), 28.dp) else LbIcon(R.drawable.ic_bell, 20.dp, p.brand) }
                        Column(Modifier.weight(1f).padding(start = 12.dp)) {
                            Text(r.name, style = lb(FontSize.body, FontWeight.Medium), color = p.textPrimary)
                            Text("${r.online} 人在线", style = lb(FontSize.caption), color = p.textSecondary)
                        }
                    } } }
            }
        }
    }
}

@Composable
private fun ChatRoomScreen(room: ApiChatRoom, loggedIn: Boolean, onLogin: () -> Unit, vm: ChatViewModel) {
    val p = LB.p; val list = rememberLazyListState()
    var text by remember { mutableStateOf("") }
    LaunchedEffect(vm.messages.size) { if (vm.messages.isNotEmpty()) list.animateScrollToItem(vm.messages.size - 1) }
    Column(Modifier.fillMaxSize().background(p.bg).imePadding()) {
        Row(Modifier.fillMaxWidth().height(48.dp).padding(horizontal = 4.dp), verticalAlignment = Alignment.CenterVertically) {
            Box(Modifier.size(48.dp).pressable(scale = 1f) { vm.open(null) }, contentAlignment = Alignment.Center) { LbIcon(R.drawable.ic_arrow_left, 22.dp, p.textPrimary) }
            Text(room.name, Modifier.weight(1f), style = lb(FontSize.title, FontWeight.Medium), color = p.textPrimary, textAlign = androidx.compose.ui.text.style.TextAlign.Center)
            Spacer(Modifier.size(48.dp))
        }
        when {
            vm.msgError != null -> EmptyState(vm.msgError!!, Modifier.weight(1f).padding(top = 80.dp)) { ChipButton("重试") { vm.open(room) } }
            else -> LazyColumn(Modifier.weight(1f), state = list, contentPadding = PaddingValues(Spacing.pageH, 8.dp)) {
                if (vm.hasMore) item { Text("加载更早消息", Modifier.fillMaxWidth().pressable(onClick = vm::older).padding(8.dp), color = p.brand, style = lb(FontSize.caption), textAlign = androidx.compose.ui.text.style.TextAlign.Center) }
                if (vm.messages.isEmpty()) item { EmptyState("暂无消息", Modifier.padding(top = 60.dp)) }
                items(vm.messages, key = { it.id }) { m -> val mine = vm.isMine(m)
                    Row(Modifier.fillMaxWidth().padding(vertical = 6.dp), horizontalArrangement = if (mine) Arrangement.End else Arrangement.Start) {
                        if (!mine) Box(Modifier.size(36.dp).background(p.brand.copy(alpha = 0.15f), CircleShape))
                        Column(Modifier.padding(horizontal = 8.dp).widthIn(max = 260.dp), horizontalAlignment = if (mine) Alignment.End else Alignment.Start) {
                            if (!mine) Text(m.sender, style = lb(FontSize.small), color = p.textTertiary)
                            Text(m.text, Modifier.background(if (mine) p.brand.copy(alpha = 0.2f) else p.fill, RoundedCornerShape(10.dp)).padding(10.dp), style = lb(FontSize.body), color = p.textPrimary)
                        }
                    } }
            }
        }
        Row(Modifier.fillMaxWidth().navigationBarsPadding().padding(Spacing.pageH, 8.dp), verticalAlignment = Alignment.CenterVertically) {
            if (!loggedIn) LbButton("登录后发言", Modifier.fillMaxWidth(), onClick = onLogin)
            else {
                Box(Modifier.weight(1f).heightIn(min = 40.dp).background(p.fill, RoundedCornerShape(20.dp)).padding(horizontal = 14.dp, vertical = 10.dp)) {
                    if (text.isEmpty()) Text("说点什么…", style = lb(FontSize.label), color = p.textTertiary)
                    BasicTextField(text, { text = it.take(500) }, textStyle = lb(FontSize.label).copy(color = p.textPrimary), cursorBrush = SolidColor(p.textPrimary), maxLines = 4)
                }
                Spacer(Modifier.width(8.dp))
                ChipButton("发送", enabled = text.isNotBlank() && !vm.sending) { vm.send(text) { text = "" } }
            }
        }
    }
}
