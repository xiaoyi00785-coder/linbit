package click.cowa.linbit.core.network

import kotlinx.coroutines.CancellationException
import kotlinx.serialization.SerializationException
import retrofit2.HttpException
import java.io.IOException

/** 把 Retrofit / OkHttp / 序列化异常统一映射为 [ApiFailure]（协程取消原样抛出）。 */
suspend fun <T> apiCall(block: suspend () -> T): T = try {
    block()
} catch (e: CancellationException) {
    throw e
} catch (e: ApiFailure) {
    throw e
} catch (e: HttpException) {
    val body = try { e.response()?.errorBody()?.string() } catch (_: IOException) { null }
    val retry = e.response()?.headers()?.get("Retry-After")?.trim()?.toIntOrNull()
    throw ApiFailure.Server(e.code(), ErrorEnvelope.parse(body), retry)
} catch (e: IOException) {
    throw ApiFailure.Transport(e)
} catch (e: SerializationException) {
    throw ApiFailure.Decoding(e)
} catch (e: IllegalArgumentException) {
    throw ApiFailure.Decoding(e)
}
