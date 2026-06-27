fn main() {
    println!("cargo:rerun-if-changed=fake_oneocr.c");
    cc::Build::new().file("fake_oneocr.c").compile("fake_oneocr");

    for sym in [
        "CreateOcrInitOptions",
        "OcrInitOptionsSetUseModelDelayLoad",
        "CreateOcrPipeline",
        "CreateOcrProcessOptions",
        "OcrProcessOptionsSetMaxRecognitionLineCount",
        "RunOcrPipeline",
        "GetOcrLineCount",
        "GetOcrLine",
        "GetOcrLineContent",
        "GetOcrLineBoundingBox",
        "GetOcrLineWordCount",
        "GetOcrWord",
        "GetOcrWordContent",
        "GetOcrWordBoundingBox",
        "ReleaseOcrResult",
    ] {
        println!("cargo:rustc-link-arg=/EXPORT:{sym}");
    }
}
