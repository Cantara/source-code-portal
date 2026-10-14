plugins { `java-library` }
group = "com.acme.shop"
dependencies {
    api("com.squareup.okio:okio:3.9.1")
    compileOnly(libs.dagger)
}
