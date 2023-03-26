FROM --platform=${BUILDPLATFORM} rust:1.68.1 AS build
WORKDIR /opt/build
COPY . .
RUN rustup target add wasm32-wasi && cargo build --target wasm32-wasi --release

FROM cgr.dev/chainguard/static:latest
COPY --from=build /opt/build/target/wasm32-wasi/release/aks_spin_demo.wasm .
COPY --from=build /opt/build/spin.toml .
