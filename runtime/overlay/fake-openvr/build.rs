fn main() {
    println!("cargo:rerun-if-changed=fake_openvr_api.c");
    cc::Build::new()
        .file("fake_openvr_api.c")
        .include("../openvr/include")
        .compile("fake_openvr_api");

    for sym in [
        "VR_InitInternal",
        "VR_ShutdownInternal",
        "VR_GetGenericInterface",
        "VR_IsHmdPresent",
        "VR_IsRuntimeInstalled",
    ] {
        println!("cargo:rustc-link-arg=/EXPORT:{sym}");
    }
}
