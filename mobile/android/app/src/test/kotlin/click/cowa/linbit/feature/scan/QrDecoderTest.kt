package click.cowa.linbit.feature.scan

import android.app.Application
import android.graphics.Bitmap
import com.google.zxing.BarcodeFormat
import com.google.zxing.MultiFormatWriter
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = Application::class)
class QrDecoderTest {
    @Test fun decodesRealQrWithoutOpeningOrSubmittingAnything() {
        val payload = "linbit:test-qr-no-transaction"
        val matrix = MultiFormatWriter().encode(payload, BarcodeFormat.QR_CODE, 320, 320)
        val pixels = IntArray(320 * 320) { i -> if (matrix[i % 320, i / 320]) android.graphics.Color.BLACK else android.graphics.Color.WHITE }
        val bitmap = Bitmap.createBitmap(pixels, 320, 320, Bitmap.Config.ARGB_8888)
        assertEquals(payload, decodeQr(bitmap))
        if (System.getProperty("production.evidence") == "1") {
            java.io.File(System.getProperty("production.evidence.dir"), "qr-fixture.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
        bitmap.recycle()
    }
    @Test fun emptyPhotoIsNotAValidQr() {
        val bitmap = Bitmap.createBitmap(100, 100, Bitmap.Config.ARGB_8888)
        bitmap.eraseColor(android.graphics.Color.WHITE)
        assertNull(decodeQr(bitmap))
        bitmap.recycle()
    }
}
