package click.cowa.linbit.data.paper

import click.cowa.linbit.core.generated.ApiAppFeatures
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import retrofit2.Response

/** Exact ApiAppFeatures map from the existing public GET /app/features, with NO_AUTH handled by LinbitApi. */
fun interface PublicFeatureFlags { suspend fun load(): Response<ApiAppFeatures> }

data class PaperFeatureSnapshot(
    val checked: Boolean = false,
    val enabled: Boolean = false,
    val failure: String? = null,
)

@javax.inject.Singleton
class PaperFeatureGate(private val features: PublicFeatureFlags) {
    private val mutex = Mutex()
    private val state = MutableStateFlow(PaperFeatureSnapshot())
    val snapshot: StateFlow<PaperFeatureSnapshot> = state.asStateFlow()
    val enabled: Boolean get() = state.value.enabled

    suspend fun refresh(): Boolean = mutex.withLock {
        state.value = PaperFeatureSnapshot()
        runCatching {
            val response = features.load()
            if (!response.isSuccessful) error("Feature-read HTTP ${response.code()}")
            val flags = response.body()?.features ?: emptyMap()
            PaperFeatureSnapshot(true, flags["paperTrading"] == true, null)
        }.onSuccess { state.value = it }.onFailure {
            state.value = PaperFeatureSnapshot(failure = "模拟功能状态暂无法确认，请重试")
        }
        state.value.enabled
    }
}
