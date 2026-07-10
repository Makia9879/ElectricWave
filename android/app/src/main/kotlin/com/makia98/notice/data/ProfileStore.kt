package com.makia98.notice.data

import android.content.Context
import android.content.SharedPreferences
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey
import com.google.gson.Gson
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * Keystore-backed secret store.
 *
 * Everything lives inside EncryptedSharedPreferences (AES256-GCM values, AES256
 * SIV keys, master key wrapped by AndroidKeystore). This guarantees the
 * `receiver_identity_token` is never stored in plaintext SharedPreferences.
 *
 * SECURITY: if the Keystore-backed store cannot be opened (rare; e.g. key
 * invalidated by a lock-screen credential change after a restore), the store
 * degrades to an IN-MEMORY-ONLY mode. It NEVER falls back to plaintext prefs:
 * [save] keeps the profile only in the live StateFlow without persisting, and
 * [secureStorageAvailable] reports false so the UI can warn the user. Secrets
 * are therefore never written to disk unencrypted.
 *
 * The store exposes two reactive [StateFlow]s (one for the profile, one for the
 * runtime snapshot) so both the foreground service (writer) and the UI (reader)
 * share a single source of truth in-process.
 */
class ProfileStore(context: Context) {

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
        // Keystore unavailable: stay in-memory only. Never fall back to plaintext.
        null
    }

    /** False when the encrypted store could not be opened (secrets won't persist). */
    val secureStorageAvailable: Boolean = prefs != null

    private val _profile = MutableStateFlow(loadProfileInternal())
    val profile: StateFlow<Profile> = _profile.asStateFlow()

    private val _runState = MutableStateFlow(loadRunStateInternal())
    val runState: StateFlow<RunState> = _runState.asStateFlow()

    fun current(): Profile = _profile.value

    fun save(profile: Profile) {
        // Persist only to the encrypted store; never to plaintext prefs.
        prefs?.edit()?.putString(KEY_PROFILE, gson.toJson(profile))?.apply()
        _profile.value = profile
    }

    fun currentRunState(): RunState = _runState.value

    /** Persists and publishes a new runtime snapshot (token-free). */
    fun publishRunState(state: RunState) {
        prefs?.edit()?.putString(KEY_RUN_STATE, gson.toJson(state))?.apply()
        _runState.value = state
    }

    fun updateRunState(transform: (RunState) -> RunState) {
        val next = transform(_runState.value)
        publishRunState(next)
    }

    private fun loadProfileInternal(): Profile = try {
        prefs?.getString(KEY_PROFILE, null)?.let {
            gson.fromJson(it, Profile::class.java)
        } ?: Profile()
    } catch (t: Throwable) {
        Profile()
    }

    private fun loadRunStateInternal(): RunState = try {
        prefs?.getString(KEY_RUN_STATE, null)?.let {
            gson.fromJson(it, RunState::class.java)
        } ?: RunState()
    } catch (t: Throwable) {
        RunState()
    }

    companion object {
        private const val FILE = "makia_notice_secure.xml"
        private const val KEY_PROFILE = "profile"
        private const val KEY_RUN_STATE = "run_state"
    }
}
