package net.scyde.locarr.ui

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import net.scyde.locarr.AppState
import net.scyde.locarr.R
import net.scyde.locarr.api.Api
import net.scyde.locarr.api.ApiException
import net.scyde.locarr.api.AuthProvider
import net.scyde.locarr.api.normalizeServerUrl

/** The Locarr mark (same drawing as the web logo). */
@Composable
fun LogoMark(modifier: Modifier = Modifier.size(56.dp)) {
    Canvas(modifier) {
        val u = size.minDimension / 32f
        drawRoundRect(Lc.surface3, cornerRadius = androidx.compose.ui.geometry.CornerRadius(7 * u))
        val p = androidx.compose.ui.graphics.Path().apply { moveTo(10 * u, 7 * u); lineTo(10 * u, 25 * u); lineTo(22 * u, 25 * u) }
        drawPath(p, Lc.accent, style = Stroke(width = 3.5f * u, cap = StrokeCap.Round, join = StrokeJoin.Round))
        drawCircle(Lc.accent, radius = 3 * u, center = Offset(21 * u, 11 * u))
    }
}

@Composable
private fun AuthFrame(title: String, subtitle: String?, content: @Composable ColumnScope.() -> Unit) {
    Box(Modifier.fillMaxSize().systemBarsPadding().imePadding(), contentAlignment = Alignment.TopCenter) {
        Column(
            Modifier.widthIn(max = 440.dp).fillMaxWidth().verticalScroll(rememberScrollState()).padding(24.dp),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            Spacer(Modifier.height(24.dp))
            LogoMark()
            Text(title, style = MaterialTheme.typography.headlineMedium, color = Lc.text, modifier = Modifier.semantics { heading() })
            if (subtitle != null) Text(subtitle, color = Lc.muted, style = MaterialTheme.typography.bodyMedium)
            Spacer(Modifier.height(4.dp))
            content()
        }
    }
}

@Composable
fun fieldColors() = OutlinedTextFieldDefaults.colors(focusedBorderColor = Lc.accent, unfocusedBorderColor = Lc.border, focusedContainerColor = Lc.surface, unfocusedContainerColor = Lc.surface, focusedLabelColor = Lc.accent)

/** Step 1: the server address. Checks that a Locarr answers there before asking for a password. */
@Composable
fun ConnectScreen(state: AppState) {
    var url by rememberSaveable { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var checked by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val notLocarr = stringResource(R.string.connect_not_locarr)
    val badUrl = stringResource(R.string.connect_bad_url)
    val setupFirst = stringResource(R.string.connect_setup_first)
    val unreachable = stringResource(R.string.err_network)

    if (checked != null) { SignInScreen(state, checked!!, onBack = { checked = null }); return }

    AuthFrame(stringResource(R.string.connect_title), stringResource(R.string.connect_subtitle)) {
        OutlinedTextField(
            value = url, onValueChange = { url = it; error = null }, singleLine = true,
            label = { Text(stringResource(R.string.connect_url)) }, placeholder = { Text("http://192.168.1.10:8484") },
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Go),
            isError = error != null, supportingText = { Text(error ?: stringResource(R.string.connect_url_hint)) },
            modifier = Modifier.fillMaxWidth(), colors = fieldColors(),
        )
        Button(
            onClick = {
                val n = normalizeServerUrl(url)
                if (n == null) { error = badUrl; return@Button }
                busy = true
                scope.launch {
                    try {
                        val info = Api(n).info()
                        when {
                            info.product != "Locarr" -> error = notLocarr
                            info.setupRequired -> error = setupFirst
                            else -> checked = n
                        }
                    } catch (e: CancellationException) { throw e } catch (e: Exception) {
                        error = if (e is ApiException) notLocarr else unreachable
                    } finally { busy = false }
                }
            },
            enabled = !busy && url.isNotBlank(), modifier = Modifier.fillMaxWidth().heightIn(min = 50.dp), shape = Shape,
        ) { if (busy) CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp) else Text(stringResource(R.string.connect_continue)) }
        Text(stringResource(R.string.connect_http_note), color = Lc.faint, style = MaterialTheme.typography.bodySmall)
        if (state.accounts.isNotEmpty()) {
            HorizontalDivider(color = Lc.border)
            Text(stringResource(R.string.servers_saved), color = Lc.muted, style = MaterialTheme.typography.labelLarge)
            state.accounts.forEach { a ->
                ListRow(a.name.ifBlank { a.url }, subtitle = listOf(a.username, a.url).filter { it.isNotBlank() }.joinToString(" · "), onClick = { state.scope.launch { state.switchTo(a.id) } })
            }
        }
    }
}

/**
 * Step 2: sign in with a Locarr account or another service's (Streamero), with the two-factor
 * step when the server asks for it (401 totp_required).
 */
@Composable
fun SignInScreen(state: AppState, url: String, onBack: (() -> Unit)? = null) {
    val scope = rememberCoroutineScope()
    var providers by remember { mutableStateOf<List<AuthProvider>>(listOf(AuthProvider("local", "password", "Locarr"))) }
    var provider by rememberSaveable { mutableStateOf("local") }
    var username by rememberSaveable { mutableStateOf(state.account?.username ?: "") }
    var password by rememberSaveable { mutableStateOf("") }
    var code by rememberSaveable { mutableStateOf("") }
    var needCode by rememberSaveable { mutableStateOf(false) }
    var recovery by rememberSaveable { mutableStateOf(false) }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    val canSubmit = !busy && username.isNotBlank() && password.isNotEmpty() && (!needCode || code.isNotBlank())
    val codeFocus = remember { FocusRequester() }
    LaunchedEffect(needCode) { if (needCode) runCatching { codeFocus.requestFocus() } }
    val submit: () -> Unit = submit@{
        if (!canSubmit) return@submit
        busy = true; error = null
        scope.launch {
            try {
                val api = Api(url)
                val totp = code.takeIf { needCode && !recovery }
                val res = if (provider == "local") api.login(username.trim(), password, totp, code.takeIf { needCode && recovery }, state.device)
                else api.providerLogin(provider, username.trim(), password, code.takeIf { needCode }, state.device)
                // On the app's scope: this screen goes away while the server opens.
                state.scope.launch { state.signedIn(url, res) }
            } catch (e: CancellationException) { throw e } catch (e: Exception) {
                if (e is ApiException && e.code == "totp_required") { needCode = true } else error = e
            } finally { busy = false }
        }
    }
    LaunchedEffect(url) { runCatching { Api(url).providers() }.onSuccess { list -> providers = list.filter { it.kind == "password" }.ifEmpty { providers } } }

    AuthFrame(
        stringResource(R.string.signin_title),
        if (state.sessionExpired) stringResource(R.string.signin_expired) else url,
    ) {
        if (providers.size > 1) {
            SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
                providers.forEachIndexed { i, p ->
                    SegmentedButton(selected = provider == p.id, onClick = { provider = p.id; needCode = false; error = null },
                        shape = SegmentedButtonDefaults.itemShape(i, providers.size),
                        colors = SegmentedButtonDefaults.colors(activeContainerColor = Lc.successBg, activeContentColor = Lc.accent, inactiveContainerColor = Lc.surface)) {
                        Text(if (p.id == "local") stringResource(R.string.signin_with_locarr) else stringResource(R.string.signin_with, p.name), maxLines = 1)
                    }
                }
            }
            if (provider != "local") Text(stringResource(R.string.signin_provider_hint, providers.first { it.id == provider }.name), color = Lc.muted, style = MaterialTheme.typography.bodySmall)
        }
        OutlinedTextField(username, { username = it }, label = { Text(stringResource(R.string.signin_username)) }, singleLine = true,
            keyboardOptions = KeyboardOptions(imeAction = ImeAction.Next), modifier = Modifier.fillMaxWidth(), colors = fieldColors())
        OutlinedTextField(password, { password = it }, label = { Text(stringResource(R.string.signin_password)) }, singleLine = true,
            visualTransformation = PasswordVisualTransformation(),
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, imeAction = ImeAction.Done), keyboardActions = KeyboardActions(onDone = { submit() }),
            modifier = Modifier.fillMaxWidth(), colors = fieldColors())
        if (needCode) {
            OutlinedTextField(code, { code = it.trim() }, singleLine = true,
                label = { Text(stringResource(if (recovery) R.string.signin_recovery else R.string.signin_code)) },
                supportingText = { Text(stringResource(R.string.signin_code_hint)) },
                keyboardOptions = KeyboardOptions(keyboardType = if (recovery) KeyboardType.Ascii else KeyboardType.NumberPassword, imeAction = ImeAction.Done),
                keyboardActions = KeyboardActions(onDone = { submit() }),
                modifier = Modifier.fillMaxWidth().focusRequester(codeFocus), colors = fieldColors())
            if (provider == "local") TextButton(onClick = { recovery = !recovery; code = "" }) {
                Text(stringResource(if (recovery) R.string.signin_use_code else R.string.signin_use_recovery))
            }
        }
        error?.let { Text(errorText(it), color = Lc.danger, style = MaterialTheme.typography.bodyMedium) }
        Button(
            onClick = submit,
            enabled = canSubmit,
            modifier = Modifier.fillMaxWidth().heightIn(min = 50.dp), shape = Shape,
        ) { if (busy) CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp) else Text(stringResource(R.string.signin_button)) }
        if (onBack != null) TextButton(onClick = onBack) { Text(stringResource(R.string.signin_other_server)) }
        else TextButton(onClick = { state.scope.launch { state.addServer() } }) { Text(stringResource(R.string.signin_other_server)) }
    }
}
