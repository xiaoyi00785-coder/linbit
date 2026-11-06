package click.cowa.linbit.ui.splash

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.scale
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.unit.dp
import click.cowa.linbit.R
import kotlin.math.pow

/** 启动动画总时长（毫秒），与 iOS SplashView.duration（1.2s）一致。 */
const val SPLASH_DURATION_MS = 1200

/** 品牌绿（与 res/values/colors.xml 的 splash_bg、brand/logo.svg 底色一致）。 */
private val Brand = Color(0xFFABE127)
private val Ink = Color(0xFF070808)

private fun seg(t: Float, a: Float, b: Float) = ((t - a) / (b - a)).coerceIn(0f, 1f)
private fun easeOut(x: Float) = 1f - (1f - x).pow(3)
private fun backOut(x: Float): Float {
    val c = 1.70158f
    return 1f + (c + 1f) * (x - 1f).pow(3) + c * (x - 1f).pow(2)
}

/**
 * 启动动画（对应 iOS SplashView，约 1.2s）。与系统 SplashScreen（品牌绿底 + 居中符号）无缝衔接：同背景色、同位置。
 * 0–0.35 符号缩放回弹并淡入 → 0.4–0.85 光环扩散 → 0.85–1 整体淡出，露出首页。
 * 由单一进度 t∈[0,1] 驱动；[frozen] 非空时冻结在该进度（截图 / 预览用）。
 * 符号取自 drawable/ic_splash_icon（与系统启动页同一个 drawable，单一来源 mobile/android/brand/logo.svg），换 Logo 后重跑 gen_android_vectors.py 即可。
 */
@Composable
fun SplashOverlay(reduceMotion: Boolean, onFinish: () -> Unit, frozen: Float? = null) {
    val progress = remember { Animatable(frozen ?: 0f) }
    LaunchedEffect(Unit) {
        if (frozen != null) return@LaunchedEffect
        if (reduceMotion) {
            progress.snapTo(0.85f)
            kotlinx.coroutines.delay(50)
        } else {
            progress.animateTo(1f, tween(SPLASH_DURATION_MS, easing = LinearEasing))
        }
        onFinish()
    }
    SplashFrame(progress.value)
}

@Composable
fun SplashFrame(t: Float) {
    val pop = seg(t, 0f, 0.35f)
    val ring = seg(t, 0.4f, 0.85f)
    val out = seg(t, 0.85f, 1f)
    Box(Modifier.fillMaxSize().background(Brand).alpha(1f - out), contentAlignment = Alignment.Center) {
        Box(Modifier.scale(1f + 0.08f * out), contentAlignment = Alignment.Center) {
            // 光环扩散
            Canvas(Modifier.size(320.dp).alpha((1f - ring) * ring * 4f * 0.5f)) {
                drawCircle(Ink, radius = size.minDimension / 2 * (0.4f + 0.6f * easeOut(ring)), center = Offset(size.width / 2, size.height / 2), style = Stroke(width = 2.dp.toPx()))
            }
            // 符号：回弹出现
            // 与系统启动页同一个 drawable（288dp 画布）→ 位置、大小完全一致；从 0.85 倍回弹到 1（始终可见，避免闪烁）
            Image(painterResource(R.drawable.ic_splash_icon), null, Modifier.size(288.dp).scale(0.85f + 0.15f * backOut(pop)))
        }
    }
}
