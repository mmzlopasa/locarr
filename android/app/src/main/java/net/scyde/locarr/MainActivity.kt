package net.scyde.locarr

import android.content.Intent
import android.graphics.Color
import android.os.Bundle
import androidx.activity.SystemBarStyle
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.appcompat.app.AppCompatActivity
import androidx.appcompat.app.AppCompatDelegate
import androidx.core.os.LocaleListCompat
import net.scyde.locarr.notify.DeepLink
import net.scyde.locarr.ui.LocarrRoot

/** AppCompatActivity so the in-app language also applies on Android 8–12. */
class MainActivity : AppCompatActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val bars = SystemBarStyle.dark(Color.TRANSPARENT)
        enableEdgeToEdge(statusBarStyle = bars, navigationBarStyle = bars)
        val state = (application as LocarrApp).state
        if (savedInstanceState == null) deliver(intent)
        setContent { LocarrRoot(state) }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        deliver(intent)
    }

    private fun deliver(intent: Intent?) {
        val link = DeepLink.parse(intent?.dataString) ?: return
        (application as LocarrApp).state.pendingLink = link
    }
}

/** Switches the app language; empty follows the phone. */
fun applyLanguage(lang: String?) {
    val tags = if (lang.isNullOrBlank()) LocaleListCompat.getEmptyLocaleList() else LocaleListCompat.forLanguageTags(lang)
    AppCompatDelegate.setApplicationLocales(tags)
}
