package click.cowa.linbit.feature.profile

import android.app.DatePickerDialog
import androidx.activity.compose.BackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import click.cowa.linbit.core.generated.ApiKycStatus
import click.cowa.linbit.core.generated.ApiKycSubmission
import click.cowa.linbit.feature.assets.AssetsNavBar
import click.cowa.linbit.feature.assets.PickerField
import click.cowa.linbit.feature.auth.LbSheet
import click.cowa.linbit.ui.components.*
import click.cowa.linbit.ui.theme.*
import java.time.LocalDate
import java.time.ZoneId
import java.util.Calendar

private val Countries = listOf(
    "SG" to "新加坡", "HK" to "中国香港", "TW" to "中国台湾", "MY" to "马来西亚", "JP" to "日本",
    "KR" to "韩国", "TH" to "泰国", "VN" to "越南", "PH" to "菲律宾", "ID" to "印度尼西亚",
    "AE" to "阿联酋", "GB" to "英国", "DE" to "德国", "AU" to "澳大利亚", "CA" to "加拿大",
)

@Composable
fun KycHost(onBack: () -> Unit, vm: KycViewModel = hiltViewModel()) {
    var formLevel by remember { mutableStateOf<Int?>(null) }
    val s by vm.state.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { vm.load() }
    BackHandler { if (formLevel == null) onBack() else formLevel = null }
    if (formLevel == null) KycStatusScreen(s, onBack, onStart = { level ->
        vm.update { KycInput(level = level, country = "SG") }
        formLevel = level
    }, onRetry = vm::load)
    else KycFormScreen(s, vm, formLevel!!, onBack = { formLevel = null })
}

@Composable
private fun KycStatusScreen(s: KycUiState, onBack: () -> Unit, onStart: (Int) -> Unit, onRetry: () -> Unit) {
    val p = LB.p
    Column(Modifier.fillMaxSize().background(p.bg).statusBarsPadding().navigationBarsPadding()) {
        AssetsNavBar("身份认证", onBack)
        when {
            s.loading && s.status == null -> ProfileLoading()
            s.error != null && s.status == null -> ProfileError(s.error, onRetry)
            s.status != null -> Column(Modifier.verticalScroll(rememberScrollState()).padding(Spacing.pageH), verticalArrangement = Arrangement.spacedBy(16.dp)) {
                KycStatusCard(s.status)
                KycLevelCard(1, "基础认证", listOf("解锁身份认证对应的提现额度", "解锁合约、理财等交易功能"), "证件正面${if (s.input.idType != ApiKycSubmission.IdType.PASSPORT) "、背面" else ""}与手持自拍", s.status, onStart)
                KycLevelCard(2, "高级认证", listOf("提升高级账户提现额度", "支持更完整的机构服务"), "基础资料、证件与近 3 个月地址证明", s.status, onStart)
                InfoNote("认证资料仅用于合规审核，经加密存储。审核结果以服务端状态为准。")
            }
        }
    }
}

@Composable
private fun KycStatusCard(status: ApiKycStatus) {
    val p = LB.p
    val color = when (status.status) {
        ApiKycStatus.Status.APPROVED -> p.up
        ApiKycStatus.Status.PENDING -> p.brand
        ApiKycStatus.Status.REJECTED -> p.down
        ApiKycStatus.Status.NONE -> p.textSecondary
    }
    Column(Modifier.fillMaxWidth().background(p.fill, RoundedCornerShape(Radius.lg)).padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(ProfileLogic.kycStatusText(status), style = lb(18.sp, FontWeight.SemiBold), color = color)
        status.submittedAt?.let { if (status.status != ApiKycStatus.Status.NONE) Text("提交时间 $it", style = lbNum(FontSize.small), color = p.textTertiary) }
        if (status.status == ApiKycStatus.Status.REJECTED && !status.reason.isNullOrBlank()) Text("原因：${status.reason}", style = lb(FontSize.small), color = p.down)
        if (status.status == ApiKycStatus.Status.PENDING) Text("资料已提交，请耐心等待审核", style = lb(FontSize.small), color = p.textSecondary)
    }
}

@Composable
private fun KycLevelCard(level: Int, title: String, perks: List<String>, need: String, status: ApiKycStatus, onStart: (Int) -> Unit) {
    val p = LB.p
    val passed = status.status == ApiKycStatus.Status.APPROVED && status.level >= level
    val pending = status.status == ApiKycStatus.Status.PENDING && status.level == level
    val locked = level == 2 && !(status.status == ApiKycStatus.Status.APPROVED && status.level >= 1)
    val active = level == 1 && status.status != ApiKycStatus.Status.APPROVED
    Column(Modifier.fillMaxWidth().background(if (active) p.upTint else p.bg, RoundedCornerShape(Radius.lg)).border(1.dp, if (active) p.brand.copy(alpha = 0.35f) else p.border, RoundedCornerShape(Radius.lg)).padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text("Lv.$level $title", Modifier.weight(1f), style = lb(FontSize.body, FontWeight.SemiBold), color = p.textPrimary)
            when {
                passed -> StatusTag("已认证", p.up)
                pending -> StatusTag("审核中", p.brand)
                locked -> StatusTag("需先完成基础认证", p.textSecondary)
            }
        }
        perks.forEach { Text("✓  $it", style = lb(FontSize.small), color = p.textSecondary) }
        Text("所需资料：$need", style = lb(FontSize.small), color = p.textTertiary)
        if (!passed && !pending && !locked) LbButton(if (status.status == ApiKycStatus.Status.REJECTED && status.level == level) "重新认证" else "去认证", height = 40.dp) { onStart(level) }
    }
}

@Composable
private fun KycFormScreen(s: KycUiState, vm: KycViewModel, level: Int, onBack: () -> Unit) {
    val p = LB.p
    val input = s.input
    var countrySheet by remember { mutableStateOf(false) }
    var typeSheet by remember { mutableStateOf(false) }
    val context = LocalContext.current
    Column(Modifier.fillMaxSize().background(p.bg).statusBarsPadding().navigationBarsPadding()) {
        AssetsNavBar("KYC 验证", onBack)
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(horizontal = Spacing.pageH)) {
            Text(if (level == 2) "高级认证" else "基础认证", style = lb(24.sp, FontWeight.Bold), color = p.textPrimary)
            Spacer(Modifier.height(8.dp))
            Text("请确保资料与证件一致、照片清晰完整。图片仅支持 JPG / PNG，单张不超过 10MB。", style = lb(FontSize.small), color = p.textSecondary)
            if (level == 2) Box(Modifier.padding(top = 12.dp)) { InfoNote("高级认证需重新提交基础资料与证件，并补充近 3 个月地址证明。") }
            Spacer(Modifier.height(20.dp))
            PickerField("国家或地区", Countries.firstOrNull { it.first == input.country }?.let { "${it.second} (${it.first})" }.orEmpty(), onClick = { countrySheet = true })
            Spacer(Modifier.height(16.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                LabeledField("名", input.firstName, "与证件一致", Modifier.weight(1f)) { v -> vm.update { it.copy(firstName = v) } }
                LabeledField("姓", input.lastName, "与证件一致", Modifier.weight(1f)) { v -> vm.update { it.copy(lastName = v) } }
            }
            Spacer(Modifier.height(16.dp))
            PickerField("出生日期", input.dateOfBirth, "请选择出生日期") {
                val initial = runCatching { LocalDate.parse(input.dateOfBirth) }.getOrElse { LocalDate.now().minusYears(30) }
                DatePickerDialog(context, { _, y, m, d -> vm.update { it.copy(dateOfBirth = "%04d-%02d-%02d".format(y, m + 1, d)) } }, initial.year, initial.monthValue - 1, initial.dayOfMonth).apply {
                    datePicker.maxDate = Calendar.getInstance().timeInMillis
                }.show()
            }
            Spacer(Modifier.height(16.dp))
            PickerField("证件类型", idTypeLabel(input.idType), onClick = { typeSheet = true })
            Spacer(Modifier.height(16.dp))
            LabeledField("证件号码", input.idNumber, "请输入证件号码") { v -> vm.update { it.copy(idNumber = v.take(64)) } }
            if (input.idNumber.isNotEmpty() && input.idNumber.trim().length < 5) Text("证件号码格式不正确", Modifier.padding(top = 6.dp), style = lb(FontSize.small), color = p.down)
            input.dateOfBirth.takeIf { it.isNotBlank() }?.let { dob ->
                if ((ProfileLogic.ageAt(dob) ?: 0) < 18) Text("未满 18 周岁无法完成认证", Modifier.padding(top = 8.dp), style = lb(FontSize.small), color = p.down)
            }
            Text("上传证件", Modifier.padding(top = 24.dp, bottom = 12.dp), style = lb(FontSize.body, FontWeight.SemiBold), color = p.textPrimary)
            ProfileLogic.requiredSides(level, input.idType).forEach { side -> DocumentSlot(side, s, vm) }
            InfoNote("请上传清晰、完整、无遮挡的原件照片。图片在本地重编码，不保留原图位置等元数据。")
            s.error?.let { Text(it, Modifier.padding(top = 12.dp), style = lb(FontSize.small), color = p.down) }
            Spacer(Modifier.height(24.dp))
        }
        LbButton("提交审核", Modifier.padding(Spacing.pageH), enabled = s.uploading == null && !s.submitting && ProfileLogic.kycError(input) == null, loading = s.submitting) { vm.submit(onBack) }
    }
    if (countrySheet) LbSheet("选择国家 / 地区", { countrySheet = false }, showClose = true) {
        LazyColumn(Modifier.height(420.dp)) {
            items(Countries, key = { it.first }) { country ->
                Row(Modifier.fillMaxWidth().height(48.dp).pressable(scale = 1f) { vm.update { it.copy(country = country.first) }; countrySheet = false }.padding(horizontal = Spacing.pageH), verticalAlignment = Alignment.CenterVertically) {
                    Text("${country.second} (${country.first})", Modifier.weight(1f), style = lb(FontSize.body), color = p.textPrimary)
                    if (country.first == input.country) Text("✓", color = p.brand)
                }
            }
        }
    }
    if (typeSheet) LbSheet("证件类型", { typeSheet = false }) {
        Column(Modifier.padding(bottom = 24.dp)) {
            ApiKycSubmission.IdType.entries.forEach { type ->
                ProfileRow(idTypeLabel(type)) { vm.update { it.copy(idType = type) }; typeSheet = false }
            }
        }
    }
}

@Composable
private fun LabeledField(label: String, value: String, placeholder: String, modifier: Modifier = Modifier, onChange: (String) -> Unit) {
    Column(modifier) {
        FieldLabel(label)
        Spacer(Modifier.height(8.dp))
        LbTextField(value, onChange, placeholder, height = 48.dp)
    }
}

@Composable
private fun DocumentSlot(side: ApiKycDocumentSide, s: KycUiState, vm: KycViewModel) {
    val p = LB.p
    val uploaded = side in s.input.uploaded
    val uploading = s.uploading == side
    val launcher = rememberLauncherForActivityResult(ActivityResultContracts.GetContent()) { uri -> if (uri != null) vm.upload(uri, side) }
    Column(Modifier.padding(bottom = 16.dp)) {
        Text("上传${side.title}", style = lb(FontSize.label), color = p.textPrimary)
        Spacer(Modifier.height(8.dp))
        Column(
            Modifier.fillMaxWidth().height(128.dp).background(p.fill, RoundedCornerShape(Radius.md)).pressable(enabled = !uploading && !s.submitting, scale = 1f) { launcher.launch("image/*") },
            horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center,
        ) {
            if (uploading) CircularProgressIndicator(Modifier.size(26.dp), color = p.brand, strokeWidth = 2.dp)
            else {
                Text(if (uploaded) "✓" else "+", style = lb(28.sp, FontWeight.Medium), color = if (uploaded) p.up else p.brand)
                Text(if (uploaded) "已上传，点击重新选择" else "点击上传", Modifier.padding(top = 8.dp), style = lb(FontSize.small, FontWeight.Medium), color = if (uploaded) p.up else p.brand)
            }
        }
    }
}

private fun idTypeLabel(type: ApiKycSubmission.IdType): String = when (type) {
    ApiKycSubmission.IdType.PASSPORT -> "护照"
    ApiKycSubmission.IdType.ID_CARD -> "身份证"
    ApiKycSubmission.IdType.DRIVER_LICENSE -> "驾照"
}
