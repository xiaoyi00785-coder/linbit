package click.cowa.linbit.feature.trade

import click.cowa.linbit.core.generated.ApiEarnProduct
import click.cowa.linbit.core.network.NetworkFactory
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject

/** Per-item decode so one unknown product shape cannot wipe the catalogue. */
object EarnCatalogue {
    fun itemsFrom(root: JsonElement): List<ApiEarnProduct> {
        val arr = (root as? JsonObject)?.get("items")?.jsonArray ?: return emptyList()
        return arr.mapNotNull { el ->
            runCatching { NetworkFactory.json.decodeFromJsonElement(ApiEarnProduct.serializer(), el) }.getOrNull()
                ?.takeUnless { it.kind == ApiEarnProduct.Kind.UNKNOWN }
        }
    }
}
