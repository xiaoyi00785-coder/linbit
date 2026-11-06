package click.cowa.linbit.data.paper

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

@javax.inject.Singleton
class PriceAlertFeatureGate(private val features: PublicFeatureFlags) {
    private val mutex = Mutex()
    private val state = MutableStateFlow(PriceAlertFeatureSnapshot())
    val snapshot: StateFlow<PriceAlertFeatureSnapshot> = state.asStateFlow()
    fun disable() { state.value = PriceAlertFeatureSnapshot(checked = true, enabled = false) }

    suspend fun refresh(): Boolean = mutex.withLock {
        state.value = PriceAlertFeatureSnapshot()
        runCatching {
            val response = features.load()
            if (!response.isSuccessful) error("Feature-read HTTP ${response.code()}")
            PriceAlertFeatureSnapshot(checked = true, enabled = response.body()?.features?.get("priceAlerts") == true)
        }.onSuccess { state.value = it }.onFailure { state.value = PriceAlertFeatureSnapshot(failure = "价格预警状态暂无法确认，请重试") }
        state.value.enabled
    }
}
data class PriceAlertFeatureSnapshot(val checked: Boolean = false, val enabled: Boolean = false, val failure: String? = null)
class PriceAlertApiClient(private val paper: PaperTradingApi, private val gate: PriceAlertFeatureGate) {
    val featureState: StateFlow<PriceAlertFeatureSnapshot> get() = gate.snapshot
    val enabled: Boolean get() = gate.snapshot.value.enabled
    val featureFailure: String? get() = gate.snapshot.value.failure
    suspend fun refreshFeature(): Boolean = gate.refresh()
    fun disable() = gate.disable()
    // Reuse the stable transport; its unrelated paperTrading gate is not consulted by alert operations.
    private val rawClient = PaperTradingClient(paper, PaperFeatureGate { retrofit2.Response.success(click.cowa.linbit.core.generated.ApiAppFeatures(features = mapOf("paperTrading" to true))) })
    suspend fun list(page: Int, pageSize: Int = 2): PriceAlerts {
        require(enabled)
        val body = rawClient.alerts(page = page, pageSize = pageSize)
        require(body.page == page && body.pageSize == pageSize)
        return body
    }
    suspend fun create(request: CreatePriceAlertRequest): PriceAlert {
        require(enabled)
        return runCatching { rawClient.createAlert(request) }.getOrElse { handleAlertError(it) }
    }
    suspend fun update(id: String, request: UpdatePriceAlertRequest): PriceAlert {
        require(enabled)
        return runCatching { rawClient.updateAlert(id, request) }.getOrElse { handleAlertError(it) }
    }
    suspend fun delete(id: String, revision: Int) {
        require(enabled)
        runCatching { rawClient.deleteAlert(id, revision) }.getOrElse { handleAlertError(it) }
    }
    suspend fun detail(id: String): PriceAlert {
        var page = 1
        while (true) {
            val list = list(page = page, pageSize = 50)
            list.items.singleOrNull { it.alertId == id }?.let { return it }
            if (page * list.pageSize >= list.total || list.items.isEmpty()) break
            page++
        }
        throw IllegalStateException("ALERT_NOT_FOUND")
    }
    private fun <T> handleAlertError(error: Throwable): T {
        if ((error as? click.cowa.linbit.core.network.ApiFailure)?.code == "FEATURE_DISABLED") disable()
        throw error
    }
}
