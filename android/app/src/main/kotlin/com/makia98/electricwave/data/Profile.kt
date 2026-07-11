package com.makia98.electricwave.data

/**
 * A single receiver profile. The data model is intentionally profile-oriented
 * to leave room for multiple profiles later, even though the MVP UI only
 * exposes one active profile.
 *
 * SECURITY: [identityToken] is a secret. It is stored only inside
 * EncryptedSharedPreferences (Keystore-backed) and is NEVER included in logs,
 * crash reports, or UI display. The token's `toString()` must not surface it;
 * callers that need a redacted form use [Profile.redactedToken].
 */
data class Profile(
    val serverEndpoint: String = "",
    val receiverId: String = "",
    val identityToken: String = "",
    val enabled: Boolean = false,
    val providerType: String = PROVIDER_TYPE_SELF_HOSTED_SSE,
    /** Off by default. When on, HTTP to local/private addresses is permitted. */
    val devMode: Boolean = false,
    /**
     * When true, notification lock-screen visibility is forced PRIVATE.
     * Defaults to true to protect potentially sensitive bodies.
     */
    val hideSensitiveContent: Boolean = true,
) {
    val isValid: Boolean
        get() = serverEndpoint.isNotBlank() &&
            receiverId.isNotBlank() &&
            identityToken.isNotBlank()

    /** True when the profile has the minimum fields to attempt a connection. */
    val isConnectable: Boolean get() = isValid

    /** Never returns the token; used only for diagnostics. */
    val tokenPresent: Boolean get() = identityToken.isNotBlank()

    // Override the data-class toString() so a stray log/exception can never
    // surface the secret token.
    override fun toString(): String =
        "Profile(serverEndpoint=$serverEndpoint, receiverId=$receiverId, " +
            "identityToken=${if (identityToken.isBlank()) "<unset>" else "<redacted>"}, " +
            "enabled=$enabled, providerType=$providerType, devMode=$devMode, " +
            "hideSensitiveContent=$hideSensitiveContent)"

    companion object {
        const val PROVIDER_TYPE_SELF_HOSTED_SSE = "self_hosted_sse"
    }
}
