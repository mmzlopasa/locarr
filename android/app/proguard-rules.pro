# kotlinx.serialization keeps generated serializers
-keepattributes *Annotation*, InnerClasses
-keepclassmembers class kotlinx.serialization.json.** { *** Companion; }
-keep,includedescriptorclasses class net.scyde.locarr.**$$serializer { *; }
-keepclassmembers class net.scyde.locarr.** { *** Companion; kotlinx.serialization.KSerializer serializer(...); }
-dontwarn org.slf4j.**
