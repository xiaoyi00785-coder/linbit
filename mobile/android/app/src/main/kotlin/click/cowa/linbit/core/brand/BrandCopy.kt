package click.cowa.linbit.core.brand

/**
 * 用户可见文案的品牌统一。参考 App 仅用于对齐样式，界面不得出现其名字。
 */
object BrandCopy {
    const val EARN_AGREEMENT = "我已阅读并同意 Linbit 的理财服务协议"

    private val competitor = Regex(
        """Easi\s*Coin|EasiCoin|easicoin|EasiEx|easiex|易币""",
        RegexOption.IGNORE_CASE,
    )

    /** 把竞品名替换为 Linbit；已是 Linbit 的保持不变。 */
    fun sanitize(text: String): String = competitor.replace(text, "Linbit")

    fun containsCompetitor(text: String): Boolean = competitor.containsMatchIn(text)
}
