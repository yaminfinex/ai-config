# herder native

The macOS client for herder serve, on GPUI (`gpui-kit = "=0.7.0"`). Shape and rules: `ARCHITECTURE.md`.

    just check        # fmt --check, clippy -D warnings, test (fixtures, no network)
    just run          # release build, talks to the tailnet serve (HERDER_URL overrides)
    just harness "wait:800 shot:lens rss quit"   # scripted run; never takes focus
    just bundle       # target/herder native.app, ad-hoc signed

Needs rustup's Rust (`rust-toolchain.toml`); the justfile puts `~/.cargo/bin` first because Homebrew's
cargo can't build gpui-pre. First build is a couple of minutes.
