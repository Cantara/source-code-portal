import org.jetbrains.kotlin.gradle.dsl.JvmTarget

plugins {
    alias(libs.plugins.kotlin.jvm)
    alias(libs.plugins.kapt)
    application
}

kotlin {
    jvmToolchain(21)
}

val myLib = "com.example:my-lib:${property("myLibVersion")}"
val coroutinesVersion = "1.9.0"

dependencies {
    implementation(project(":lib:core"))
    implementation(project(path = ":tools"))
    implementation(kotlin("stdlib"))
    implementation(kotlin("reflect", "2.0.0"))
    implementation(libs.bundles.ktor)
    implementation(libs.kotlinx.coroutines.core)
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-test:$coroutinesVersion")
    implementation("org.jetbrains.kotlinx:kotlinx-datetime:${libs.versions.coroutines.get()}")
    kapt(libs.dagger.compiler)
    implementation(libs.dagger)
    implementation(libs.managed.lib)
    implementation(libs.does.not.exist)
    implementation(otherCatalog.something)
    testImplementation(more.junit5)
    testImplementation(libs.junit.jupiter)
    implementation(myLib)
    implementation(files("libs/local.jar"))
    runtimeOnly("ch.qos.logback:logback-classic") {
        version { strictly("1.5.6") }
    }
    implementation(enforcedPlatform("io.ktor:ktor-bom:3.0.1"))
    debugImplementation("androidx.compose.ui:ui-tooling:1.7.0")
    "implementation"("com.quoted:call:1.0")
}
