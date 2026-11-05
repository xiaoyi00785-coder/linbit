package click.cowa.linbit.ui.components

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.scaleIn
import androidx.compose.animation.scaleOut
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import click.cowa.linbit.core.network.lbMessage
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.Radius
import click.cowa.linbit.ui.theme.lb
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch

/** Toast（居中黑色半透明胶囊，2 秒消失；成功 / 失败 / 普通），对应 iOS ToastCenter。 */
class ToastCenter(private val scope: CoroutineScope) {
    enum class Kind { INFO, SUCCESS, ERROR }
    data class Item(val id: Long, val text: String, val kind: Kind)

    private val _current = MutableStateFlow<Item?>(null)
    val current: StateFlow<Item?> = _current
    private var seq = 0L
    private var hide: Job? = null

    fun show(text: String, kind: Kind = Kind.INFO) {
        val item = Item(++seq, text, kind)
        _current.value = item
        hide?.cancel()
        hide = scope.launch { delay(2_000); if (_current.value?.id == item.id) _current.value = null }
    }

    fun error(t: Throwable, sessionExpired: Boolean = false) =
        show(t.lbMessage(sessionExpired), Kind.ERROR)
}

@Composable
fun ToastOverlay(center: ToastCenter) {
    val item by center.current.collectAsStateWithLifecycle()
    Box(Modifier.fillMaxSize().statusBarsPadding(), contentAlignment = Alignment.TopCenter) {
        AnimatedVisibility(item != null, enter = fadeIn() + scaleIn(initialScale = 0.95f), exit = fadeOut() + scaleOut(targetScale = 0.95f)) {
            val it = item
            if (it != null) {
                Row(
                    Modifier
                        .padding(top = 48.dp, start = 48.dp, end = 48.dp)
                        .background(Color.Black.copy(alpha = 0.82f), RoundedCornerShape(Radius.md))
                        .padding(horizontal = 16.dp, vertical = 11.dp),
                ) {
                    Text(
                        (when (it.kind) { ToastCenter.Kind.SUCCESS -> "✓ "; ToastCenter.Kind.ERROR -> "! "; else -> "" }) + it.text,
                        style = lb(FontSize.label), color = Color.White, textAlign = TextAlign.Center,
                    )
                }
            }
        }
    }
}
