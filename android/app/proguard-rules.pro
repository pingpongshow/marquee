# kotlinx.serialization models in the generated API client.
-keepattributes *Annotation*, InnerClasses
-keep,includedescriptorclasses class app.marquee.api.**$$serializer { *; }
-keepclassmembers class app.marquee.api.** { *** Companion; }
-keepclasseswithmembers class app.marquee.api.** { kotlinx.serialization.KSerializer serializer(...); }
-dontwarn org.slf4j.**
