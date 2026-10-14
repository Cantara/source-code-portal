val kotlinVersion = "1.9.25"
val cyc = "$cyc"
extra["springVersion"] = "6.1.12"
dependencies {
    implementation("org.springframework:spring-core:${property("springVersion")}")
    implementation("org.springframework:spring-web:${rootProject.extra["springVersion"]}")
    implementation("org.example:cyclic:$cyc")
    implementation("org.example:dyn:latest.release")
    implementation("org.example:snap:1.0-SNAPSHOT")
    implementation("org.example:strict:1.0!!")
    implementation("org.jetbrains.kotlin:kotlin-reflect:$kotlinVersion")
    implementation("org.example:undefined:$nope")
    implementation(undefinedVar)
    constraints {
        implementation("org.example:managed-by-constraint:2.5.0")
    }
    implementation("org.example:managed-by-constraint")
    if (project.hasProperty("withExtra")) {
        implementation("org.example:conditional:1.0")
    }
    add("implementation", "org.example:via-add:1.0")
    implementation(
        "org.example:multiline:1.0",
        "org.example:multiline2:2.0"
    )
    implementation("org.example:a:1") ; implementation("org.example:b:2")
}
