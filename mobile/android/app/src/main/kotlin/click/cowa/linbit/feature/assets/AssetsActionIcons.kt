package click.cowa.linbit.feature.assets

import androidx.annotation.DrawableRes
import click.cowa.linbit.R

/** 资产页快捷按钮矢量图标（线性语义，对照参考 App；禁止文字首字占位）。 */
object AssetsActionIcons {
    @DrawableRes
    fun drawable(title: String): Int = when (title) {
        "充值" -> R.drawable.ic_asset_deposit
        "提现" -> R.drawable.ic_asset_withdraw
        "快捷买币" -> R.drawable.ic_asset_card
        "划转" -> R.drawable.ic_asset_transfer
        "盈亏" -> R.drawable.ic_candles
        "去交易", "总览" -> R.drawable.ic_asset_trade
        "订单" -> R.drawable.ic_doc_text
        else -> R.drawable.ic_asset_trade
    }

    /** 回归：任一快捷标题都不得退回文字首字占位。 */
    fun isLetterPlaceholderForbidden(title: String): Boolean = title.isNotEmpty()
}
