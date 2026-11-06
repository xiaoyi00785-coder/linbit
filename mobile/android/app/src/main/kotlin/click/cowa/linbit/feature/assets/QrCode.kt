package click.cowa.linbit.feature.assets

import android.graphics.Bitmap
import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import com.google.zxing.BarcodeFormat
import com.google.zxing.EncodeHintType
import com.google.zxing.qrcode.QRCodeWriter

fun qrBitmap(text: String, sizePx: Int = 512): Bitmap? {
    if (text.isBlank()) return null
    return try {
        val bits = QRCodeWriter().encode(
            text, BarcodeFormat.QR_CODE, sizePx, sizePx,
            mapOf(EncodeHintType.MARGIN to 1, EncodeHintType.CHARACTER_SET to "UTF-8"),
        )
        val w = bits.width
        val h = bits.height
        val px = IntArray(w * h) { i ->
            val x = i % w
            val y = i / w
            if (bits[x, y]) 0xFF000000.toInt() else 0xFFFFFFFF.toInt()
        }
        Bitmap.createBitmap(w, h, Bitmap.Config.ARGB_8888).also { it.setPixels(px, 0, w, 0, 0, w, h) }
    } catch (_: Exception) {
        null
    }
}

@Composable
fun QrImage(text: String, size: Dp = 168.dp, modifier: Modifier = Modifier) {
    val bmp = remember(text) { qrBitmap(text) }
    if (bmp != null) {
        Image(bmp.asImageBitmap(), contentDescription = "二维码", modifier = modifier.size(size))
    }
}
