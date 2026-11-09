package click.cowa.linbit.feature.more

import click.cowa.linbit.core.generated.ApiNotification
import click.cowa.linbit.core.generated.ApiNotificationLink

/**
 * 正式契约：仅 `type=support_ticket` 且 `ticketId` 为非空数字字符串时才可打开工单。
 * 无 link、未知 type、外链或正文工单号一律无效，不得猜测。
 */
internal fun supportTicketId(link: ApiNotificationLink?): String? {
    if (link == null) return null
    if (link.type != ApiNotificationLink.Type.SUPPORT_TICKET) return null
    val id = link.ticketId.trim()
    if (id.isEmpty() || id.any { !it.isDigit() }) return null
    return id
}

internal fun supportTicketId(item: ApiNotification): String? = supportTicketId(item.link)

/** 系统通知→工单：仅在详情成功进入后才标该通知；404/未登录不标；普通打开工单无 mark id。 */
internal sealed class SupportNotificationMarkDecision {
    data object None : SupportNotificationMarkDecision()
    data class MarkRead(val notificationId: String) : SupportNotificationMarkDecision()
    /** 打开意图失败（404等）：消费导航态，不调用 /notifications/read。 */
    data object ConsumeWithoutMark : SupportNotificationMarkDecision()
}

internal fun evaluateSupportNotificationMark(
    openTicketId: String?,
    markNotificationId: String?,
    loggedIn: Boolean,
    alreadyHandled: Boolean,
    detailId: String?,
    loading: Boolean,
    error: String?,
    localTicketId: String?,
): SupportNotificationMarkDecision {
    val wanted = openTicketId ?: return SupportNotificationMarkDecision.None
    val nid = markNotificationId ?: return SupportNotificationMarkDecision.None
    if (!loggedIn || alreadyHandled || loading) return SupportNotificationMarkDecision.None
    if (detailId == wanted) return SupportNotificationMarkDecision.MarkRead(nid)
    if (localTicketId == wanted && detailId == null && error != null) {
        return SupportNotificationMarkDecision.ConsumeWithoutMark
    }
    return SupportNotificationMarkDecision.None
}
