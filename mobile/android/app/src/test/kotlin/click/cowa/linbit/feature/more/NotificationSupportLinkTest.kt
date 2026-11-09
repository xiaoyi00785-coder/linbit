package click.cowa.linbit.feature.more

import click.cowa.linbit.core.generated.ApiNotification
import click.cowa.linbit.core.generated.ApiNotificationLink
import org.junit.Assert.*
import org.junit.Test

/** 纯内存：正式 link 契约与标已读决策，不发真实通知/工单。 */
class NotificationSupportLinkTest {
    private fun link(type: ApiNotificationLink.Type = ApiNotificationLink.Type.SUPPORT_TICKET, ticketId: String = "42") =
        ApiNotificationLink(type = type, ticketId = ticketId)

    @Test fun onlySupportTicketDigitsAccepted() {
        assertEquals("42", supportTicketId(link()))
        assertEquals("1001", supportTicketId(ApiNotification(id = "n1", body = "工单#999", link = link(ticketId = "1001"))))
        assertNull(supportTicketId(null as ApiNotificationLink?))
        assertNull(supportTicketId(ApiNotification(id = "n2", body = "请查看工单 42", link = null)))
        assertNull(supportTicketId(link(ticketId = "")))
        assertNull(supportTicketId(link(ticketId = "  ")))
        assertNull(supportTicketId(link(ticketId = "42a")))
        assertNull(supportTicketId(link(ticketId = "https://evil.example/42")))
        assertNull(supportTicketId(link(ticketId = "../42")))
        assertNull(supportTicketId(link(ticketId = "42/../1")))
    }

    @Test fun unknownTypeOrBodyNeverInferred() {
        // 生成枚举仅 support_ticket；未知 JSON type 在反序列化层失败，客户端不得用 body 猜。
        val old = ApiNotification(id = "old", title = "工单已回复", body = "您的工单 77 有新回复", link = null)
        assertNull(supportTicketId(old))
        assertNull(supportTicketId(old.copy(body = "ticketId=77")))
    }

    @Test fun markOnlyAfterSuccessfulDetailEntry() {
        val mark = evaluateSupportNotificationMark(
            openTicketId = "42", markNotificationId = "n9", loggedIn = true, alreadyHandled = false,
            detailId = "42", loading = false, error = null, localTicketId = "42",
        )
        assertEquals(SupportNotificationMarkDecision.MarkRead("n9"), mark)
    }

    @Test fun noMarkWhileLoadingOrMissingIds() {
        assertEquals(SupportNotificationMarkDecision.None, evaluateSupportNotificationMark(
            "42", "n9", true, false, null, true, null, "42"))
        assertEquals(SupportNotificationMarkDecision.None, evaluateSupportNotificationMark(
            null, "n9", true, false, "42", false, null, "42"))
        assertEquals(SupportNotificationMarkDecision.None, evaluateSupportNotificationMark(
            "42", null, true, false, "42", false, null, "42"))
        assertEquals(SupportNotificationMarkDecision.None, evaluateSupportNotificationMark(
            "42", "n9", false, false, "42", false, null, "42"))
        assertEquals(SupportNotificationMarkDecision.None, evaluateSupportNotificationMark(
            "42", "n9", true, true, "42", false, null, "42"))
    }

    @Test fun notFoundConsumesWithoutMarking() {
        assertEquals(SupportNotificationMarkDecision.ConsumeWithoutMark, evaluateSupportNotificationMark(
            openTicketId = "42", markNotificationId = "n9", loggedIn = true, alreadyHandled = false,
            detailId = null, loading = false, error = "未找到工单", localTicketId = "42",
        ))
    }

    @Test fun ordinaryTicketOpenHasNoMarkId() {
        assertEquals(SupportNotificationMarkDecision.None, evaluateSupportNotificationMark(
            openTicketId = null, markNotificationId = null, loggedIn = true, alreadyHandled = false,
            detailId = "42", loading = false, error = null, localTicketId = "42",
        ))
    }

    @Test fun staleDetailForOtherTicketDoesNotMark() {
        assertEquals(SupportNotificationMarkDecision.None, evaluateSupportNotificationMark(
            openTicketId = "42", markNotificationId = "n9", loggedIn = true, alreadyHandled = false,
            detailId = "99", loading = false, error = null, localTicketId = "42",
        ))
    }
}
