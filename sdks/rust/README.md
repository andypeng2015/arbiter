# Arbiter Rust SDK

Rust client crate for the Arbiter gRPC API with bearer-token metadata and bounded retries on transient unary failures.

## Build

```bash
cargo build
```

## Example

```bash
cargo run --example smoke
```

The client accepts bare `host:port` targets for local plaintext use and `https://...` targets for TLS endpoints. Add a token with `.with_token("...")` when the server requires auth.

See [src/lib.rs](/home/draco/work/arbiter/sdks/rust/src/lib.rs) and [examples/smoke.rs](/home/draco/work/arbiter/sdks/rust/examples/smoke.rs).
