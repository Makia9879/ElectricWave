package com.makia98.notice.util

import android.util.Log

/**
 * Single logging entry point. Callers MUST never pass secrets here.
 * `receiver_identity_token` is treated as a secret and is never logged anywhere
 * in the codebase.
 */
object Logx {
    private const val TAG = "MakiaNotice"

    fun d(msg: String) {
        Log.d(TAG, msg)
    }

    fun i(msg: String) {
        Log.i(TAG, msg)
    }

    fun w(msg: String, t: Throwable? = null) {
        if (t != null) Log.w(TAG, msg, t) else Log.w(TAG, msg)
    }

    fun e(msg: String, t: Throwable? = null) {
        if (t != null) Log.e(TAG, msg, t) else Log.e(TAG, msg)
    }
}
