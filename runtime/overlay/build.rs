fn main() {
    let target_os = std::env::var("CARGO_CFG_TARGET_OS").unwrap_or_default();
    if target_os != "windows" {
        return;
    }

    let shim_root = "openvr";
    let shim_c = format!("{shim_root}/openvr_shim.c");
    let openvr_include = format!("{shim_root}/include");

    println!("cargo:rerun-if-changed={shim_c}");
    println!("cargo:rerun-if-changed={shim_root}/openvr_shim.h");

    cc::Build::new()
        .file(&shim_c)
        .include(&openvr_include)
        .std("c11")
        .compile("openvr_shim");
}
