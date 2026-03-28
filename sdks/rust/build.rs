fn main() -> Result<(), Box<dyn std::error::Error>> {
    tonic_build::configure()
        .build_server(true)
        .compile_protos(&["service.proto", "capability.proto"], &["."])?;
    println!("cargo:rerun-if-changed=service.proto");
    println!("cargo:rerun-if-changed=capability.proto");
    Ok(())
}
