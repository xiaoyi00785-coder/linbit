package click.cowa.linbit.feature.markets

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import click.cowa.linbit.core.network.*
import click.cowa.linbit.ui.components.*
import click.cowa.linbit.ui.theme.*
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.serialization.Serializable
import javax.inject.Inject

@Serializable data class ApiAcademyCategory(val id: String, val name: String)
@Serializable data class ApiAcademyCategories(val items: List<ApiAcademyCategory> = emptyList())
@Serializable data class ApiAcademyArticle(val id: String, val title: String, val summary: String? = null, val category: String? = null, val categoryId: String? = null, val publishedAt: String? = null, val body: String? = null, val format: String? = "plain")
@Serializable data class ApiAcademyArticles(val items: List<ApiAcademyArticle> = emptyList(), val hasMore: Boolean = false)
@Serializable data class ApiSearchSymbol(val symbol: String)
@Serializable data class ApiMarketSearch(val query: String? = null, val symbols: List<kotlinx.serialization.json.JsonElement> = emptyList(), val articles: List<ApiAcademyArticle> = emptyList())

@HiltViewModel
class AcademyViewModel @Inject constructor(private val api: LinbitApi) : ViewModel() {
    var categories by mutableStateOf<List<ApiAcademyCategory>>(emptyList()); private set
    var category by mutableStateOf<String?>(null); private set
    var articles by mutableStateOf<List<ApiAcademyArticle>?>(null); private set
    var error by mutableStateOf<String?>(null); private set
    var detail by mutableStateOf<ApiAcademyArticle?>(null)
    private var searchJob: Job? = null
    fun load() { error = null; viewModelScope.launch {
        runCatching { apiCall { api.academyCategories() } }.onSuccess { categories = it.items }
        loadArticles(category) } }
    fun loadArticles(c: String?) { category = c; articles = null; error = null; viewModelScope.launch {
        runCatching { apiCall { api.academyArticles(category = c) } }.onSuccess { articles = it.items }.onFailure { error = it.lbMessage() } } }
    /** 搜索框输入时走 /market/search，只取 articles。 */
    private var lastQuery = ""
    fun search(q: String) { if (q == lastQuery) return; val prev = lastQuery; lastQuery = q; searchJob?.cancel(); if (q.isBlank()) { if (prev.isNotBlank()) loadArticles(category); return }
        searchJob = viewModelScope.launch { delay(300); articles = null
            runCatching { apiCall { api.marketSearch(q.take(64)) } }.onSuccess { articles = it.articles }.onFailure { error = it.lbMessage() } } }
    fun open(a: ApiAcademyArticle) { detail = a; viewModelScope.launch { runCatching { apiCall { api.academyArticle(a.id) } }.onSuccess { detail = it }.onFailure { error = it.lbMessage() } } }
}

@Composable
fun AcademyTab(query: String, vm: AcademyViewModel = hiltViewModel()) {
    val p = LB.p
    LaunchedEffect(Unit) { vm.load() }
    LaunchedEffect(query) { vm.search(query) }
    vm.detail?.let { d ->
        Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(Spacing.pageH)) {
            Text("‹ 返回", Modifier.pressable { vm.detail = null }.padding(vertical = 8.dp), color = p.brand)
            Text(d.title, style = lb(FontSize.title, FontWeight.Bold), color = p.textPrimary)
            d.publishedAt?.let { Text(it.take(10), Modifier.padding(vertical = 6.dp), style = lb(FontSize.small), color = p.textTertiary) }
            if (d.body == null) CircularProgressIndicator(Modifier.padding(24.dp)) else Text(d.body, style = lb(FontSize.body), color = p.textPrimary)
        }
        return
    }
    Column(Modifier.fillMaxSize()) {
        if (vm.categories.isNotEmpty() && query.isBlank()) PrimaryTabs(listOf("全部") + vm.categories.map { it.name }, vm.categories.indexOfFirst { it.id == vm.category } + 1,
            { vm.loadArticles(if (it == 0) null else vm.categories[it - 1].id) }, Modifier.padding(horizontal = Spacing.pageH), size = FontSize.small)
        val list = vm.articles
        when {
            vm.error != null -> EmptyState(vm.error!!, Modifier.padding(top = 40.dp)) { ChipButton("重试", onClick = vm::load) }
            list == null -> Box(Modifier.fillMaxWidth().padding(top = 40.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
            list.isEmpty() -> EmptyState(if (query.isBlank()) "暂无文章" else "未找到相关文章", Modifier.padding(top = 40.dp))
            else -> LazyColumn(contentPadding = PaddingValues(Spacing.pageH)) { items(list, key = { it.id }) { a ->
                Column(Modifier.fillMaxWidth().pressable(scale = 1f) { vm.open(a) }.padding(vertical = 12.dp)) {
                    Text(a.title, style = lb(FontSize.body, FontWeight.SemiBold), color = p.textPrimary, maxLines = 2)
                    a.summary?.let { Text(it, Modifier.padding(top = 4.dp), style = lb(FontSize.caption), color = p.textSecondary, maxLines = 2) }
                    a.publishedAt?.let { Text(it.take(10), Modifier.padding(top = 4.dp), style = lb(FontSize.small), color = p.textTertiary) }
                } } }
        }
    }
}
