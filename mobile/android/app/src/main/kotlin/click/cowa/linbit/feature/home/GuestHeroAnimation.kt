package click.cowa.linbit.feature.home

import android.content.Context
import android.provider.Settings
import androidx.compose.foundation.Canvas
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ColorFilter
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.rotate
import androidx.compose.ui.graphics.drawscope.scale
import androidx.compose.ui.graphics.drawscope.translate
import androidx.compose.ui.graphics.painter.Painter
import androidx.compose.ui.layout.boundsInWindow
import androidx.compose.ui.layout.onGloballyPositioned
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.animation.core.withInfiniteAnimationFrameMillis
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import click.cowa.linbit.R
import kotlin.math.PI
import kotlin.math.abs
import kotlin.math.cos
import kotlin.math.max
import kotlin.math.min
import kotlin.math.sin

/**
 * 游客首页主视觉动画（对照 EasiCoin 未登录首页顶部 16:9、约 11.3 秒循环的 3D 金币动图的节奏与版式）。
 *
 * 完全由 Compose Canvas 自绘：Linbit 品牌币（自有 Logo）+ App 内已有的币种图标 + 金属轨道环，
 * 不含任何参考 App 的素材，也不含任何行情数字。
 * 节奏：品牌币升起放大（0–1.1s）→ 币群迸散（0.9–3.1s）→ 双轨道环环绕品牌币、光球绕行（3–8s）
 * → 小币群聚拢悬浮在环形平台上（8–10.4s）→ 轨道收平淡出并衔接下一轮（10.4–11.33s）。
 *
 * 性能：只在「可见 + 生命周期 RESUMED + [active]」时逐帧推进，否则停在当前帧；
 * 时间只在绘制阶段读取，推进帧不会触发重组。系统「移除动画」（动画时长缩放为 0）时显示静止帧。
 */
const val HERO_LOOP_MS = 11_333L

/** 系统关闭动画时显示的静止帧：品牌币 + 环形平台 + 小币群，信息最完整。 */
const val HERO_STATIC_MS = 9_200L

enum class HeroPhase { RISE, BURST, ORBIT, CLUSTER, SETTLE }

/** 动画阶段（纯函数，供测试校验节奏）。 */
fun heroPhaseAt(ms: Long): HeroPhase {
    val t = Math.floorMod(ms, HERO_LOOP_MS)
    return when {
        t < 1_000 -> HeroPhase.RISE
        t < 3_000 -> HeroPhase.BURST
        t < 8_000 -> HeroPhase.ORBIT
        t < 10_400 -> HeroPhase.CLUSTER
        else -> HeroPhase.SETTLE
    }
}

/** 系统开发者选项/无障碍「移除动画」：Animator 时长缩放为 0。 */
fun heroReduceMotion(ctx: Context): Boolean =
    runCatching { Settings.Global.getFloat(ctx.contentResolver, Settings.Global.ANIMATOR_DURATION_SCALE, 1f) == 0f }.getOrDefault(false)

/** 起始帧：减少动画时用静止帧，否则从头播放。 */
fun heroInitialFrame(reduceMotion: Boolean, override: Long?): Long = override ?: if (reduceMotion) HERO_STATIC_MS else 0L

const val HERO_TAG = "home.guest.hero.anim"

@Composable
internal fun GuestHeroAnimation(modifier: Modifier = Modifier, active: Boolean = true, frameOverrideMs: Long? = null) {
    val ctx = LocalContext.current
    val reduceMotion = remember { heroReduceMotion(ctx) }
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    val lc by lifecycle.currentStateFlow.collectAsState()
    var onScreen by remember { mutableStateOf(true) }
    val time = remember { mutableLongStateOf(heroInitialFrame(reduceMotion, frameOverrideMs)) }
    if (frameOverrideMs != null) time.longValue = frameOverrideMs
    val running = frameOverrideMs == null && !reduceMotion && active && onScreen && lc.isAtLeast(Lifecycle.State.RESUMED)
    LaunchedEffect(running) {
        if (!running) return@LaunchedEffect
        val base = time.longValue
        var start = -1L
        while (true) {
            withInfiniteAnimationFrameMillis { now ->
                if (start < 0) start = now
                time.longValue = (base + now - start) % HERO_LOOP_MS
            }
        }
    }
    val art = HeroArt(
        mark = painterResource(R.drawable.ic_mark),
        coins = listOf(
            painterResource(R.drawable.coin_btc), painterResource(R.drawable.coin_eth), painterResource(R.drawable.coin_usdt),
            painterResource(R.drawable.coin_sol), painterResource(R.drawable.coin_xrp), painterResource(R.drawable.coin_usdc),
            painterResource(R.drawable.coin_trx), painterResource(R.drawable.coin_link),
        ),
    )
    Canvas(
        modifier
            .onGloballyPositioned { c -> val b = c.boundsInWindow(); onScreen = b.height > 1f && b.width > 1f }
            .testTag(HERO_TAG)
            .semantics { contentDescription = "Linbit 品牌动画" },
    ) { drawHero(time.longValue / 1000f, art) }
}

internal class HeroArt(val mark: Painter, val coins: List<Painter>)

private val LimeHi = Color(0xFFE2F784)
private val Lime = Color(0xFFABE127)
private val LimeLo = Color(0xFF6F9A12)
private val LimeEdge = Color(0xFF55760C)
private val SilverHi = Color(0xFFFAFBFC)
private val Silver = Color(0xFFCDD1D7)
private val SilverLo = Color(0xFF8A9098)
private val Ink = Color(0xFF15200A)

private fun clamp01(v: Float) = v.coerceIn(0f, 1f)
private fun seg(t: Float, a: Float, b: Float) = clamp01((t - a) / (b - a))
private fun lerp(a: Float, b: Float, f: Float) = a + (b - a) * f
private fun easeOut(f: Float) = 1f - (1f - f) * (1f - f) * (1f - f)
private fun easeIn(f: Float) = f * f * f
private fun easeInOut(f: Float) = if (f < .5f) 4f * f * f * f else 1f - (-2f * f + 2f).let { it * it * it } / 2f

/** 迸散币群的确定性参数（无随机数，帧帧一致）。 */
private class Burst(val angle: Float, val dist: Float, val r: Float, val coin: Int, val spin: Float)
private val BURST = List(18) { i ->
    val k = (i * 37 % 18) / 18f
    Burst(
        angle = (i * 2f * PI.toFloat() / 18f) + (k - .5f) * .3f,
        dist = .32f + .6f * ((i * 53 % 11) / 10f),
        r = .065f + .055f * ((i * 29 % 7) / 6f),
        coin = i % 8,
        spin = 2.5f + 3f * k,
    )
}

private fun DrawScope.drawHero(tRaw: Float, art: HeroArt) {
    val t = tRaw % (HERO_LOOP_MS / 1000f)
    val w = size.width; val h = size.height
    val cx = w / 2f; val cy = h * .5f; val u = h

    // ---------- 1. 品牌币升起、翻转放大（0–1.1s）+ 与上一轮末尾衔接 ----------
    if (t < 1.1f) {
        val rise = easeOut(seg(t, 0f, .6f))
        val zoom = seg(t, .6f, 1.1f)
        val y = lerp(h * .85f, cy, rise)
        val r = if (zoom <= 0f) lerp(.2f * u, .34f * u, rise) else lerp(.34f * u, .95f * u, easeIn(zoom))
        val spin = if (zoom <= 0f) sin(t * 5f) * .35f else lerp(.2f, PI.toFloat() / 2f * .9f, zoom)
        drawBrandCoin(Offset(cx, y), r, spin, 1f - easeIn(zoom), art, bezel = false)
    }

    // ---------- 2. 币群迸散（0.9–3.1s） ----------
    if (t in .9f..3.1f) {
        val s = seg(t, .9f, 3.1f)
        // 中心品牌币：迸散中心，随后缩小成小币飞入下一幕
        val cr = lerp(.17f * u, .07f * u, easeInOut(seg(s, .45f, 1f)))
        val ca = seg(s, 0f, .08f)
        BURST.forEach { b ->
            val travel = easeOut(min(1f, s * 1.15f)) + .45f * easeIn(s)
            val d = b.dist * travel
            val x = cx + cos(b.angle) * d * w * .5f
            val yy = cy + sin(b.angle) * d * h * .62f
            val depth = .75f + .25f * sin(b.angle + 1f)
            val a = seg(s, 0f, .1f) * (1f - seg(s, .72f, 1f))
            drawIconCoin(Offset(x, yy), b.r * u * depth * lerp(.5f, 1.1f, easeOut(min(1f, s * 2f))), t * b.spin, a, art.coins[b.coin])
        }
        drawBrandCoin(Offset(cx, cy), cr, t * 4f, ca, art, bezel = false)
    }

    // ---------- 3–5. 轨道环 + 品牌币（3.0–11.33s） ----------
    if (t >= 3f) {
        val enter = easeOut(seg(t, 3f, 3.7f))
        val settle = easeInOut(seg(t, 8f, 8.9f)) // 收成环形平台
        val out = easeInOut(seg(t, 10.4f, 11.333f)) // 尾声：收平、淡出
        val k = t - 3f
        val ringScale = lerp(2.4f, 1f, enter)
        val alpha = enter * (1f - out)

        val coinY = lerp(cy, cy + .1f * u, settle)
        val aRx = .44f * w * ringScale
        val aTilt = lerp(lerp(.16f + .14f * sin(k * .8f), .2f, settle), .015f, out)
        val aRot = lerp(lerp(-14f * sin(k * .55f), 0f, settle), -10f, out)
        val bRx = lerp(.31f * w, .34f * w, settle) * ringScale
        val bTilt = lerp(lerp(.24f + .12f * cos(k * .7f), .2f, settle), .015f, out)
        val bRot = lerp(lerp(58f + 20f * sin(k * .5f), 0f, settle), -10f, out)
        val bAlpha = seg(t, 3.3f, 3.9f) * (1f - out)
        val ringCenter = Offset(cx, lerp(coinY, coinY + .2f * u, settle))
        val stroke = .03f * u

        // 光球沿外环绕行（4–8s）
        val orbOn = t in 4f..8.2f
        val phi = (t - 4f) * 1.9f
        val orbA = seg(t, 4f, 4.4f) * (1f - seg(t, 7.8f, 8.2f))
        val orbBehind = sin(phi) < 0f

        drawRing(ringCenter, aRx, aRx * aTilt, aRot, stroke, alpha, front = false)
        drawRing(ringCenter, bRx, bRx * bTilt, bRot, stroke * .8f, bAlpha, front = false)
        if (orbOn && orbBehind) drawOrb(ringCenter, aRx, aRx * aTilt, aRot, phi, .04f * u, orbA * alpha)

        val coinR = lerp(.07f * u, .2f * u, easeOut(seg(t, 3f, 3.8f))) * lerp(1f, .82f, settle) * lerp(1f, .3f, out)
        drawBrandCoin(Offset(cx, coinY), coinR, .6f * sin(k * .9f) + if (t > 8f) .35f * sin(t * 1.3f) else 0f, 1f - out, art, bezel = true)

        drawRing(ringCenter, bRx, bRx * bTilt, bRot, stroke * .8f, bAlpha, front = true)
        drawRing(ringCenter, aRx, aRx * aTilt, aRot, stroke, alpha, front = true)
        if (orbOn && !orbBehind) drawOrb(ringCenter, aRx, aRx * aTilt, aRot, phi, .04f * u, orbA * alpha)

        // 小币群悬浮（8.2–10.6s）
        if (t in 8.2f..10.8f) {
            val gA = seg(t, 8.2f, 8.6f) * (1f - seg(t, 10.2f, 10.7f))
            val gc = Offset(cx, coinY - .34f * u + sin(t * 2.2f) * .015f * u)
            for (i in 0 until 5) {
                val ang = -PI.toFloat() / 2f + i * 2f * PI.toFloat() / 5f + (t - 8.2f) * .35f
                val pop = easeOut(seg(t, 8.2f + i * .08f, 8.6f + i * .08f))
                drawIconCoin(Offset(gc.x + cos(ang) * .11f * u, gc.y + sin(ang) * .085f * u), .05f * u * pop, sin(t * 2f + i) * .5f, gA, art.coins[i])
            }
            drawBrandCoin(gc, .055f * u * easeOut(seg(t, 8.5f, 8.9f)), t * 2f, gA, art, bezel = false)
        }
    }
}

/** 金属轨道环：只画前半（下半弧）或后半（上半弧），实现与币体的前后遮挡。 */
private fun DrawScope.drawRing(c: Offset, rx: Float, ry: Float, rotDeg: Float, stroke: Float, alpha: Float, front: Boolean) {
    if (alpha <= .01f || rx < 1f) return
    val ryy = max(ry, 1f)
    rotate(rotDeg, c) {
        val tl = Offset(c.x - rx, c.y - ryy)
        val sz = Size(rx * 2f, ryy * 2f)
        val start = if (front) 0f else 180f
        val metal = Brush.linearGradient(
            0f to SilverLo, .22f to SilverHi, .45f to Silver, .62f to Lime, .72f to LimeHi, .86f to SilverHi, 1f to SilverLo,
            start = Offset(c.x - rx, c.y), end = Offset(c.x + rx, c.y),
        )
        // 底面厚度
        drawArc(SilverLo.copy(alpha = .55f * alpha), start, 180f, false, tl.copy(y = tl.y + stroke * .55f), sz, style = Stroke(stroke, cap = StrokeCap.Round))
        drawArc(metal, start, 180f, false, tl, sz, alpha = alpha, style = Stroke(stroke, cap = StrokeCap.Round))
        drawArc(Color.White.copy(alpha = .85f * alpha), start, 180f, false, tl.copy(y = tl.y - stroke * .18f), sz, style = Stroke(stroke * .22f, cap = StrokeCap.Round))
    }
}

private fun DrawScope.drawOrb(c: Offset, rx: Float, ry: Float, rotDeg: Float, phi: Float, r: Float, alpha: Float) {
    if (alpha <= .01f) return
    val px = cos(phi) * rx; val py = sin(phi) * max(ry, 1f)
    val rad = rotDeg * PI.toFloat() / 180f
    val p = Offset(c.x + px * cos(rad) - py * sin(rad), c.y + px * sin(rad) + py * cos(rad))
    drawCircle(Brush.radialGradient(listOf(LimeHi, Lime, LimeLo), center = p - Offset(r * .35f, r * .35f), radius = r * 1.4f), r, p, alpha)
    drawCircle(Color.White.copy(alpha = .6f * alpha), r * .28f, p - Offset(r * .35f, r * .35f))
}

/** 硬币厚度：两片偏移的椭圆 + 连接矩形。 */
private fun DrawScope.drawCoinBody(c: Offset, r: Float, spin: Float, edge: Color, alpha: Float): Float {
    val ws = max(abs(cos(spin)), .07f)
    val shift = r * .17f * sin(spin)
    val rw = r * ws
    if (abs(shift) > .5f) {
        drawOval(edge, Offset(c.x + shift - rw, c.y - r), Size(rw * 2f, r * 2f), alpha)
        drawRect(edge, Offset(min(c.x, c.x + shift), c.y - r), Size(abs(shift), r * 2f), alpha)
    }
    return ws
}

private fun DrawScope.drawGloss(c: Offset, r: Float, ws: Float, alpha: Float) {
    drawOval(
        Brush.linearGradient(listOf(Color.White.copy(alpha = .5f), Color.Transparent), start = Offset(c.x - r * ws, c.y - r), end = Offset(c.x, c.y)),
        Offset(c.x - r * ws * .86f, c.y - r * .86f), Size(r * ws * 1.72f, r * 1.72f), alpha,
    )
}

private fun DrawScope.drawBrandCoin(c: Offset, r: Float, spin: Float, alpha: Float, art: HeroArt, bezel: Boolean) {
    if (alpha <= .01f || r < 1f) return
    val faceR = if (bezel) r * .8f else r
    val ws = drawCoinBody(c, r, spin, if (bezel) SilverLo else LimeEdge, alpha)
    if (bezel) {
        drawOval(Brush.linearGradient(listOf(SilverHi, Silver, SilverLo, SilverHi), start = Offset(c.x - r, c.y - r), end = Offset(c.x + r, c.y + r)),
            Offset(c.x - r * ws, c.y - r), Size(r * ws * 2f, r * 2f), alpha)
    }
    drawOval(Brush.radialGradient(listOf(LimeHi, Lime, LimeLo), center = Offset(c.x - faceR * ws * .35f, c.y - faceR * .4f), radius = faceR * 1.7f),
        Offset(c.x - faceR * ws, c.y - faceR), Size(faceR * ws * 2f, faceR * 2f), alpha)
    drawOval(LimeLo.copy(alpha = .7f), Offset(c.x - faceR * .84f * ws, c.y - faceR * .84f), Size(faceR * 1.68f * ws, faceR * 1.68f), alpha, style = Stroke(max(1f, faceR * .04f)))
    if (ws > .12f) {
        val d = faceR * 1.1f
        scale(ws, 1f, c) {
            translate(c.x - d / 2f, c.y - d / 2f) {
                with(art.mark) { draw(Size(d, d), alpha = alpha, colorFilter = ColorFilter.tint(Ink)) }
            }
        }
    }
    drawGloss(c, r, ws, alpha)
}

private fun DrawScope.drawIconCoin(c: Offset, r: Float, spin: Float, alpha: Float, icon: Painter) {
    if (alpha <= .01f || r < 1f) return
    val ws = drawCoinBody(c, r, spin, SilverLo, alpha)
    drawOval(Silver, Offset(c.x - r * ws, c.y - r), Size(r * ws * 2f, r * 2f), alpha)
    val d = r * 1.84f
    scale(ws, 1f, c) {
        translate(c.x - d / 2f, c.y - d / 2f) { with(icon) { draw(Size(d, d), alpha = alpha) } }
    }
    drawGloss(c, r, ws, alpha)
}
