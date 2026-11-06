plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.kotlin.serialization)
    alias(libs.plugins.ksp)
    alias(libs.plugins.hilt)
}

android {
    namespace = "click.cowa.linbit"
    compileSdk = 35
    buildToolsVersion = "35.0.0"

    defaultConfig {
        applicationId = "click.cowa.linbit"
        minSdk = 26
        targetSdk = 35
        testInstrumentationRunner = "click.cowa.linbit.instrumentation.ProductionLoopbackRunner"
        versionCode = 2
        versionName = "0.2.0"

        // 与 iOS 的 LBAPIBase / LBWSURL 一致；可用 -PlinbitApiBase=... 覆盖（例如指向本地 gateway）
        val apiBase = (project.findProperty("linbitApiBase") as String?) ?: "https://api.cowa.click"
        val wsUrl = (project.findProperty("linbitWsUrl") as String?) ?: "wss://api.cowa.click/ws"
        buildConfigField("String", "API_BASE", "\"$apiBase\"")
        buildConfigField("String", "WS_URL", "\"$wsUrl\"")
        // 后端按 UA 识别客户端（REST 与 WS 都带），iOS 为 Linbit-iOS/1.0
        buildConfigField("String", "USER_AGENT", "\"Linbit-Android/$versionName\"")
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            // 签名尚未配置：release 产物需要 keystore（见 mobile/android/README.md）
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions {
        jvmTarget = "17"
    }
    buildFeatures {
        compose = true
        buildConfig = true
    }
    lint {
        abortOnError = true
        warningsAsErrors = false
        checkDependencies = false
        // 生成代码与依赖版本提示不阻塞 CI
        disable += setOf("GradleDependency", "NewerVersionAvailable", "AndroidGradlePluginVersion")
    }
    testOptions {
        unitTests.isReturnDefaultValues = true
        unitTests.isIncludeAndroidResources = true
        unitTests.all {
            it.maxHeapSize = "2g"
            // 离屏证据：-PoffscreenShots=1 或 -Doffscreen.shots=1
            val shots = (project.findProperty("offscreenShots") as? String)
                ?: System.getProperty("offscreen.shots")
                ?: ""
            it.systemProperty("offscreen.shots", shots)
            setOf("production.evidence", "production.evidence.dir", "legacy.composite.shots").forEach { key ->
                System.getProperty(key)?.let { value -> it.systemProperty(key, value) }
            }
        }
    }
    packaging {
        resources.excludes += "/META-INF/{AL2.0,LGPL2.1}"
    }
}

dependencies {
    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.credentials)
    implementation(libs.androidx.core.splashscreen)
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.lifecycle.runtime.compose)
    implementation(libs.androidx.lifecycle.viewmodel.compose)
    implementation(libs.androidx.navigation.compose)
    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.androidx.compose.ui)
    implementation(libs.androidx.compose.ui.graphics)
    implementation(libs.androidx.compose.ui.tooling.preview)
    implementation(libs.androidx.compose.material3)
    debugImplementation(libs.androidx.compose.ui.tooling)

    implementation(libs.hilt.android)
    ksp(libs.hilt.compiler)
    implementation(libs.hilt.navigation.compose)

    implementation(libs.kotlinx.coroutines.android)
    implementation(libs.kotlinx.serialization.json)
    implementation(libs.retrofit)
    implementation(libs.retrofit.kotlinx.serialization)
    implementation(libs.okhttp)
    implementation(libs.okhttp.logging)

    implementation(libs.androidx.datastore.preferences)
    implementation(libs.androidx.security.crypto)
    implementation(libs.coil.compose)
    implementation(libs.zxing.core)

    testImplementation(libs.junit)
    testImplementation(libs.kotlinx.coroutines.test)
    testImplementation(libs.okhttp.mockwebserver)
    testImplementation(libs.mockito.kotlin)
    testImplementation(libs.robolectric)
    testImplementation(platform(libs.androidx.compose.bom))
    testImplementation(libs.androidx.compose.ui.test.junit4)
    testImplementation(libs.androidx.test.core)
    androidTestImplementation(platform(libs.androidx.compose.bom))
    androidTestImplementation(libs.androidx.compose.ui.test.junit4)
    androidTestImplementation(libs.okhttp.mockwebserver)
    androidTestImplementation("androidx.test.ext:junit:1.2.1")
    androidTestImplementation("androidx.test:runner:1.6.2")
    androidTestImplementation("androidx.test.espresso:espresso-core:3.6.1")
    debugImplementation(libs.androidx.compose.ui.test.manifest)
}


// 本地截图对照（不入库）：存在 shots.gradle.kts 时加载 Robolectric / Compose 测试依赖
file("shots.gradle.kts").takeIf { it.exists() }?.let { apply(from = it) }
