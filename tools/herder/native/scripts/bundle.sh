#!/bin/sh
# Build "target/herder native.app": the release binary in a bundle with a stable id
# (dev.herder.native, the id notifications are granted to), ad-hoc signed. Build only: it never
# launches the app; the owner opens it, or copies it with `just install`.
set -e
cd "$(dirname "$0")/.."
export PATH="$HOME/.cargo/bin:$PATH"   # rustup's cargo, not Homebrew's
cargo build --release
APP="target/herder native.app"
mkdir -p "$APP/Contents/MacOS"
cp target/release/herder-native "$APP/Contents/MacOS/herder-native"
cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleIdentifier</key><string>dev.herder.native</string>
  <key>CFBundleName</key><string>herder native</string>
  <key>CFBundleDisplayName</key><string>herder native</string>
  <key>CFBundleExecutable</key><string>herder-native</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>0.1.0</string>
  <key>CFBundleVersion</key><string>1</string>
  <key>LSMinimumSystemVersion</key><string>13.0</string>
  <key>LSApplicationCategoryType</key><string>public.app-category.developer-tools</string>
  <key>NSHighResolutionCapable</key><true/>
</dict></plist>
PLIST
codesign --force -s - "$APP"
echo "bundle: $APP (dev.herder.native)"
