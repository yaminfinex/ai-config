# whiteboard setup (once per machine)

## Binaries

```bash
# presenterm — terminal markdown deck with hot reload + kitty graphics
gh release download --repo mfontanini/presenterm --pattern '*x86_64-unknown-linux-gnu.tar.gz' -D /tmp/pt --clobber
tar xzf /tmp/pt/presenterm-*.tar.gz -C /tmp/pt && cp /tmp/pt/presenterm-*/presenterm ~/.local/bin/

# mermaid CLI (pulls its own headless Chrome)
npm install -g @mermaid-js/mermaid-cli

# d2 — diagrams with good layout + ASCII output mode
gh release download --repo terrastruct/d2 --pattern '*linux-amd64.tar.gz' -D /tmp/d2 --clobber
tar xzf /tmp/d2/d2-*linux-amd64.tar.gz -C /tmp/d2 && cp /tmp/d2/d2-*/bin/d2 ~/.local/bin/

# SVG rasterizer + graphviz
sudo apt-get install -y librsvg2-bin graphviz
```

On macOS: `brew install presenterm d2 librsvg graphviz && npm install -g @mermaid-js/mermaid-cli`.

## presenterm mermaid config

mmdc's Chrome refuses to start sandboxed on most Linux boxes. Field name is
`puppeteer_config_path` (not `_file`).

```bash
mkdir -p ~/.config/presenterm
cat > ~/.config/presenterm/puppeteer.json <<'EOF'
{ "args": ["--no-sandbox", "--disable-setuid-sandbox"] }
EOF
cat > ~/.config/presenterm/config.yaml <<EOF
mermaid:
  scale: 2
  puppeteer_config_path: $HOME/.config/presenterm/puppeteer.json
EOF
```

The unquoted heredoc expands `$HOME` at write time on purpose — presenterm
reads the path literally.

## herdr kitty graphics

In `~/.config/herdr/config.toml`:

```toml
[experimental]
kitty_graphics = true
```

Then `herdr server reload-config`, and detach/reattach the client
(`ctrl+b d`, run `herdr` again).

## Transport requirements

Images reach the screen only over a byte-transparent connection into a
kitty-graphics-capable terminal:

- Outer terminal: Ghostty, kitty, or WezTerm.
- Plain SSH (Tailscale SSH is ideal — WireGuard absorbs roaming, and herdr
  already provides session persistence). mosh strips all image protocols by
  design; there is no mosh-side fix.
