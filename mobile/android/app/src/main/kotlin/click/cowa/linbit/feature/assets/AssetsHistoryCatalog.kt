package click.cowa.linbit.feature.assets

/**
 * 资产页右上「历史记录」弹层选项，按当前 tab 给出对应真实入口（对照参考 App 分类结构）。
 * tab 索引与 AssetsScreen.Tabs 一致：0总览 1资金 2合约 3现货 4理财 5策略。
 */
data class AssetsHistoryOption(val title: String, val desc: String, val action: Action) {
    enum class Action {
        DEPOSIT_RECORDS,
        WITHDRAW_RECORDS,
        SPOT_HISTORY,
        FUTURES_HISTORY,
        EARN_ORDERS,
        EARN_RECORDS,
    }
}

object AssetsHistoryCatalog {
    fun optionsForTab(tab: Int): List<AssetsHistoryOption> = when (tab) {
        2 -> listOf(
            AssetsHistoryOption("合约账户记录", "划转、已实现盈亏、资金费用等", AssetsHistoryOption.Action.FUTURES_HISTORY),
            AssetsHistoryOption("交易历史", "永续委托、成交与已实现盈亏", AssetsHistoryOption.Action.FUTURES_HISTORY),
        )
        3 -> listOf(
            AssetsHistoryOption("现货交易历史", "委托、成交明细", AssetsHistoryOption.Action.SPOT_HISTORY),
            AssetsHistoryOption("提现记录", "链上提现申请与状态", AssetsHistoryOption.Action.WITHDRAW_RECORDS),
            AssetsHistoryOption("充值记录", "链上充值到账记录", AssetsHistoryOption.Action.DEPOSIT_RECORDS),
        )
        4 -> listOf(
            AssetsHistoryOption("理财订单", "定期申购、持有中与到期订单", AssetsHistoryOption.Action.EARN_ORDERS),
            AssetsHistoryOption("理财流水", "申购、赎回、利息、到期与解锁", AssetsHistoryOption.Action.EARN_RECORDS),
        )
        else -> listOf(
            AssetsHistoryOption("充值记录", "链上充值到账记录", AssetsHistoryOption.Action.DEPOSIT_RECORDS),
            AssetsHistoryOption("提现记录", "链上提现申请与状态", AssetsHistoryOption.Action.WITHDRAW_RECORDS),
            AssetsHistoryOption("现货交易历史", "委托、成交明细", AssetsHistoryOption.Action.SPOT_HISTORY),
            AssetsHistoryOption("合约交易历史", "永续委托、成交与资金流水", AssetsHistoryOption.Action.FUTURES_HISTORY),
        )
    }
}
