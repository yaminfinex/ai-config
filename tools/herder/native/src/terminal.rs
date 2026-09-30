//! A terminal panel (Rung 2). `alacritty_terminal` drives a local PTY running `et <host>` when the host
//! supports it, else `ssh -t <host>`, opened in the agent's cwd (settled decisions 2 and 7). The grid
//! is painted by a view in `views`; this module owns the PTY, the emulator state and resize.
//! Rung 3 reuses it for the persistent herdr sidecar.
