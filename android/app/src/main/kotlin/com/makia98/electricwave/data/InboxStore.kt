package com.makia98.electricwave.data

import android.content.Context
import android.content.SharedPreferences
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey
import com.google.gson.Gson
import com.google.gson.reflect.TypeToken
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * Encrypted, persistent inbox of received notifications.
 *
 * Bodies may contain sensitive content, so records live in
 * EncryptedSharedPreferences (AES256-GCM values, AES256-SIV keys, master key
 * wrapped by AndroidKeystore) — never plaintext. If the Keystore cannot be
 * opened, the inbox degrades to in-memory only (never plaintext), matching the
 * policy in [ProfileStore]. The list is capped to [MAX_ITEMS] newest entries.
 */
class InboxStore(context: Context) {

    private val appContext = context.applicationContext
    private val gson = Gson()

    private val prefs: SharedPreferences? = try {
        val masterKey = MasterKey.Builder(appContext)
            .setKeyScheme(MasterKey.KeyScheme.AES256_GCM)
            .build()
        EncryptedSharedPreferences.create(
            appContext,
            FILE,
            masterKey,
            EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
            EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM,
        )
    } catch (t: Throwable) {
        // Keystore unavailable: stay in-memory only. Never write plaintext.
        null
    }

    private val _notifications = MutableStateFlow(load())
    val notifications: StateFlow<List<ReceivedNotification>> = _notifications.asStateFlow()

    /** Prepends [item], dedups by id, caps to [MAX_ITEMS]. */
    fun add(item: ReceivedNotification) {
        val updated = (listOf(item) + _notifications.value)
            .distinctBy { it.notificationId }
            .take(MAX_ITEMS)
        persist(updated)
    }

    fun markRead(id: String) =
        persist(_notifications.value.map { if (it.notificationId == id) it.copy(read = true) else it })

    fun markAllRead() = persist(_notifications.value.map { it.copy(read = true) })

    fun clear() = persist(emptyList())

    fun get(id: String): ReceivedNotification? =
        _notifications.value.firstOrNull { it.notificationId == id }

    private fun persist(list: List<ReceivedNotification>) {
        val p = prefs
        if (p == null) {
            _notifications.value = list
            return
        }
        p.edit().putString(KEY, gson.toJson(list)).apply()
        _notifications.value = list
    }

    private fun load(): List<ReceivedNotification> = try {
        prefs?.getString(KEY, null)?.let {
            val type = object : TypeToken<List<ReceivedNotification>>() {}.type
            gson.fromJson<List<ReceivedNotification>>(it, type) ?: emptyList()
        } ?: emptyList()
    } catch (t: Throwable) {
        emptyList()
    }

    companion object {
        private const val FILE = "electricwave_inbox.xml"
        private const val KEY = "notifications"
        private const val MAX_ITEMS = 200
    }
}
