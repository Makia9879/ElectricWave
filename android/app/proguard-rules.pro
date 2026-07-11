# Keep Gson reflection models.
-keep class com.makia98.electricwave.sse.** { *; }
-keepclassmembers,allowobfuscation class * {
  @com.google.gson.annotations.SerializedName <fields>;
}
