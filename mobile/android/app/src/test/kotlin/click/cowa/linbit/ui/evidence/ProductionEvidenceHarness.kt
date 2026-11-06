package click.cowa.linbit.ui.evidence

import android.app.Activity
import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.Rect
import android.view.View
import android.view.WindowInsets
import android.view.WindowManager
import androidx.compose.ui.test.junit4.AndroidComposeTestRule
import androidx.test.ext.junit.rules.ActivityScenarioRule
import java.io.File

/** App content bitmaps, not device full-screen captures. Hashes recorded externally by the caller. */
internal class ProductionEvidenceHarness<A : androidx.activity.ComponentActivity>(
    private val rule: AndroidComposeTestRule<ActivityScenarioRule<A>, A>,
) {
    val out = File(System.getProperty("production.evidence.dir") ?: "build/reports/production-evidence").apply { mkdirs() }
    private val expected: Qualifier by lazy {
        val prop = System.getProperty("production.evidence.qualifier") ?: "w=390;h=844;xxhdpi;densityDpi=480;night=no;fontScale=1.0"
        val d = prop.split(';').associate { it.substringBefore('=') to it.substringAfter('=', "") }
        Qualifier(d.getValue("w").toInt(), d.getValue("h").toInt(), d.getValue("densityDpi").toInt(), d["night"] == "yes", d.getValue("fontScale").toFloat())
    }
    data class Qualifier(val width: Int, val height: Int, val density: Int, val dark: Boolean, val fontScale: Float)

    data class Capture(val name: String, val width: Int, val height: Int, val insets: String, val window: Boolean)

    fun app(name: String, requiredWidthPx: Int? = null, requiredHeightPx: Int? = null): Capture {
        val cfg = rule.activity.resources.configuration
        check(cfg.uiMode and android.content.res.Configuration.UI_MODE_NIGHT_MASK == if (expected.dark) android.content.res.Configuration.UI_MODE_NIGHT_YES else android.content.res.Configuration.UI_MODE_NIGHT_NO) { "Night mode must match this run" }
        check(cfg.screenWidthDp == expected.width && cfg.screenHeightDp == expected.height && cfg.densityDpi == expected.density && cfg.fontScale == expected.fontScale) { "Expected $expected got ${cfg.screenWidthDp}x${cfg.screenHeightDp} / ${cfg.densityDpi} / ${cfg.fontScale}" }
        lateinit var result: Capture
        rule.runOnIdle {
            val view = rule.activity.findViewById<View>(android.R.id.content)
            check(view.width == requiredWidthPx ?: view.width) { "Unexpected content width ${view.width}" }
            check(view.height == requiredHeightPx ?: view.height) { "Unexpected content height ${view.height}" }
            check(view.width > 0 && view.height > 0) { "Empty app content rectangle" }
            val decor = rule.activity.window.decorView
            val rect = android.graphics.Rect(); view.getGlobalVisibleRect(rect)
            check(rect.width() == view.width && rect.height() == view.height) { "Clipped app content: $rect vs ${view.width}x${view.height}" }
            val full = Bitmap.createBitmap(decor.width, decor.height, Bitmap.Config.ARGB_8888)
            decor.draw(Canvas(full))
            check(rect.left >= 0 && rect.top >= 0 && rect.right <= full.width && rect.bottom <= full.height) { "App content outside real window: $rect; decor=${decor.width}x${decor.height}" }
            val bitmap = Bitmap.createBitmap(full, rect.left, rect.top, rect.width(), rect.height())

            save(name, bitmap)
            val nav = Rect(); @Suppress("DEPRECATION") rule.activity.windowManager.defaultDisplay.getRectSize(nav)
            result = Capture(name, bitmap.width, bitmap.height,
                "rootViewMeasured=${view.width}x${view.height}px; navigationFrame=$nav; statusBars=${view.rootWindowInsets?.getInsets(WindowInsets.Type.statusBars())}; navigationBars=${view.rootWindowInsets?.getInsets(WindowInsets.Type.navigationBars())}; ime=${view.rootWindowInsets?.getInsets(WindowInsets.Type.ime())}; appContentOnly=true", false)
        }
        writeManifest(result)
        return result
    }

    /** Fail closed when Robolectric cannot expose a true dialog window; no inline substitute. */
    fun dialogWindow(name: String, productionWindow: android.view.Window?): Capture {
        lateinit var result: Capture
        rule.waitForIdle()
        rule.runOnIdle {
            val window = productionWindow ?: throw AssertionError("No real production dialog window; use instrumentation instead")
            check(window !== rule.activity.window) { "Cannot substitute the app window for the dialog" }
            val dialog = window.decorView
            check(dialog.isShown && dialog.width > 0 && dialog.height > 0) { "Empty detached dialog rectangle; use instrumentation" }
            val bitmap = Bitmap.createBitmap(dialog.width, dialog.height, Bitmap.Config.ARGB_8888)
            dialog.draw(Canvas(bitmap))
            save(name, bitmap)
            result = Capture(name, bitmap.width, bitmap.height, "realDialogWindow=true; dialogDecorViewMeasured=${dialog.width}x${dialog.height}px; pixelCopy=NOT_RUN; independent Dialog Canvas capture is not instrumentation; systemBars=${dialog.rootWindowInsets?.getInsets(WindowInsets.Type.systemBars())}", true)
        }
        writeManifest(result)
        return result
    }

    private fun save(name: String, bitmap: Bitmap) {
        check(name.matches(Regex("[a-z0-9-]+"))) { "Unsafe evidence name $name" }
        File(out, "$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    private fun writeManifest(c: Capture) {
        check(c.width > 0 && c.height > 0)
        val png = File(out, "${c.name}.png")
        check(png.isFile && png.length() > 0)
        val root = rule.activity.findViewById<View>(android.R.id.content)
        val cfg = root.resources.configuration
        val rectangle = android.graphics.Rect(); root.getWindowVisibleDisplayFrame(rectangle)
        val entries = when {
            c.name.startsWith("auth-") -> "AuthHost(AuthFlow,LocalLbSheetInlineEvidence=false)"
            c.name.startsWith("deposit-") || c.name.startsWith("withdraw-") -> "Production AssetsHost/AssetsNav (direct Host; not an AppRoot click-through)"
            c.name.startsWith("security-") -> "ProfileHost/SecurityHost/SecurityPage (real security Host)"
            c.name.startsWith("approot-address-edit-") -> "Verified production AppRoot HOME -> Assets -> Withdraw choice -> AddressBook card edit -> label/memo form -> true final Window -> exact loopback PATCH -> same-id/value list readback"
            c.name.startsWith("approot-address-") -> "Verified production AppRoot HOME -> Assets -> Withdraw choice -> Address book -> AddAddress -> true final review Window -> exact loopback create -> list readback"
            c.name.startsWith("approot-withdraw-") -> "Verified production AppRoot HOME -> Assets -> Withdraw form -> real security-review Window -> exact loopback withdrawal -> submitted/readback status and debit-balance evidence; not blockchain settlement"
            c.name.startsWith("approot-transfer-") -> "Verified production AppRoot HOME -> Assets tab -> Transfer form -> true LbSheet review -> exact loopback futures transfer -> actual server ref/status and balance readback"
            c.name.startsWith("approot-algo-") -> "Verified production AppRoot HOME -> futures bottom tab -> visible clock touch -> History; real Modal Window; loopback TPSL cancel review/readback"
            c.name.startsWith("approot-") -> "Full production AppRoot: HOME -> ASSETS Tab -> actual feature click -> AssetFeatureHost (showSplash=false)"
            c.name.startsWith("history-") -> "Production FuturesHistoryScreen direct Host; NOT yet a successful AppRoot click-through"
            c.name.startsWith("asset-") -> "Direct AssetFeatureHost (actual production Host, not an AppRoot click-through)"
            c.name.startsWith("convert-") -> "Production ConvertSubpage/quote/final-review LbSheet (direct Subpage; not an AppRoot click-through)"
            else -> "UNMAPPED"
        }
        check(entries != "UNMAPPED") { "Every evidence PNG must map to its production entry" }
        val ref = when (c.name) {
            "auth-step1-login" -> "ref2/3.jpg"
            "auth-step2-password" -> "ref2/10.jpg"
            "auth-step3-login-verify", "auth-step2-register-verify", "auth-step3-forgot-verify" -> "ref2/11.jpg"
            "deposit-normal", "deposit-empty", "deposit-error" -> "none-mapped (ref2/15 is 2FA, not deposit)"
            "withdraw-before-confirm" -> "none-mapped (ref2/20.jpg=add-address form; ref2/21.jpg=address-verification picker, not withdrawal confirmation)"
            "withdraw-empty-error" -> "ref2/22.jpg (internal transfer only; chain form no exact reference)"
            else -> "none-mapped"
        }
        val json = org.json.JSONObject()
            .put("case", c.name).put("productionEntry", entries)
            .put("qualifier", "w${cfg.screenWidthDp}dp;h${cfg.screenHeightDp}dp;night=${cfg.uiMode and android.content.res.Configuration.UI_MODE_NIGHT_MASK};fontScale=${cfg.fontScale};densityDpi=${cfg.densityDpi}")
            .put("captureScope", if (c.window) "dialog-content-only" else "app-content-only")
            .put("bitmapPx", "${c.width}x${c.height}")
            .put("actualAppRootPx", "${root.width}x${root.height}")
            .put("visibleDisplayFrame", rectangle.toShortString())
            .put("insets", c.insets).put("productionDialogWindow", c.window)
            .put("reference", ref).put("deviceFullScreen", false)
            .put("pngBytes", png.length()).put("checksumSha256", sha256(png))
            .put("networkFixture", "loopback, explicit route map; see mock-routes.csv")
            .put("visualVerdict", "PENDING_HUMAN_READ")
            .put("deviceVisualVerdict", "NOT_RUN")
        File(out, "${c.name}.json").writeText(json.toString(2))
    }

    private fun sha256(file: File): String = java.security.MessageDigest.getInstance("SHA-256")
        .digest(file.readBytes()).joinToString("") { "%02x".format(it) }
}
