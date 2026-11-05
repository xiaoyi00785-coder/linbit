package click.cowa.linbit.core.realtime

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.filter
import kotlinx.coroutines.flow.onCompletion
import kotlinx.coroutines.flow.onSubscription
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener

/**
 * 行情 / 用户数据 WebSocket 客户端（对应 iOS MarketSocket）。
 * - 单连接多订阅，按频道引用计数：第一个订阅者发 subscribe，最后一个离开发 unsubscribe。
 * - 断线后指数退避重连（1,2,4…30s）并自动重订阅；收到真实帧才重置退避。
 * - 心跳：每 20s 发 {"op":"ping"}；服务端 {"event":"ping"} 回 {"op":"pong"}。
 * - 看门狗：60s 内未收到任何帧即判定断开，主动重连。
 * - 已登录时连上先发 auth（[accessToken]），再订阅 orders/balances 等私有频道。
 */
class MarketSocket(
    private val client: OkHttpClient,
    private val url: String,
    private val userAgent: String,
    private val scope: CoroutineScope,
    private val accessToken: () -> String?,
    private val now: () -> Long = System::currentTimeMillis,
) {
    enum class Phase { IDLE, CONNECTING, OPEN, RECONNECTING }

    data class State(val phase: Phase = Phase.IDLE, val attempt: Int = 0)

    private val lock = Any()
    private val _state = MutableStateFlow(State())
    val state: StateFlow<State> = _state.asStateFlow()

    private val _frames = MutableSharedFlow<WsInbound.Frame>(extraBufferCapacity = 1024)
    val frames: SharedFlow<WsInbound.Frame> = _frames.asSharedFlow()

    private var socket: WebSocket? = null
    private var timers: Job? = null
    private var retryJob: Job? = null
    private var wanted = false
    private var attempt = 0
    private var lastFrameAt = 0L
    private var reqSeq = 0
    private val refCounts = HashMap<String, Int>()

    // ---- 公共 API ----

    fun connect() {
        synchronized(lock) {
            wanted = true
            if (socket != null) return
            open()
        }
    }

    /** 进入后台时调用；回到前台再 [connect]。 */
    fun disconnect() {
        synchronized(lock) {
            wanted = false
            retryJob?.cancel(); retryJob = null
            teardown()
            _state.value = State(Phase.IDLE, 0)
            attempt = 0
        }
    }

    /** 订阅频道，返回该频道的帧流；collect 取消时自动 unsubscribe。channel 例：ticker:BTCUSDT、depth:BTCUSDT、kline:BTCUSDT:15m、orders */
    fun subscribe(channel: String): Flow<WsInbound.Frame> = subscribe(listOf(channel))

    /** 一次订阅多个频道，共用一个收集器（行情列表用，避免 N 个流各自过滤）。 */
    fun subscribe(channels: Collection<String>): Flow<WsInbound.Frame> {
        val set = channels.toSet()
        return frames
            .onSubscription { acquire(set) }
            .filter { it.channel in set }
            .onCompletion { release(set) }
    }

    /** 访问令牌被刷新后调用：重新 auth。 */
    fun reauth() { synchronized(lock) { sendAuthIfNeeded() } }

    // ---- 订阅计数 ----

    private fun acquire(channels: Set<String>) {
        val fresh = ArrayList<String>()
        synchronized(lock) {
            for (c in channels) {
                val n = refCounts.getOrDefault(c, 0)
                refCounts[c] = n + 1
                if (n == 0) fresh += c
            }
            if (fresh.isNotEmpty() && _state.value.phase == Phase.OPEN) send("subscribe", fresh)
        }
        connect()
    }

    private fun release(channels: Set<String>) {
        val gone = ArrayList<String>()
        synchronized(lock) {
            for (c in channels) {
                val n = refCounts.getOrDefault(c, 0)
                if (n <= 1) { refCounts.remove(c); if (n == 1) gone += c } else refCounts[c] = n - 1
            }
            if (gone.isNotEmpty() && _state.value.phase == Phase.OPEN) send("unsubscribe", gone)
        }
    }

    // ---- 连接 ----

    private fun open() {
        _state.value = State(if (attempt == 0) Phase.CONNECTING else Phase.RECONNECTING, attempt)
        val req = Request.Builder().url(url).header("User-Agent", userAgent).build()
        val ws = client.newWebSocket(req, object : WebSocketListener() {
            override fun onOpen(webSocket: WebSocket, response: Response) = onOpened(webSocket)
            override fun onMessage(webSocket: WebSocket, text: String) = onText(webSocket, text)
            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) = onLost(webSocket)
            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) = onLost(webSocket)
        })
        socket = ws
        lastFrameAt = now()
    }

    private fun onOpened(ws: WebSocket) {
        synchronized(lock) {
        if (socket !== ws) return
        lastFrameAt = now()
        _state.value = State(Phase.OPEN, attempt)
        sendAuthIfNeeded()
        if (refCounts.isNotEmpty()) send("subscribe", refCounts.keys.toList())
        startTimers(ws)
        }
    }

    private fun onText(ws: WebSocket, text: String) {
        val msg = WsMessageParser.parse(text)
        synchronized(lock) {
            if (socket !== ws) return
            lastFrameAt = now()
            attempt = 0 // 收到真实帧才重置退避
            if (msg is WsInbound.Event && msg.event == "ping") send("pong", null)
        }
        if (msg is WsInbound.Frame) _frames.tryEmit(msg)
    }

    private fun onLost(ws: WebSocket) {
        synchronized(lock) {
            if (socket !== ws) return
            scheduleReconnect()
        }
    }

    private fun startTimers(ws: WebSocket) {
        timers?.cancel()
        timers = scope.launch {
            var sinceHeartbeat = 0L
            while (isActive) {
                delay(WATCHDOG_TICK_MS)
                sinceHeartbeat += WATCHDOG_TICK_MS
                synchronized(lock) {
                    if (socket !== ws) return@launch
                    if (sinceHeartbeat >= HEARTBEAT_MS) { sinceHeartbeat = 0; send("ping", null) }
                    if (now() - lastFrameAt > IDLE_TIMEOUT_MS) { scheduleReconnect(); return@launch }
                }
            }
        }
    }

    private fun scheduleReconnect() {
        if (!wanted) return
        teardown()
        attempt += 1
        _state.value = State(Phase.RECONNECTING, attempt)
        val delayMs = backoffSeconds(attempt) * 1000L
        retryJob?.cancel()
        retryJob = scope.launch {
            delay(delayMs)
            synchronized(lock) { if (wanted && socket == null) open() }
        }
    }

    private fun teardown() {
        timers?.cancel(); timers = null
        socket?.cancel()
        socket = null
    }

    private fun sendAuthIfNeeded() {
        val token = accessToken() ?: return
        if (_state.value.phase == Phase.OPEN) send("auth", listOf(token))
    }

    private fun send(op: String, args: List<String>?) {
        val ws = socket ?: return
        reqSeq += 1
        if (!ws.send(WsMessageParser.encode(op, args, "android-$reqSeq"))) scheduleReconnect()
    }

    companion object {
        const val HEARTBEAT_MS = 20_000L
        const val IDLE_TIMEOUT_MS = 60_000L
        private const val WATCHDOG_TICK_MS = 5_000L

        /** 重连退避：1,2,4,8,16,30,30…秒。attempt 从 1 开始。 */
        fun backoffSeconds(attempt: Int): Int {
            val a = attempt.coerceIn(1, 31)
            return minOf(1L shl (a - 1), 30L).toInt()
        }
    }
}
