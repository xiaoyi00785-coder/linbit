package click.cowa.linbit.feature.more

import android.graphics.BitmapFactory
import androidx.activity.compose.BackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
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
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.MultipartBody
import okhttp3.RequestBody.Companion.toRequestBody
import javax.inject.Inject

/** 工单操作状态；所有请求由用户点击触发，无自动创建、回复或关闭。 */
data class SupportState(
    val items: List<ApiSupportTicket> = emptyList(),
    val detail: ApiSupportTicket? = null,
    val loading: Boolean = true,
    val error: String? = null,
    val busy: Boolean = false,
    val cooldown: Int = 0,
    val closedByConflict: Boolean = false,
)

@HiltViewModel
class SupportViewModel @Inject constructor(private val api: LinbitApi, private val tokens: TokenStore, val toast: ToastCenter) : ViewModel() {
    private val _state = MutableStateFlow(SupportState())
    val state: StateFlow<SupportState> = _state
    val loggedIn get() = tokens.loggedIn
    var status: String? = null
    internal var holdEvidence: Boolean = false

    init { viewModelScope.launch { tokens.sessionVersion.collect { if (!holdEvidence) _state.value = SupportState() } } }

    /** 离屏证据：工单列表示例。 */
    internal fun seedEvidenceSupport(items: List<ApiSupportTicket>) {
        holdEvidence = true
        _state.value = SupportState(items = items, loading = false)
    }

    suspend fun downloadAttachment(id: String) = api.supportAttachment(id)

    fun loadList(newStatus: String? = status) {
        if (holdEvidence) return
        status = newStatus
        _state.value = _state.value.copy(loading = true, error = null)
        viewModelScope.launch {
            runCatching { apiCall { api.supportTickets(status) } }
                .onSuccess { _state.value = _state.value.copy(items = it.items, loading = false) }
                .onFailure { _state.value = _state.value.copy(loading = false, error = it.lbMessage()) }
        }
    }

    fun loadDetail(id: String) {
        _state.value = _state.value.copy(loading = true, error = null)
        viewModelScope.launch {
            runCatching { apiCall { api.supportTicket(id) } }
                .onSuccess { _state.value = _state.value.copy(detail = it, loading = false) }
                .onFailure { _state.value = _state.value.copy(loading = false, error = it.lbMessage()) }
        }
    }

    private suspend fun upload(images: List<SupportImage>): List<String> = images.map { image ->
        apiCall {
            api.uploadSupportAttachment(MultipartBody.Part.createFormData(
                "file", image.filename, image.bytes.toRequestBody(image.mime.toMediaType()),
            ))
        }.id
    }

    fun create(category: ApiSupportTicketCategory, subject: String, content: String, images: List<SupportImage>, onCreated: (String) -> Unit) {
        if (_state.value.busy) return
        _state.value = _state.value.copy(busy = true, error = null)
        viewModelScope.launch {
            runCatching {
                val ids = upload(images)
                apiCall { api.createSupportTicket(ApiSupportCreateTicketRequest(category, subject.trim(), content.trim(), ids.takeIf { it.isNotEmpty() })) }
            }.onSuccess {
                toast.show("工单已提交", ToastCenter.Kind.SUCCESS)
                _state.value = _state.value.copy(busy = false, detail = it)
                onCreated(it.id)
            }.onFailure { fail(it) }
        }
    }

    fun reply(id: String, content: String, images: List<SupportImage>, onDone: () -> Unit) {
        if (_state.value.busy) return
        _state.value = _state.value.copy(busy = true, error = null)
        viewModelScope.launch {
            runCatching {
                val ids = upload(images)
                apiCall { api.replySupportTicket(id, ApiSupportReplyTicketRequest(content.trim(), ids.takeIf { it.isNotEmpty() })) }
            }.onSuccess {
                _state.value = _state.value.copy(busy = false, detail = it, closedByConflict = false)
                onDone()
            }.onFailure { fail(it) }
        }
    }

    fun close(id: String) {
        if (_state.value.busy) return
        _state.value = _state.value.copy(busy = true)
        viewModelScope.launch {
            runCatching { apiCall { api.closeSupportTicket(id) } }
                .onSuccess { toast.show("工单已关闭", ToastCenter.Kind.SUCCESS); _state.value = _state.value.copy(busy = false, detail = it) }
                .onFailure { fail(it) }
        }
    }

    private fun fail(t: Throwable) {
        if ((t as? ApiFailure)?.httpStatus == 409) {
            _state.value = _state.value.copy(busy = false, closedByConflict = true, error = null)
            toast.show("工单已关闭，无法继续回复", ToastCenter.Kind.ERROR)
        } else {
            _state.value = _state.value.copy(busy = false, error = t.lbMessage())
        }
        val retry = (t as? ApiFailure)?.retryAfterSeconds
        if ((t as? ApiFailure)?.httpStatus == 429) startCooldown(retry ?: 60)
    }

    private fun startCooldown(seconds: Int) {
        viewModelScope.launch {
            for (left in seconds downTo 1) {
                _state.value = _state.value.copy(cooldown = left)
                delay(1_000)
            }
            _state.value = _state.value.copy(cooldown = 0)
        }
    }
}

@Composable
fun SupportTicketsScreen(onBack: () -> Unit, onLogin: () -> Unit, vm: SupportViewModel = hiltViewModel()) {
    var route by remember { mutableStateOf("list") }
    var ticketId by remember { mutableStateOf<String?>(null) }
    val loggedIn by vm.loggedIn.collectAsStateWithLifecycle()
    if (!loggedIn) {
        MoreFrame("我的工单", onBack) { EmptyState("登录后查看和提交工单", Modifier.weight(1f)) { LbButton("登录", Modifier.width(160.dp), onClick = onLogin) } }
        return
    }
    BackHandler {
        when {
            ticketId != null -> { ticketId = null; route = "list"; vm.loadList() }
            route == "create" -> route = "list"
            else -> onBack()
        }
    }
    when {
        ticketId != null -> SupportTicketDetail(ticketId!!, vm, onBack = { ticketId = null; route = "list"; vm.loadList() })
        route == "create" -> SupportCreate(vm, onBack = { route = "list" }, onCreated = { ticketId = it })
        else -> SupportTicketList(vm, onBack, onCreate = { route = "create" }, onOpen = { ticketId = it })
    }
}

@Composable
private fun SupportTicketList(vm: SupportViewModel, onBack: () -> Unit, onCreate: () -> Unit, onOpen: (String) -> Unit) {
    val s by vm.state.collectAsStateWithLifecycle()
    var tab by remember { mutableIntStateOf(0) }
    val filters = listOf(null to "全部", "open" to "待处理", "replied" to "已回复", "closed" to "已关闭")
    LaunchedEffect(tab) { vm.loadList(filters[tab].first) }
    MoreFrame("我的工单", onBack, trailing = {
        Text("新建", Modifier.pressable(scale = 1f, onClick = onCreate), style = lb(FontSize.label, FontWeight.Medium), color = LB.p.brand)
    }) {
        PrimaryTabs(filters.map { it.second }, tab, { tab = it }, Modifier.padding(horizontal = Spacing.pageH), size = FontSize.label, underline = true)
        when {
            s.loading -> MoreLoading(Modifier.weight(1f))
            s.error != null -> MoreError(s.error ?: "", { vm.loadList() }, Modifier.weight(1f))
            s.items.isEmpty() -> EmptyState("暂无工单", Modifier.weight(1f)) { LbButton("提交工单", Modifier.width(160.dp)) { onCreate() } }
            else -> Column(Modifier.weight(1f).verticalScroll(rememberScrollState())) {
                s.items.forEach { ticket ->
                    Row(Modifier.fillMaxWidth().pressable(scale = 1f) { onOpen(ticket.id) }.padding(horizontal = Spacing.pageH, vertical = 14.dp), verticalAlignment = Alignment.Top) {
                        Box(Modifier.size(36.dp).background(LB.p.fill, RoundedCornerShape(18.dp)), contentAlignment = Alignment.Center) {
                            LbIcon(R.drawable.ic_headset, 17.dp)
                            if (ticket.unread) Box(Modifier.align(Alignment.TopEnd).size(8.dp).background(LB.p.down, RoundedCornerShape(4.dp)))
                        }
                        Spacer(Modifier.width(12.dp))
                        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(5.dp)) {
                            Row {
                                Text(ticket.subject, Modifier.weight(1f), style = lb(FontSize.body, FontWeight.Medium), color = LB.p.textPrimary, maxLines = 1)
                                SupportStatus(ticket.status)
                            }
                            Text("${supportCategory(ticket.category)} · ${formatTime(ticket.lastReplyAt)} · ${ticket.messageCount} 条消息", style = lb(FontSize.small), color = LB.p.textTertiary)
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun SupportCreate(vm: SupportViewModel, onBack: () -> Unit, onCreated: (String) -> Unit) {
    val s by vm.state.collectAsStateWithLifecycle()
    var category by remember { mutableStateOf<ApiSupportTicketCategory?>(null) }
    var subject by remember { mutableStateOf("") }
    var content by remember { mutableStateOf("") }
    var images by remember { mutableStateOf<List<SupportImage>>(emptyList()) }
    var pickError by remember { mutableStateOf<String?>(null) }
    val picker = supportImagePicker(images.size, !s.busy) { result ->
        result.onSuccess { images = images + it; pickError = null }.onFailure { pickError = it.message }
    }
    BackHandler(enabled = s.busy) {}
    MoreFrame("提交工单", onBack) {
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(Spacing.pageH), verticalArrangement = Arrangement.spacedBy(14.dp)) {
            Text("问题类型", style = lb(FontSize.label, FontWeight.Medium), color = LB.p.textPrimary)
            ApiSupportTicketCategory.entries.chunked(3).forEach { row -> Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                row.forEach { c -> LbButton(supportCategory(c), Modifier.weight(1f), kind = if (category == c) ButtonKind.PRIMARY else ButtonKind.SECONDARY, height = 36.dp) { category = c } }
                repeat(3 - row.size) { Spacer(Modifier.weight(1f)) }
            } }
            FieldLabel("标题")
            LbTextField(subject, { subject = it.take(121) }, "简要描述问题（120 字以内）", error = if (subject.length > 120) "标题不能超过 120 字" else null)
            FieldLabel("问题描述")
            LbTextField(content, { content = it.take(5000) }, "请详细描述问题", height = 120.dp)
            Text("${content.length}/5000", style = lbNum(FontSize.small), color = LB.p.textTertiary)
            SupportImageStrip(images, { images = images - it }, pickError, picker)
            if (s.error != null) Text(s.error ?: "", style = lb(FontSize.small), color = LB.p.down)
            val valid = category != null && subject.isNotBlank() && subject.length <= 120 && content.isNotBlank()
            LbButton(if (s.cooldown > 0) "${s.cooldown}s 后可提交" else "提交", enabled = valid && !s.busy && s.cooldown == 0, loading = s.busy) {
                vm.create(category!!, subject, content, images, onCreated)
            }
        }
    }
}

@Composable
private fun SupportTicketDetail(id: String, vm: SupportViewModel, onBack: () -> Unit) {
    val s by vm.state.collectAsStateWithLifecycle()
    var text by remember { mutableStateOf("") }
    var images by remember { mutableStateOf<List<SupportImage>>(emptyList()) }
    var confirmClose by remember { mutableStateOf(false) }
    val picker = supportImagePicker(images.size, !s.busy && s.cooldown == 0) { result ->
        result.onSuccess { images = images + it }.onFailure { vm.toast.show(it.message ?: "无法读取所选图片", ToastCenter.Kind.ERROR) }
    }
    LaunchedEffect(id) { vm.loadDetail(id) }
    BackHandler(enabled = s.busy) {}
    MoreFrame("工单详情", onBack, trailing = {
        if (s.detail != null && s.detail?.status != ApiSupportTicketStatus.CLOSED && !s.closedByConflict)
            Text("关闭工单", Modifier.pressable(scale = 1f) { confirmClose = true }, style = lb(FontSize.label), color = LB.p.textPrimary)
    }) {
        when {
            s.loading && s.detail == null -> MoreLoading(Modifier.weight(1f))
            s.detail == null -> MoreError(s.error ?: "", { vm.loadDetail(id) }, Modifier.weight(1f))
            else -> {
                val ticket = s.detail!!
                Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(Spacing.pageH), verticalArrangement = Arrangement.spacedBy(14.dp)) {
                    Row {
                        Text(ticket.subject, Modifier.weight(1f), style = lb(FontSize.title, FontWeight.SemiBold), color = LB.p.textPrimary)
                        SupportStatus(if (s.closedByConflict) ApiSupportTicketStatus.CLOSED else ticket.status)
                    }
                    Text("${supportCategory(ticket.category)} · 工单号 ${ticket.id} · ${formatTime(ticket.createdAt)}", style = lb(FontSize.small), color = LB.p.textTertiary)
                    ticket.messages.orEmpty().forEach { message ->
                        val mine = message.author == ApiSupportTicketMessage.Author.USER
                        Column(Modifier.fillMaxWidth(), horizontalAlignment = if (mine) Alignment.End else Alignment.Start) {
                            Text(if (mine) "我" else message.authorName ?: "客服", style = lb(FontSize.small), color = LB.p.textTertiary)
                            Column(Modifier.padding(top = 4.dp).background(if (mine) LB.p.primary else LB.p.fill, RoundedCornerShape(Radius.lg)).padding(10.dp)) {
                                Text(message.body, style = lb(FontSize.label), color = if (mine) LB.p.textOnPrimary else LB.p.textPrimary)
                                if (message.attachments.isNotEmpty()) Row(Modifier.padding(top = 8.dp), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                                    message.attachments.forEach { SupportAttachmentImage(it.id, 72, vm) }
                                }
                            }
                            Text(formatTime(message.createdAt), Modifier.padding(top = 4.dp), style = lbNum(11.sp), color = LB.p.textTertiary)
                        }
                    }
                }
                if (ticket.status == ApiSupportTicketStatus.CLOSED || s.closedByConflict) {
                    Box(Modifier.padding(Spacing.pageH)) { Text("工单已关闭，无法继续回复。如仍有问题，请重新提交工单。", style = lb(FontSize.caption), color = LB.p.textSecondary) }
                } else Column(Modifier.padding(Spacing.pageH), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    SupportImageStrip(images, { images = images - it }, null, picker)
                    LbTextField(text, { text = it.take(5000) }, "输入回复内容")
                    if (s.error != null) Text(s.error ?: "", style = lb(FontSize.small), color = LB.p.down)
                    LbButton(if (s.cooldown > 0) "${s.cooldown}s" else "发送", enabled = text.isNotBlank() && !s.busy && s.cooldown == 0, loading = s.busy) {
                        vm.reply(id, text, images) { text = ""; images = emptyList() }
                    }
                }
            }
        }
    }
    if (confirmClose) AlertDialog(
        onDismissRequest = { confirmClose = false }, title = { Text("关闭工单") },
        text = { Text("关闭后将无法继续回复，如仍有问题可重新提交工单。") },
        confirmButton = { TextButton(onClick = { confirmClose = false; vm.close(id) }) { Text("关闭", color = LB.p.down) } },
        dismissButton = { TextButton(onClick = { confirmClose = false }) { Text("取消") } },
    )
}

@Composable
private fun supportImagePicker(count: Int, enabled: Boolean, onResult: (Result<SupportImage>) -> Unit): () -> Unit {
    val context = LocalContext.current
    var latest by remember { mutableStateOf<(Result<SupportImage>) -> Unit>({}) }
    latest = onResult
    val launcher = rememberLauncherForActivityResult(ActivityResultContracts.PickMultipleVisualMedia((SupportImageProcessor.MAX_COUNT - count).coerceAtLeast(1))) { uris ->
        uris.take(SupportImageProcessor.MAX_COUNT - count).forEach { uri ->
            // 启动协程逐个读取并去元数据，不持有图片权限。
            kotlinx.coroutines.CoroutineScope(kotlinx.coroutines.Dispatchers.Main).launch {
                latest(SupportImageProcessor.read(context, uri))
            }
        }
    }
    return { if (enabled) launcher.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly)) }
}

@Composable
private fun SupportImageStrip(images: List<SupportImage>, onRemove: (SupportImage) -> Unit, error: String?, picker: () -> Unit) {
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            images.forEach { image ->
                val bitmap = remember(image) { BitmapFactory.decodeByteArray(image.bytes, 0, image.bytes.size) }
                Box(Modifier.size(64.dp)) {
                    if (bitmap != null) Image(bitmap.asImageBitmap(), null, Modifier.size(64.dp))
                    Text("×", Modifier.align(Alignment.TopEnd).pressable(scale = 1f) { onRemove(image) }, color = LB.p.down)
                }
            }
            if (images.size < SupportImageProcessor.MAX_COUNT) Box(Modifier.size(64.dp).background(LB.p.fill, RoundedCornerShape(Radius.md)).pressable(scale = 1f, onClick = picker), contentAlignment = Alignment.Center) {
                Text("+ ${images.size}/${SupportImageProcessor.MAX_COUNT}", style = lb(FontSize.small), color = LB.p.textSecondary)
            }
        }
        Text("支持 JPG / PNG / WEBP，单张 ≤ 5MB，最多 3 张；上传前移除位置等元数据", style = lb(FontSize.small), color = LB.p.textTertiary)
        if (error != null) Text(error, style = lb(FontSize.small), color = LB.p.down)
    }
}

@Composable
private fun SupportAttachmentImage(id: String, size: Int, vm: SupportViewModel = hiltViewModel()) {
    var bitmap by remember(id) { mutableStateOf<android.graphics.Bitmap?>(null) }
    LaunchedEffect(id) {
        runCatching {
            val bytes = apiCall { vm.downloadAttachment(id) }.use { it.bytes() }
            bitmap = BitmapFactory.decodeByteArray(bytes, 0, bytes.size)
        }
    }
    Box(Modifier.size(size.dp).background(LB.p.fillStrong, RoundedCornerShape(Radius.md)), contentAlignment = Alignment.Center) {
        if (bitmap != null) Image(bitmap!!.asImageBitmap(), null, Modifier.fillMaxSize()) else LbIcon(R.drawable.ic_doc_text, 24.dp, LB.p.textTertiary)
    }
}

fun supportCategory(c: ApiSupportTicketCategory): String = when (c) {
    ApiSupportTicketCategory.ACCOUNT -> "账户"; ApiSupportTicketCategory.DEPOSIT_WITHDRAW -> "充值提现"
    ApiSupportTicketCategory.TRADE -> "交易"; ApiSupportTicketCategory.KYC -> "身份认证"
    ApiSupportTicketCategory.SECURITY -> "账户安全"; ApiSupportTicketCategory.OTHER -> "其他"
}

@Composable
private fun SupportStatus(status: ApiSupportTicketStatus) {
    SmallTag(when (status) { ApiSupportTicketStatus.OPEN -> "待处理"; ApiSupportTicketStatus.REPLIED -> "客服已回复"; ApiSupportTicketStatus.CLOSED -> "已关闭" }, if (status == ApiSupportTicketStatus.REPLIED) LB.p.up else LB.p.textSecondary)
}
