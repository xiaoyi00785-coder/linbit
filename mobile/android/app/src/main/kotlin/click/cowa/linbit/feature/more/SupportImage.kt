package click.cowa.linbit.feature.more

import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.net.Uri
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.ByteArrayOutputStream

/** 图片重编码去掉 EXIF/GPS 等元数据；失败时不上传原图。 */
data class SupportImage(val bytes: ByteArray, val mime: String, val filename: String) {
    override fun equals(other: Any?): Boolean = other is SupportImage && bytes.contentEquals(other.bytes) && mime == other.mime && filename == other.filename
    override fun hashCode(): Int = bytes.contentHashCode()
}

object SupportImageProcessor {
    const val MAX_BYTES = 5 * 1024 * 1024
    const val MAX_COUNT = 3

    suspend fun read(context: Context, uri: Uri): Result<SupportImage> = withContext(Dispatchers.IO) {
        runCatching {
            val raw = context.contentResolver.openInputStream(uri)?.use { input ->
                val output = ByteArrayOutputStream()
                val buffer = ByteArray(16 * 1024)
                while (true) {
                    val n = input.read(buffer)
                    if (n < 0) break
                    output.write(buffer, 0, n)
                    if (output.size() > MAX_BYTES) throw IllegalArgumentException("单张图片不能超过 5MB")
                }
                output.toByteArray()
            } ?: throw IllegalArgumentException("无法读取所选图片")
            val opts = BitmapFactory.Options().apply { inJustDecodeBounds = true }
            BitmapFactory.decodeByteArray(raw, 0, raw.size, opts)
            val mime = opts.outMimeType
            val format = when (mime) {
                "image/jpeg" -> Bitmap.CompressFormat.JPEG to "jpg"
                "image/png" -> Bitmap.CompressFormat.PNG to "png"
                "image/webp" -> Bitmap.CompressFormat.WEBP to "webp"
                else -> throw IllegalArgumentException("仅支持 JPG / PNG / WEBP 格式图片")
            }
            val bitmap = BitmapFactory.decodeByteArray(raw, 0, raw.size)
                ?: throw IllegalArgumentException("无法处理所选图片，请换一张再试")
            val clean = ByteArrayOutputStream().use { out ->
                if (!bitmap.compress(format.first, 92, out)) throw IllegalArgumentException("无法处理所选图片，请换一张再试")
                out.toByteArray()
            }
            bitmap.recycle()
            if (clean.size > MAX_BYTES) throw IllegalArgumentException("单张图片不能超过 5MB")
            SupportImage(clean, mime, "image.${format.second}")
        }
    }
}
