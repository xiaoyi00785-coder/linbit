@file:Suppress("DEPRECATION")
package click.cowa.linbit.feature.scan

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.hardware.Camera
import android.os.Handler
import android.os.Looper
import android.view.SurfaceHolder
import android.view.SurfaceView
import androidx.activity.compose.BackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.content.ContextCompat
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import click.cowa.linbit.feature.assets.AssetsNavBar
import click.cowa.linbit.ui.components.*
import click.cowa.linbit.ui.theme.*
import com.google.zxing.*
import com.google.zxing.common.HybridBinarizer
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean

internal fun decodeQr(bitmap: android.graphics.Bitmap): String? {
    val pixels = IntArray(bitmap.width * bitmap.height)
    bitmap.getPixels(pixels, 0, bitmap.width, 0, 0, bitmap.width, bitmap.height)
    return runCatching { MultiFormatReader().decode(BinaryBitmap(HybridBinarizer(RGBLuminanceSource(bitmap.width, bitmap.height, pixels)))).text }.getOrNull()
}

/** Camera frames are decoded off-main; no QR content can initiate a money action or open a URL. */
private class QrPreview(context: Context, val onResult: (String) -> Unit, val onError: (String) -> Unit) : SurfaceView(context), SurfaceHolder.Callback {
    private var camera: Camera? = null
    private val executor = Executors.newSingleThreadExecutor()
    private val busy = AtomicBoolean(false)
    private var released = false
    init { holder.addCallback(this) }
    override fun surfaceCreated(holder: SurfaceHolder) {
        if (released) return
        runCatching {
            val id = (0 until Camera.getNumberOfCameras()).firstOrNull { Camera.CameraInfo().also { info -> Camera.getCameraInfo(it, info) }.facing == Camera.CameraInfo.CAMERA_FACING_BACK } ?: 0
            val c = Camera.open(id); camera = c
            val params = c.parameters
            params.supportedPreviewSizes.minByOrNull { kotlin.math.abs(it.width - 1280) + kotlin.math.abs(it.height - 720) }?.let { params.setPreviewSize(it.width, it.height) }
            if (Camera.Parameters.FOCUS_MODE_CONTINUOUS_PICTURE in params.supportedFocusModes.orEmpty()) params.focusMode = Camera.Parameters.FOCUS_MODE_CONTINUOUS_PICTURE
            c.parameters = params
            val info = Camera.CameraInfo().also { Camera.getCameraInfo(id, it) }
            c.setDisplayOrientation(info.orientation)
            c.setPreviewDisplay(holder)
            c.setPreviewCallback { bytes, cam ->
                if (released || bytes == null || !busy.compareAndSet(false, true)) return@setPreviewCallback
                val size = cam.parameters.previewSize
                val copy = bytes.copyOf()
                runCatching { executor.execute {
                    val reader = MultiFormatReader()
                    val source = PlanarYUVLuminanceSource(copy, size.width, size.height, 0, 0, size.width, size.height, false)
                    val result = runCatching { reader.decode(BinaryBitmap(HybridBinarizer(source))).text }.getOrNull()
                        ?: runCatching { reader.reset(); reader.decode(BinaryBitmap(HybridBinarizer(source.invert()))).text }.getOrNull()
                    if (result != null) Handler(Looper.getMainLooper()).post { if (!released) onResult(result) }
                    busy.set(false)
                } }.onFailure { busy.set(false) }
            }
            c.startPreview()
        }.onFailure { stop(); onError("无法打开相机，请检查权限或相机是否被其他应用占用") }
    }
    override fun surfaceChanged(holder: SurfaceHolder, format: Int, width: Int, height: Int) = Unit
    override fun surfaceDestroyed(holder: SurfaceHolder) = stop()
    fun torch(enabled: Boolean) {
        runCatching { camera?.let { c -> val p = c.parameters; val mode = if (enabled) Camera.Parameters.FLASH_MODE_TORCH else Camera.Parameters.FLASH_MODE_OFF; if (mode in p.supportedFlashModes.orEmpty()) { p.flashMode = mode; c.parameters = p } } }
    }
    fun stop() { camera?.let { runCatching { it.setPreviewCallback(null); it.stopPreview(); it.release() } }; camera = null }
    fun release() { released = true; stop(); executor.shutdown() }
}

@Composable
fun ScanScreen(onClose: () -> Unit) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val clipboard = LocalClipboardManager.current
    val owner = LocalLifecycleOwner.current
    var permitted by remember { mutableStateOf(ContextCompat.checkSelfPermission(context, Manifest.permission.CAMERA) == PackageManager.PERMISSION_GRANTED) }
    var active by remember { mutableStateOf(true) }
    var torch by remember { mutableStateOf(false) }
    var result by remember { mutableStateOf<String?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var preview by remember { mutableStateOf<QrPreview?>(null) }
    val permission = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { permitted = it }
    val photo = rememberLauncherForActivityResult(ActivityResultContracts.GetContent()) { uri ->
        if (uri != null) scope.launch {
            val decoded = withContext(Dispatchers.Default) {
                runCatching {
                    val bounds = android.graphics.BitmapFactory.Options().apply { inJustDecodeBounds = true }
                    context.contentResolver.openInputStream(uri)?.use { android.graphics.BitmapFactory.decodeStream(it, null, bounds) }
                    var sample = 1
                    while (bounds.outWidth / sample > 2048 || bounds.outHeight / sample > 2048) sample *= 2
                    val options = android.graphics.BitmapFactory.Options().apply { inSampleSize = sample }
                    context.contentResolver.openInputStream(uri)?.use { stream ->
                        android.graphics.BitmapFactory.decodeStream(stream, null, options)?.let { bitmap -> try { decodeQr(bitmap) } finally { bitmap.recycle() } }
                    }
                }.getOrNull()
            }
            if (decoded == null) error = "未识别到二维码，请选择清晰图片" else result = decoded
        }
    }
    LaunchedEffect(Unit) { if (!permitted) permission.launch(Manifest.permission.CAMERA) }
    DisposableEffect(owner) {
        val observer = LifecycleEventObserver { _, event -> if (event == Lifecycle.Event.ON_PAUSE) active = false else if (event == Lifecycle.Event.ON_RESUME) active = true }
        owner.lifecycle.addObserver(observer)
        onDispose { owner.lifecycle.removeObserver(observer); preview?.release() }
    }
    BackHandler(onBack = onClose)
    Column(Modifier.fillMaxSize().background(LB.p.bg).statusBarsPadding().navigationBarsPadding()) {
        AssetsNavBar("扫一扫", onClose)
        Box(Modifier.weight(1f).fillMaxWidth(), contentAlignment = Alignment.Center) {
            if (permitted && active && result == null) AndroidView(
                factory = { QrPreview(it, { value -> result = value }, { value -> error = value }).also { preview = it } },
                modifier = Modifier.fillMaxSize(), update = { it.torch(torch) }, onRelease = { it.release(); preview = null },
            ) else if (!permitted) Column(horizontalAlignment = Alignment.CenterHorizontally) {
                Text("允许相机权限以扫描二维码", color = LB.p.textSecondary)
                LbButton("授权相机", Modifier.padding(24.dp)) { permission.launch(Manifest.permission.CAMERA) }
                TextButton(onClick = { context.startActivity(android.content.Intent(android.provider.Settings.ACTION_APPLICATION_DETAILS_SETTINGS, android.net.Uri.parse("package:" + context.packageName))) }) { Text("前往系统设置") }
            }
        }
        Text("将二维码放入镜头范围内，自动识别", Modifier.padding(20.dp), color = LB.p.textSecondary, style = lb(FontSize.caption))
        Row(Modifier.padding(20.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            LbButton(if (torch) "关闭手电筒" else "手电筒", Modifier.weight(1f), kind = ButtonKind.SECONDARY) { torch = !torch }
            LbButton("相册", Modifier.weight(1f), kind = ButtonKind.SECONDARY) { photo.launch("image/*") }
        }
    }
    result?.let { value -> LbDialog("扫描结果", { result = null }, "复制", { clipboard.setText(AnnotatedString(value)); result = null }, message = value.take(4096), dismissText = "继续扫描") }
    error?.let { message -> LbDialog("扫一扫", { error = null }, "知道了", { error = null }, message = message, dismissText = null) }
}
