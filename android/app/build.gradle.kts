plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlin.plugin.compose")
    id("org.jetbrains.kotlin.plugin.serialization")
}

/**
 * versionCode from the version: major*10000 + minor*100 + patch (1.2.3 → 10203), so a newer
 * release always has a higher code and installs over the previous one.
 */
fun versionCodeOf(name: String): Int {
    val parts = Regex("""^v?(\d+)\.(\d+)\.(\d+)""").find(name)?.groupValues ?: return 1
    return (parts[1].toInt() * 10000 + parts[2].toInt() * 100 + parts[3].toInt()).coerceAtLeast(1)
}
val appVersionName = System.getenv("VERSION_NAME")?.ifBlank { null } ?: "0.0.0-dev"

android {
    namespace = "net.scyde.locarr"
    compileSdk = 35
    buildToolsVersion = "35.0.0"

    defaultConfig {
        applicationId = "net.scyde.locarr"
        minSdk = 26
        targetSdk = 35
        versionCode = versionCodeOf(appVersionName)
        versionName = appVersionName
    }

    signingConfigs {
        // Release builds are signed with the project key when ANDROID_KEYSTORE points to it
        // (CI restores it from the ANDROID_KEYSTORE_BASE64 secret). Without it the debug key is
        // used, so the .apk still installs; it cannot update an app signed with the real key.
        val ks = System.getenv("ANDROID_KEYSTORE")?.ifBlank { null }
        if (ks != null && !file(ks).exists()) throw GradleException("ANDROID_KEYSTORE is set to $ks, but that file does not exist")
        if (ks != null) {
            create("release") {
                storeFile = file(ks)
                storePassword = System.getenv("ANDROID_KEYSTORE_PASSWORD")
                keyAlias = System.getenv("ANDROID_KEY_ALIAS")
                keyPassword = System.getenv("ANDROID_KEY_PASSWORD")?.ifBlank { null } ?: System.getenv("ANDROID_KEYSTORE_PASSWORD")
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            signingConfig = signingConfigs.findByName("release") ?: signingConfigs.getByName("debug")
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions { jvmTarget = "17" }
    buildFeatures { compose = true; buildConfig = true }
    packaging { resources { excludes += "/META-INF/{AL2.0,LGPL2.1}" } }
    lint {
        abortOnError = true
        warningsAsErrors = false
        // Translations of server codes are generated from the web client (tools/gen-codes.py);
        // not every code is shown on every screen.
        disable += setOf("UnusedResources")
    }
    testOptions { unitTests.isReturnDefaultValues = true }
}

dependencies {
    val composeBom = platform("androidx.compose:compose-bom:2024.12.01")
    implementation(composeBom)
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.ui:ui-tooling-preview")
    implementation("androidx.compose.material3:material3")
    implementation("androidx.compose.material:material-icons-extended")
    implementation("androidx.activity:activity-compose:1.9.3")
    implementation("androidx.navigation:navigation-compose:2.8.5")
    implementation("androidx.lifecycle:lifecycle-viewmodel-compose:2.8.7")
    implementation("androidx.lifecycle:lifecycle-runtime-compose:2.8.7")
    implementation("androidx.lifecycle:lifecycle-process:2.8.7")
    implementation("androidx.datastore:datastore-preferences:1.1.1")
    implementation("androidx.core:core-ktx:1.15.0")
    implementation("androidx.appcompat:appcompat:1.7.0")
    implementation("androidx.browser:browser:1.8.0")
    implementation("androidx.work:work-runtime-ktx:2.10.0")
    implementation("com.squareup.okhttp3:okhttp:4.12.0")
    implementation("org.jetbrains.kotlinx:kotlinx-serialization-json:1.7.3")
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.9.0")
    implementation("io.coil-kt:coil-compose:2.7.0")
    debugImplementation("androidx.compose.ui:ui-tooling")

    testImplementation("junit:junit:4.13.2")
    testImplementation("com.squareup.okhttp3:mockwebserver:4.12.0")
    testImplementation("org.jetbrains.kotlinx:kotlinx-coroutines-test:1.9.0")
}
