package click.cowa.linbit.core.network

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/** OpenAPI /earn/products/{id}/subscribe 请求体（生成器未单独产出 schema）。 */
@Serializable
data class ApiEarnSubscribeRequest(
    @SerialName("amount") val amount: String,
)
