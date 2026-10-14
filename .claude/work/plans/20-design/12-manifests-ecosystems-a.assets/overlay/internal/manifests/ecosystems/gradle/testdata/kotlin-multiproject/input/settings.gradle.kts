pluginManagement {
    repositories { gradlePluginPortal() }
    plugins {
        id("org.gradle.toolchains.foojay-resolver-convention") version "0.8.0"
    }
}
rootProject.name = "shop"
include(":app", ":lib:core")
include("tools")
includeBuild("build-logic")
dependencyResolutionManagement {
    versionCatalogs {
        create("libs") {
            from(files("gradle/libs.versions.toml"))
        }
        create("more") {
            library("junit5", "org.junit.jupiter", "junit-jupiter").version("5.10.2")
            version("mockk", "1.13.12")
        }
    }
}
