plugins {
    alias(libs.plugins.kotlin.jvm) apply false
    id("com.diffplug.spotless") version "6.25.0" apply false
}

subprojects {
    dependencies {
        testImplementation(libs.junit.jupiter)
    }
}

allprojects {
    group = "com.acme.shop"
    version = "3.2.1"
}
