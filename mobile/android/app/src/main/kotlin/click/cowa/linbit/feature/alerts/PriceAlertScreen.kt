package click.cowa.linbit.feature.alerts

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import click.cowa.linbit.data.paper.*
import click.cowa.linbit.feature.assets.AssetsNavBar
import click.cowa.linbit.ui.components.*
import click.cowa.linbit.ui.theme.*

@Composable
fun PriceAlertScreen(onBack: () -> Unit, onLogin: () -> Unit, vm: PriceAlertViewModel = hiltViewModel(), onDialogWindow: (android.view.Window?) -> Unit = {}) {
    val loggedIn by vm.tokens.loggedIn.collectAsStateWithLifecycle()
    val gate by vm.featureState.collectAsStateWithLifecycle()
    LaunchedEffect(loggedIn) { vm.refresh() }
    LaunchedEffect(vm.retryRemaining) {
        while (vm.retryRemaining > 0) {
            kotlinx.coroutines.delay(250)
            vm.updateRetryRemaining()
        }
    }
    PriceAlertContent(onBack, onLogin, vm, loggedIn, gate, onDialogWindow)
}

@Composable
private fun PriceAlertContent(onBack: () -> Unit, onLogin: () -> Unit, vm: PriceAlertViewModel, loggedIn: Boolean, gate: PriceAlertFeatureSnapshot, onDialogWindow: (android.view.Window?) -> Unit) {
    val p = LB.p
    Column(Modifier.fillMaxSize().background(p.bg).verticalScroll(rememberScrollState()).padding(Spacing.pageH)) {
        AssetsNavBar("价格预警", onBack)
        Text("价格预警", style = lb(FontSize.title, FontWeight.SemiBold), color = p.textPrimary)
        Text("现货（spot）· 外部指数（INDEX）· 仅触发一次（once）· 应用内通知（in_app）。不代表真实交易所或下单成交。", Modifier.padding(vertical = 12.dp), style = lb(FontSize.small), color = p.textSecondary)
        when {
            !loggedIn -> EmptyState("登录后可查看价格预警") { LbButton("登录 / 注册", Modifier.fillMaxWidth(), onClick = onLogin) }
            !gate.checked && gate.failure == null -> CircularProgressIndicator(color = p.brand)
            !gate.enabled -> EmptyState(gate.failure ?: "价格预警功能尚未开放") {
                Text("仅当公开功能状态明确为 true 时读取列表；错误、缺失或 false 均不访问预警接口。", Modifier.padding(vertical = 8.dp), style = lb(FontSize.body), color = p.textSecondary)
                LbButton("重试", Modifier.fillMaxWidth(), testTag = "alert-gate-retry") { vm.refresh() }
            }
            vm.loading && vm.page == 0 -> CircularProgressIndicator(color = p.brand)
            vm.failure != null && vm.page == 0 -> EmptyState("价格预警暂不可用") {
                Text("操作未完成：${vm.failure}", style = lb(FontSize.body), color = p.down)
                RetryRow(vm)
            }
            else -> {
                vm.successNotice?.let { Text(it, color = LB.p.brand, style = lb(FontSize.body), modifier = Modifier.padding(vertical = 5.dp)) }
                LbButton("创建预警", Modifier.fillMaxWidth(), enabled = !vm.mutationBusy && vm.canRequest(), testTag = "alert-create") { vm.openCreate() }
                vm.items.forEachIndexed { index, item ->
                    Column(Modifier.fillMaxWidth().padding(vertical = 7.dp), horizontalAlignment = androidx.compose.ui.Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(5.dp)) {
                        val chinese = if (item.condition == "gte") "指数价格 ≥ ${item.threshold}" else "指数价格 ≤ ${item.threshold}"
                        Text(item.symbol, Modifier.fillMaxWidth(), style = lb(FontSize.label, FontWeight.Medium), color = p.textPrimary)
                        AlertKvRow("阈值 / 状态", "$chinese / ${item.status}")
                        AlertKvRow("价格来源", "spot · INDEX · once · in_app")
                        AlertKvRow("通知与上限", "${item.deliveryStatus} · activePerUser ${item.limit.activePerUser}")
                        LbButton("查看详情", Modifier.fillMaxWidth(), kind = ButtonKind.OUTLINE, enabled = !vm.loading, testTag = "alert-detail-$index") { vm.details(item) }
                        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            LbButton("编辑", Modifier.weight(1f), kind = ButtonKind.OUTLINE, enabled = !vm.mutationBusy && vm.canRequest(), testTag = "alert-edit-$index") { vm.openEdit(item) }
                            LbButton("删除", Modifier.weight(1f), kind = ButtonKind.OUTLINE, enabled = !vm.mutationBusy && vm.canRequest(), testTag = "alert-delete-$index") { vm.askDelete(item) }
                        }
                    }
                }
                if (vm.items.isEmpty() && !vm.loading) Text("暂无价格预警", Modifier.padding(vertical = 18.dp), style = lb(FontSize.body), color = p.textSecondary)
                vm.failure?.let { Text("操作未完成：$it", style = lb(FontSize.body), color = p.down); RetryRow(vm) }
                Text("第${vm.page}页 · ${vm.items.size}/${vm.total}${if (vm.items.size >= vm.total) " · 已到末页" else ""}", style = lb(FontSize.small), color = p.textSecondary)
                if (vm.items.size < vm.total) LbButton(if (vm.loading) "加载中…" else "下一页", enabled = vm.canRequest() && !vm.loading, testTag = "alert-next") { vm.next() }
            }
        }
    }
    if (vm.selected != null) AlertDetail(vm, onDialogWindow)
    if (vm.editor != null) AlertEditorDialog(vm, onDialogWindow)
    if (vm.pendingDelete != null) AlertDeleteDialog(vm, onDialogWindow)
}

@Composable private fun RetryRow(vm: PriceAlertViewModel) {
    if (vm.retryRemaining > 0) Text("${vm.retryRemaining} 秒后可重试", style = lb(FontSize.small), color = LB.p.down)
    val retry = vm.retryRemaining
    LbButton(if (retry > 0) "重试（$retry s）" else "重试", enabled = !vm.loading && retry == 0 && vm.canRequest(), testTag = "alert-retry") { vm.retry() }
}

@Composable private fun AlertDetail(vm: PriceAlertViewModel, onDialogWindow: (android.view.Window?) -> Unit) {
    val p = LB.p
    val item = vm.selected ?: return
    click.cowa.linbit.feature.auth.LbSheet("价格预警详情", { vm.closeDetails() }, showClose = true, onWindowReady = onDialogWindow) {
        Column(Modifier.padding(Spacing.pageH), verticalArrangement = Arrangement.spacedBy(9.dp)) {
            AlertKvRow("预警ID", item.alertId)
            AlertKvRow("市场 / 价格源", "${item.market} / ${item.priceSource}")
            AlertKvRow("条件 / 阈值", "${item.condition} / ${item.threshold}")
            AlertKvRow("触发方式", "${item.triggerMode} · ${item.channels.joinToString()}")
            AlertKvRow("状态 / 通知", "${item.status} / ${item.deliveryStatus}")
            AlertKvRow("修订版 / active上限", "${item.revision} / ${item.limit.activePerUser}")
            AlertKvRow("最后指数价", item.lastObservedPrice ?: "未观察")
            AlertKvRow("触发价", item.triggeredPrice ?: "未触发")
            AlertKvRow("备注", item.note ?: "无")
            Text("仅查看契约事实；创建、编辑、删除需要独立入口与复核。不执行交易。", style = lb(FontSize.small), color = p.textSecondary)
        }
    }
}

@Composable private fun AlertEditorDialog(vm: PriceAlertViewModel, onDialogWindow: (android.view.Window?) -> Unit) {
    val draft = vm.editor ?: return
    click.cowa.linbit.feature.auth.LbSheet(if (draft.alertId == null) "创建价格预警" else "编辑价格预警", { vm.cancelAlertDialogs() }, showClose = true, onWindowReady = onDialogWindow) {
        Column(Modifier.padding(Spacing.pageH), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text("spot · INDEX · once · in_app；不执行交易", color = LB.p.textSecondary)
            LbTextField(draft.symbol, { vm.editDraft { copy(symbol = it) } }, "交易对（例如 BTCUSDT）", testTag = "alert-field-symbol", enabled = draft.alertId == null && !vm.mutationBusy, keyboardType = androidx.compose.ui.text.input.KeyboardType.Ascii)
            if (draft.alertId == null) {
                AlertKvRow("条件", if (draft.condition == "gte") "gte / ≥" else "lte / ≤")
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    LbButton("≥", kind = if (draft.condition == "gte") ButtonKind.PRIMARY else ButtonKind.OUTLINE, enabled = !vm.mutationBusy) { vm.editDraft { copy(condition = "gte") } }
                    LbButton("≤", kind = if (draft.condition == "lte") ButtonKind.PRIMARY else ButtonKind.OUTLINE, enabled = !vm.mutationBusy) { vm.editDraft { copy(condition = "lte") } }
                }
            } else AlertKvRow("交易对 / revision", "${draft.symbol} / ${draft.revision}")
            LbTextField(draft.threshold, { vm.editDraft { copy(threshold = it) } }, "Decimal 正阈值字符串", testTag = "alert-field-threshold", enabled = !vm.mutationBusy, keyboardType = androidx.compose.ui.text.input.KeyboardType.Decimal)
            LbTextField(draft.note, { vm.editDraft { copy(note = it) } }, "备注（≤120字符）", testTag = "alert-field-note", enabled = !vm.mutationBusy)
            AlertKvRow("启用", draft.enabled.toString())
            LbButton(if (draft.enabled) "设为启用 = false" else "设为启用 = true", kind = ButtonKind.OUTLINE, enabled = !vm.mutationBusy) { vm.editDraft { copy(enabled = !enabled) } }
            Text("50额度只以契约与服务端响应为准；额度服务判断，不在客户端猜测。", color = LB.p.textSecondary, style = lb(FontSize.small))
            vm.mutationError?.let {
                Text(it.message, color = LB.p.down)
                it.retryAfter?.let { secs -> Text("Retry-After：${vm.retryRemaining} 秒后允许再试（服务值 $secs 秒）", color = LB.p.down) }
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                LbButton("取消", Modifier.weight(1f), kind = ButtonKind.OUTLINE, enabled = !vm.mutationBusy, testTag = "alert-cancel") { vm.cancelAlertDialogs() }
                LbButton(if (vm.mutationBusy) "保存中…" else "复核并保存", Modifier.weight(1f), enabled = vm.canSave(), testTag = "alert-save") { vm.saveAlert() }
            }
        }
    }
}

@Composable private fun AlertDeleteDialog(vm: PriceAlertViewModel, onDialogWindow: (android.view.Window?) -> Unit) {
    val item = vm.pendingDelete ?: return
    click.cowa.linbit.feature.auth.LbSheet("独立确认删除", { vm.cancelAlertDialogs() }, showClose = true, onWindowReady = onDialogWindow) {
        Column(Modifier.padding(Spacing.pageH), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Text("软删除 ${item.symbol} · ${item.alertId}；revision ${item.revision}。删除不会执行交易。")
            vm.mutationError?.let {
                Text(it.message, color = LB.p.down)
                it.retryAfter?.let { secs -> Text("Retry-After：${vm.retryRemaining} 秒后允许再试（服务值 $secs 秒）", color = LB.p.down) }
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                LbButton("取消", Modifier.weight(1f), enabled = !vm.mutationBusy, kind = ButtonKind.OUTLINE, testTag = "alert-delete-cancel") { vm.cancelAlertDialogs() }
                LbButton(if (vm.mutationBusy) "删除中…" else "确认软删除", Modifier.weight(1f), enabled = vm.canSave(), testTag = "alert-delete-confirm") { vm.confirmDelete() }
            }
        }
    }
}

@Composable private fun AlertKvRow(label: String, value: String) {
    val p = LB.p
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(label, Modifier.weight(0.85f), style = lb(FontSize.small), color = p.textSecondary)
        Text(value, Modifier.weight(1.15f), style = lb(FontSize.small), color = p.textPrimary)
    }
}
