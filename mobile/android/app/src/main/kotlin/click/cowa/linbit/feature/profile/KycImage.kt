package click.cowa.linbit.feature.profile

import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.net.Uri
import java.io.ByteArrayOutputStream
import kotlin.math.max

/** KYC 图片本地解码、缩放与 JPEG 重编码：不保留原图 EXIF / GPS 元数据。 */
object KycImage {
    const val MAX_BYTES = 10 * 1024 * 1024

    fun jpeg(context: Context, uri: Uri): ByteArray {
        val original = context.contentResolver.openInputStream(uri)?.use(BitmapFactory::decodeStream)
            ?: throw IllegalArgumentException("无法读取图片")
        var bitmap = original
        if (max(bitmap.width, bitmap.height) > 2400) {
            val scale = 2400f / max(bitmap.width, bitmap.height)
            bitmap = Bitmap.createScaledBitmap(bitmap, (bitmap.width * scale).toInt(), (bitmap.height * scale).toInt(), true)
        }
        try {
            for (quality in listOf(90, 80, 70, 60, 50, 40)) {
                val out = ByteArrayOutputStream()
                bitmap.compress(Bitmap.CompressFormat.JPEG, quality, out)
                if (out.size() <= MAX_BYTES) return out.toByteArray()
            }
            throw IllegalArgumentException("图片过大，请选择更小的图片")
        } finally {
            if (bitmap !== original) bitmap.recycle()
            original.recycle()
        }
    }
}
