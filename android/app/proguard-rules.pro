# Keep Gson reflection models.
-keep class com.makia98.notice.sse.** { *; }
-keepclassmembers,allowobfuscation class * {
  @com.google.gson.annotations.SerializedName <fields>;
}
