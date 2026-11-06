package click.cowa.linbit.feature.alerts

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import click.cowa.linbit.core.network.ApiFailure
import click.cowa.linbit.core.network.TokenStore
import click.cowa.linbit.core.network.lbMessage
import click.cowa.linbit.data.paper.*
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.drop
import kotlinx.coroutines.launch
import javax.inject.Inject

data class AlertEditor(val alertId: String? = null, val revision: Int = 0, val symbol: String = "BTCUSDT", val condition: String = "gte", val threshold: String = "", val note: String = "", val enabled: Boolean = true)
data class AlertMutationStatus(val message: String, val retryAfter: Int? = null)

@HiltViewModel
class PriceAlertViewModel @Inject constructor(internal val client: PriceAlertApiClient, val tokens: TokenStore) : ViewModel() {
    var items by mutableStateOf<List<PriceAlert>>(emptyList()); private set
    var page by mutableIntStateOf(0); private set
    var total by mutableIntStateOf(0); private set
    var loading by mutableStateOf(false); private set
    var failure by mutableStateOf<String?>(null); private set
    var retryAfter by mutableStateOf<Int?>(null); private set
    var selected by mutableStateOf<PriceAlert?>(null); private set
    var editor by mutableStateOf<AlertEditor?>(null); private set
    var pendingDelete by mutableStateOf<PriceAlert?>(null); private set
    var mutationBusy by mutableStateOf(false); private set
    var mutationError by mutableStateOf<AlertMutationStatus?>(null); private set
    var successNotice by mutableStateOf<String?>(null); private set
    var retryRemaining by mutableIntStateOf(0); private set
    private var retryUntil = 0L
    var conflictRefreshing by mutableStateOf(false); private set
    internal var clock: () -> Long = System::currentTimeMillis
    private var epoch = 0L
    private var readVersion = 0L
    private var mutationVersion = 0L
    private var inFlight = false
    private var listInFlight = false
    private var pendingCreateId: String? = null
    val featureState: StateFlow<PriceAlertFeatureSnapshot> get() = client.featureState
    private var lastEnabled = false
    init {
        viewModelScope.launch { tokens.sessionVersion.drop(1).collect { epoch++; invalidate(); refresh() } }
        viewModelScope.launch { featureState.collect { state ->
            if (!state.enabled) { lastEnabled = false; invalidate() }
            else if (!lastEnabled && tokens.loggedIn.value) { lastEnabled = true; load() }
        } }
    }
    internal fun generation() = epoch
    fun refresh() {
        invalidate()
        if (tokens.loggedIn.value) viewModelScope.launch { client.refreshFeature() }
    }
    private fun invalidate() {
        readVersion++; mutationVersion++; mutationBusy = inFlight; pendingCreateId = null; items = emptyList(); page = 0; total = 0
        listInFlight = false; loading = false; failure = null; retryAfter = null; retryUntil = 0; retryRemaining = 0
        closeMutation(); successNotice = null; selected = null; conflictRefreshing = false
    }
    fun canRequest() = tokens.loggedIn.value && client.enabled && !inFlight && !listInFlight && clock() >= retryUntil
    fun canSave() = canRequest() && !conflictRefreshing && !inFlight
    fun canDismissMutation() = !inFlight && !conflictRefreshing && tokens.loggedIn.value && client.enabled
    fun updateRetryRemaining() { retryRemaining = if (clock() < retryUntil) ((retryUntil - clock() + 999) / 1000).toInt() else 0 }
    private fun setRateLimit(seconds: Int) {
        if (seconds >= 1) { retryUntil = clock() + seconds * 1000L; updateRetryRemaining() }
    }
    fun retry() { if (canRequest()) load(if (failure != null && items.isNotEmpty()) page + 1 else 1) }
    fun next() { if (page > 0 && items.size < total) load(page + 1) }
    fun load(requestedPage: Int = 1) {
        if (!canRequest()) return
        if (requestedPage < 1 || (page > 0 && requestedPage !in setOf(1, page + 1))) return
        if (page > 0 && requestedPage == page + 1 && items.size >= total) return
        val version = ++readVersion; val generation = epoch
        listInFlight = true; loading = true; failure = null; retryAfter = null
        viewModelScope.launch {
            runCatching { client.list(requestedPage) }.onSuccess { result ->
                if (current(version, generation) && client.enabled) {
                    items = if (requestedPage == 1) result.items else items + result.items
                    page = result.page; total = result.total; selected = if (requestedPage == 1) null else selected
                }
            }.onFailure { error ->
                if (current(version, generation) && tokens.loggedIn.value) {
                    if (error.code() == "FEATURE_DISABLED") client.disable()
                    else { failure = error.lbMessage(); retryAfter = error.retrySeconds()?.also(::setRateLimit) }
                }
            }
            if (current(version, generation)) { listInFlight = false; loading = false }
        }
    }
    fun details(item: PriceAlert) { if (item in items && canRequest()) selected = item }
    fun closeDetails() { selected = null }
    fun openCreate() { if (canSave()) { closeMutation(); pendingCreateId = "android-${java.util.UUID.randomUUID()}"; editor = AlertEditor() } }
    fun openEdit(item: PriceAlert) {
        if (item !in items || !canSave()) return
        closeMutation(); selected = null
        editor = AlertEditor(item.alertId, item.revision, item.symbol, item.condition, item.threshold, item.note.orEmpty(), item.enabled)
    }
    fun editDraft(transform: AlertEditor.() -> AlertEditor) { if (!inFlight) editor?.let { editor = it.transform(); mutationError = null } }
    fun stopAlertEditing() { mutationVersion++; mutationBusy = inFlight; closeMutation(); pendingCreateId = null }
    private fun closeMutation() { editor = null; pendingDelete = null; mutationError = null }
    fun cancelAlertDialogs() { if (canDismissMutation()) stopAlertEditing() }
    fun saveAlert() {
        val draft = editor ?: return
        if (!canSave() || !validate(draft)) return
        if (inFlight) return
        inFlight = true
        val version = ++mutationVersion; val gen = epoch
        mutationBusy = true; mutationError = null; successNotice = null
        viewModelScope.launch {
            runCatching {
                if (draft.alertId == null) client.create(CreatePriceAlertRequest(clientAlertId = pendingCreateId ?: "android-${java.util.UUID.randomUUID()}", symbol = draft.symbol.trim().uppercase(), condition = draft.condition, threshold = draft.threshold.trim(), enabled = draft.enabled, note = draft.note))
                else client.update(draft.alertId, UpdatePriceAlertRequest(revision = draft.revision, threshold = draft.threshold.trim(), condition = draft.condition, enabled = draft.enabled, channels = listOf("in_app"), note = draft.note))
            }.fold({
                    if (mutationIsCurrent(version, gen)) {
                        merge(it); total += if (draft.alertId == null) 1 else 0; if (page == 0) page = 1
                        editor = null; successNotice = if (draft.alertId == null) "已创建价格预警" else "已保存价格预警"
                    }
                }, { handleMutationFailure(it, version, gen, draft) })
            finishMutation(version, gen)
        }
    }
    private fun validate(draft: AlertEditor): Boolean {
        val message = when {
            draft.symbol.isBlank() -> "请输入交易对"
            !validPositiveDecimal(draft.threshold.trim()) -> "请输入大于 0 的 Decimal 字符串阈值"
            draft.condition !in setOf("gte", "lte") || draft.note.length > 120 -> "请复核条件与备注"
            draft.alertId != null && draft.revision < 1 -> "需要服务端 revision"
            else -> null
        }
        if (message != null) mutationError = AlertMutationStatus(message)
        return message == null
    }
    fun askDelete(item: PriceAlert) { if (item in items && canSave()) { closeMutation(); selected = null; pendingDelete = item } }
    fun confirmDelete() {
        val item = pendingDelete ?: return
        if (!canSave()) return
        if (inFlight) return
        inFlight = true
        val version = ++mutationVersion; val gen = epoch
        mutationBusy = true; mutationError = null; successNotice = null
        viewModelScope.launch {
            runCatching { client.delete(item.alertId, item.revision) }.onSuccess {
                if (mutationIsCurrent(version, gen)) {
                    items = items.filterNot { it.alertId == item.alertId }; total = maxOf(0, total - 1)
                    if (items.isEmpty()) page = 0
                    if (selected?.alertId == item.alertId) selected = null
                    pendingDelete = null; successNotice = "已删除价格预警"
                }
            }.onFailure { handleMutationFailure(it, version, gen, null) }
            finishMutation(version, gen)
        }
    }
    private fun finishMutation(version: Long, gen: Long) { inFlight = false; if (version == mutationVersion && gen == epoch) mutationBusy = false }
    private suspend fun handleMutationFailure(error: Throwable, version: Long, gen: Long, draft: AlertEditor?) {
        if (!mutationIsCurrent(version, gen)) return
        val api = error as? ApiFailure
        if (api?.code == "FEATURE_DISABLED") { client.disable(); return }
        if (api?.code == "SESSION_STALE") { closeMutation(); return }
        if (api?.code == "REVISION_CONFLICT" && draft?.alertId != null) {
            conflictRefreshing = true
            val currentVersion = mutationVersion
            val current = runCatching { client.detail(draft.alertId) }.getOrNull()
            if (!mutationIsCurrent(version, gen) || version != currentVersion) return
            conflictRefreshing = false
            // Never infer a revision, and never overwrite the user's draft fields.
            val message = if (current != null) { merge(current); editor = if (current.revision > draft.revision) currentEditorRevision(draft, current.revision) else draft; "已刷新同一预警至 revision ${current.revision}；保留您的编辑，请再次复核后保存" } else "已保留您的编辑；未能找到同id记录，请刷新列表后复核"
            mutationError = AlertMutationStatus(message)
            return
        }
        mutationError = AlertMutationStatus(error.lbMessage(), api?.retryAfterSeconds?.takeIf { api.httpStatus == 429 && it > 0 }?.also { retryAfter = it; setRateLimit(it) })
    }
    private fun currentEditorRevision(draft: AlertEditor, revision: Int): AlertEditor? = if (draft == editor) draft.copy(revision = revision) else editor
    private fun current(version: Long, gen: Long) = readVersion == version && gen == epoch
    private fun mutationIsCurrent(version: Long, gen: Long) = mutationVersion == version && gen == epoch && client.enabled && tokens.loggedIn.value
    private fun merge(item: PriceAlert) { items = listOf(item) + items.filterNot { it.alertId == item.alertId }; if (selected?.alertId == item.alertId) selected = item }
}
private fun Throwable.code() = (this as? ApiFailure)?.code
private fun Throwable.retrySeconds() = (this as? ApiFailure)?.retryAfterSeconds
