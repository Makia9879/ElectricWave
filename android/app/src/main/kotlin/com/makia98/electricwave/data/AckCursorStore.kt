package com.makia98.electricwave.data

import android.content.Context
import android.content.SharedPreferences
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * Persistent, Keystore-backed store for the highest acked event_id
 * (contract §10.2). Mirrors the [ProfileStore]/[InboxStore] encrypted-prefs
 * pattern: AES256-GCM values, AES256-SIV keys, master key wrapped by
 * AndroidKeystore. If the Keystore cannot be opened, the store degrades to
 * in-memory only (never plaintext), matching the policy in [ProfileStore].
 *
 * The value `0L` means "nothing acked yet". It is sent on reconnect as
 * `Last-Event-ID` / `X-Receiver-Ack` (omitted when 0).
 */
class AckCursorStore(context: Context) {

    private val appContext = context.applicationContext

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

    /** False when the encrypted store could not be opened (cursor won't persist). */
    val secureStorageAvailable: Boolean = prefs != null

    private val _ackedEventId = MutableStateFlow(load())
    /** Reactive view of the highest acked event_id (0 = none). */
    val ackedEventId: StateFlow<Long> = _ackedEventId.asStateFlow()

    fun current(): Long = _ackedEventId.value

    /** Persists and publishes [value] (clamped to >= 0). */
    fun set(value: Long) {
        val v = value.coerceAtLeast(0L)
        prefs?.edit()?.putLong(KEY_ACKED_EVENT_ID, v)?.apply()
        _ackedEventId.value = v
    }

    /**
     * Convenience: advances to [eventId] only if newer, persists, and returns the
     * new value. Safe to call from the SSE callback thread.
     */
    fun advanceTo(eventId: Long): Long {
        val next = maxOf(_ackedEventId.value, eventId.coerceAtLeast(0L))
        if (next != _ackedEventId.value) set(next)
        return next
    }

    private fun load(): Long = try {
        prefs?.getLong(KEY_ACKED_EVENT_ID, 0L) ?: 0L
    } catch (t: Throwable) {
        0L
    }

    companion object {
        private const val FILE = "electricwave_cursor.xml"
        private const val KEY_ACKED_EVENT_ID = "acked_event_id"
    }
}
