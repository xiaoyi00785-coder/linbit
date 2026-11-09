package click.cowa.linbit.ui

import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * LbSheet 本体已经是 heightIn(max=680).verticalScroll。弹层内容里再放无界的 verticalScroll / LazyColumn，
 * 打开时测量即抛 IllegalStateException（vc8–vc10「质押借币」、vc4–vc10「我的持仓」闪退根因）。
 * 这里静态扫描所有 LbSheet { ... } 内容：内层滚动容器必须同一行给出有界高度（height / heightIn(max) / weight）。
 */
class LbSheetNestedScrollGuardTest {
    private fun sources(): List<File> {
        val root = listOf(File("src/main/kotlin"), File("app/src/main/kotlin")).first { it.isDirectory }
        return root.walkTopDown().filter { it.isFile && it.extension == "kt" }.toList()
    }

    private fun block(s: String, from: Int, open: Char, close: Char): Int {
        var i = from; var d = 0
        while (i < s.length) { val c = s[i]; if (c == open) d++ else if (c == close) { d--; if (d == 0) return i }; i++ }
        return s.length - 1
    }

    @Test fun noUnboundedScrollInsideLbSheet() {
        val bad = mutableListOf<String>()
        val scroll = Regex("""verticalScroll\(|LazyColumn\(|LazyVerticalGrid\(""")
        sources().forEach { f ->
            val s = f.readText()
            if (f.name == "AuthFlow.kt" && !s.contains("fun LbSheet(")) return@forEach
            Regex("""\bLbSheet\(""").findAll(s).forEach { m ->
                if (s.substring(maxOf(0, m.range.first - 4), m.range.first).endsWith("fun ")) return@forEach
                val argsEnd = block(s, m.range.last, '(', ')')
                var j = argsEnd + 1
                while (j < s.length && s[j].isWhitespace()) j++
                val body = if (j < s.length && s[j] == '{') s.substring(j, block(s, j, '{', '}') + 1) else s.substring(m.range.last, argsEnd)
                scroll.findAll(body).forEach { sm ->
                    val lineStart = body.lastIndexOf('\n', sm.range.first) + 1
                    val prefix = body.substring(lineStart, sm.range.first)
                    val bounded = Regex("""\.height\(|heightIn\(max|\.weight\(|LazyColumn\(Modifier\.height\(""").containsMatchIn(prefix + body.substring(sm.range.first, minOf(body.length, sm.range.first + 40)))
                    if (!bounded) bad += "${f.name}:${s.substring(0, m.range.first).count { it == '\n' } + 1} ${sm.value}"
                }
            }
        }
        assertTrue("LbSheet 内存在无界嵌套滚动（会闪退）：\n" + bad.joinToString("\n"), bad.isEmpty())
    }
}
