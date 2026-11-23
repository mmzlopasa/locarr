package net.scyde.locarr.notify

import android.Manifest
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.content.ContextCompat
import androidx.work.Constraints
import androidx.work.CoroutineWorker
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import net.scyde.locarr.MainActivity
import net.scyde.locarr.R
import net.scyde.locarr.api.Api
import net.scyde.locarr.api.ApiException
import net.scyde.locarr.data.DataStoreKeyValue
import net.scyde.locarr.data.KeystoreTokenCipher
import net.scyde.locarr.data.ServerAccount
import net.scyde.locarr.data.Servers
import net.scyde.locarr.ui.notificationText
import java.util.concurrent.TimeUnit

/**
 * Notifications without Firebase (D-096): every 15 minutes (the shortest WorkManager period)
 * and whenever the app opens, ask each signed-in server for notifications newer than the last
 * one seen (`GET /notifications?since=`) and post them locally.
 */
class NotificationWorker(ctx: Context, params: WorkerParameters) : CoroutineWorker(ctx, params) {
    override suspend fun doWork(): Result {
        val servers = Servers(DataStoreKeyValue(applicationContext), KeystoreTokenCipher())
        for (acc in servers.all()) {
            val token = servers.token(acc) ?: continue
            try {
                poll(servers, acc, Api(acc.url, token))
            } catch (e: ApiException) {
                // A refused token is handled when the app opens; other errors wait for the next run.
                if (e.status == 401) continue
            } catch (_: Exception) {
                // Server unreachable (away from home): try again next time.
            }
        }
        return Result.success()
    }

    private suspend fun poll(servers: Servers, acc: ServerAccount, api: Api) {
        val list = if (acc.lastNotificationId > 0) api.notifications(since = acc.lastNotificationId, limit = 50) else api.notifications(limit = 1)
        val plan = PollLogic.plan(acc.lastNotificationId, list.items, list.latestId)
        plan.show.forEach { post(acc, it.id.toInt(), PollLogic.channelFor(it.type), it.title, notificationText(applicationContext, it), PollLogic.routeFor(it)) }
        if (plan.summarized > 0) {
            val text = applicationContext.resources.getQuantityString(R.plurals.notify_more, plan.summarized, plan.summarized)
            post(acc, ("summary" + acc.id).hashCode(), Channels.REQUESTS, acc.name.ifBlank { applicationContext.getString(R.string.app_name) }, text, "notifications")
        }
        servers.setLastNotification(acc.id, plan.newLastId)
    }

    private fun post(acc: ServerAccount, id: Int, channel: String, title: String, text: String, route: String) {
        val ctx = applicationContext
        if (Build.VERSION.SDK_INT >= 33 && ContextCompat.checkSelfPermission(ctx, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) return
        val intent = Intent(Intent.ACTION_VIEW, Uri.parse(DeepLink(acc.id, route).toUri()), ctx, MainActivity::class.java)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP)
        val pi = PendingIntent.getActivity(ctx, id, intent, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
        val n = NotificationCompat.Builder(ctx, channel)
            .setSmallIcon(R.drawable.ic_notification)
            .setColor(ContextCompat.getColor(ctx, R.color.accent))
            .setContentTitle(title)
            .setContentText(text)
            .setStyle(NotificationCompat.BigTextStyle().bigText(text))
            .setContentIntent(pi)
            .setAutoCancel(true)
            .setGroup("locarr-" + acc.id)
            .build()
        NotificationManagerCompat.from(ctx).notify("locarr-" + acc.id, id, n)
    }

    companion object {
        private const val PERIODIC = "notifications-poll"
        private const val NOW = "notifications-now"
        private val online = Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build()

        fun schedule(ctx: Context) {
            val req = PeriodicWorkRequestBuilder<NotificationWorker>(15, TimeUnit.MINUTES).setConstraints(online).build()
            WorkManager.getInstance(ctx).enqueueUniquePeriodicWork(PERIODIC, ExistingPeriodicWorkPolicy.KEEP, req)
        }

        fun runNow(ctx: Context) {
            val req = OneTimeWorkRequestBuilder<NotificationWorker>().setConstraints(online).build()
            WorkManager.getInstance(ctx).enqueueUniqueWork(NOW, ExistingWorkPolicy.REPLACE, req)
        }

        fun createChannels(ctx: Context) {
            val nm = ctx.getSystemService(NotificationManager::class.java) ?: return
            listOf(
                Triple(Channels.REQUESTS, R.string.channel_requests, R.string.channel_requests_hint),
                Triple(Channels.NEW_REQUESTS, R.string.channel_new_requests, R.string.channel_new_requests_hint),
                Triple(Channels.ISSUES, R.string.channel_issues, R.string.channel_issues_hint),
            ).forEach { (id, name, hint) ->
                nm.createNotificationChannel(NotificationChannel(id, ctx.getString(name), NotificationManager.IMPORTANCE_DEFAULT).apply { description = ctx.getString(hint) })
            }
        }
    }
}
