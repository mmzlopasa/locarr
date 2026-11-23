package net.scyde.locarr.data

import android.content.Context
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.flow.first

private val Context.store by preferencesDataStore("locarr")

/** [KeyValue] on Jetpack DataStore (one file in the app's private storage, not backed up). */
class DataStoreKeyValue(private val context: Context) : KeyValue {
    override suspend fun read(key: String): String? = context.store.data.first()[stringPreferencesKey(key)]
    override suspend fun write(key: String, value: String?) {
        context.store.edit { if (value == null) it.remove(stringPreferencesKey(key)) else it[stringPreferencesKey(key)] = value }
    }
}
