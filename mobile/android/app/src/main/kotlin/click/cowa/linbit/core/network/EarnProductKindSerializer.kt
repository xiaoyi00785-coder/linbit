package click.cowa.linbit.core.network

import click.cowa.linbit.core.generated.ApiEarnProduct
import kotlinx.serialization.KSerializer
import kotlinx.serialization.descriptors.PrimitiveKind
import kotlinx.serialization.descriptors.PrimitiveSerialDescriptor
import kotlinx.serialization.descriptors.SerialDescriptor
import kotlinx.serialization.encoding.Decoder
import kotlinx.serialization.encoding.Encoder

/** Unknown kind strings → UNKNOWN instead of failing the whole /earn/products payload. */
object EarnProductKindSerializer : KSerializer<ApiEarnProduct.Kind> {
    override val descriptor: SerialDescriptor =
        PrimitiveSerialDescriptor("ApiEarnProduct.Kind", PrimitiveKind.STRING)

    override fun deserialize(decoder: Decoder): ApiEarnProduct.Kind {
        val raw = decoder.decodeString()
        return ApiEarnProduct.Kind.entries.firstOrNull { it.value == raw && it != ApiEarnProduct.Kind.UNKNOWN }
            ?: ApiEarnProduct.Kind.UNKNOWN
    }

    override fun serialize(encoder: Encoder, value: ApiEarnProduct.Kind) {
        encoder.encodeString(value.value)
    }
}
