/**
 * Please note: hand-aligned with production GET /loans/market terms objects.
 * OpenAPI historically omitted days/annualRate; those fields are returned by earn service.
 */
@file:Suppress("ArrayInDataClass","EnumEntryName","RemoveRedundantQualifierName","UnusedImport")
package click.cowa.linbit.core.generated

import kotlinx.serialization.Serializable
import kotlinx.serialization.SerialName

@Serializable
data class ApiLoanMarketTermsInner (
    @SerialName(value = "term") val term: kotlin.String? = null,
    @SerialName(value = "hourlyRate") val hourlyRate: kotlin.String? = null,
    @SerialName(value = "status") val status: kotlin.String? = null,
    @SerialName(value = "days") val days: kotlin.Int? = null,
    @SerialName(value = "annualRate") val annualRate: kotlin.String? = null,
    @SerialName(value = "outstanding") val outstanding: kotlin.String? = null,
)
