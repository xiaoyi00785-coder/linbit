package click.cowa.linbit.feature.home

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import click.cowa.linbit.core.generated.ApiAnnouncement
import click.cowa.linbit.core.network.*
import click.cowa.linbit.feature.more.*
import click.cowa.linbit.ui.components.*
import click.cowa.linbit.ui.theme.*
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import javax.inject.Inject

data class AnnouncementState(val item: ApiAnnouncement?=null,val loading: Boolean=true,val error: String?=null)
@HiltViewModel
class AnnouncementViewModel @Inject constructor(private val api: LinbitApi): ViewModel() {
    private val _state=MutableStateFlow(AnnouncementState())
    val state=_state.asStateFlow()
    private var job: Job?=null
    private var epoch=0L
    fun load(id: Long) {
        stop();val lease=epoch
        _state.value=AnnouncementState()
        job=viewModelScope.launch {
            try {
                val item=apiCall { api.announcement(id) };ensureActive()
                if (lease==epoch) _state.value=if(item.id==id) AnnouncementState(item,false) else AnnouncementState(loading=false,error="公告数据不匹配")
            } catch(e: CancellationException) { throw e }
            catch(e: Throwable) { if(lease==epoch) _state.value=AnnouncementState(loading=false,error=e.lbMessage()) }
        }
    }
    fun stop() { epoch++;job?.cancel();job=null }
}

@Composable
internal fun AnnouncementDetailScreen(id: Long,onBack: ()->Unit,vm: AnnouncementViewModel=hiltViewModel()) {
    val state by vm.state.collectAsStateWithLifecycle()
    DisposableEffect(id,vm) { vm.load(id);onDispose { vm.stop() } }
    AnnouncementDetailContent(state,onBack) { vm.load(id) }
}

@Composable
internal fun AnnouncementDetailContent(state: AnnouncementState,onBack: ()->Unit,onRetry: ()->Unit) {
    MoreFrame("公告详情",onBack) {
        when {
            state.loading -> MoreLoading(Modifier.weight(1f))
            state.error!=null -> MoreError(state.error,onRetry,Modifier.weight(1f))
            state.item!=null -> Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(Spacing.pageH),verticalArrangement=Arrangement.spacedBy(16.dp)) {
                Text(state.item.title ?: "--",style=lb(FontSize.title,FontWeight.SemiBold),color=LB.p.textPrimary)
                Text(announcementTime(state.item),style=lbNum(FontSize.small),color=LB.p.textTertiary)
                Text(state.item.body ?: "--",style=lb(FontSize.body),color=LB.p.textPrimary)
            }
        }
    }
}

internal fun announcementTime(a: ApiAnnouncement): String = a.publishAt?.takeIf { it.isNotBlank() }?.let {
    runCatching { java.time.Instant.parse(it).atZone(java.time.ZoneId.systemDefault()).format(java.time.format.DateTimeFormatter.ofPattern("yyyy-MM-dd HH:mm:ss")) }.getOrDefault(it)
} ?: a.date ?: "--"
