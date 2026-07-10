package com.makia98.notice

import android.app.Application
import com.makia98.notice.data.InboxStore
import com.makia98.notice.data.ProfileStore
import com.makia98.notice.notify.NoticeChannels
import okhttp3.OkHttpClient
import java.util.concurrent.TimeUnit

class NoticeApplication : Application() {

    lateinit var profileStore: ProfileStore
        private set

    lateinit var inboxStore: InboxStore
        private set

    val httpClient: OkHttpClient by lazy {
        OkHttpClient.Builder()
            .connectTimeout(15, TimeUnit.SECONDS)
            // Default read timeout; the SSE client overrides it per-call to act
            // as the heartbeat watchdog.
            .readTimeout(75, TimeUnit.SECONDS)
            .retryOnConnectionFailure(true)
            .build()
    }

    override fun onCreate() {
        super.onCreate()
        NoticeChannels.create(this)
        profileStore = ProfileStore(this)
        inboxStore = InboxStore(this)
    }
}
