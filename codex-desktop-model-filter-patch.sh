#!/usr/bin/env bash
set -euo pipefail

mode="${1:-patch}"
app="${2:-/Applications/ChatGPT.app}"
asar="$app/Contents/Resources/app.asar"
plist="$app/Contents/Info.plist"
main_executable="$app/Contents/MacOS/ChatGPT"
signature_dir="$app/Contents/_CodeSignature"
backup_root="$HOME/Library/Application Support/Codex/model-filter-patch-backups"
current_backup="$backup_root/current"
old='l=o&&e!==`amazonBedrock`'
new='l=!1                    '

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

usage() {
  cat <<EOF
Usage:
  $0 patch [ChatGPT.app path]
  $0 restore [ChatGPT.app path]
  $0 help

Commands:
  patch    Disable Codex Desktop's model allowlist filter.
  restore  Restore the original app files and OpenAI signature.

Examples:
  osascript -e 'quit app "ChatGPT"'
  $0 patch
  open -a ChatGPT

  osascript -e 'quit app "ChatGPT"'
  $0 restore
  open -a ChatGPT

Notes:
  - The default app path is /Applications/ChatGPT.app.
  - ChatGPT must be closed before patching or restoring.
  - A Desktop update may overwrite the patch; run patch again afterward.
  - Backups are stored under:
    $backup_root
EOF
}

case "$mode" in
  help | -h | --help)
    usage
    exit 0
    ;;
  patch | restore) ;;
  *)
    usage >&2
    exit 1
    ;;
esac

[[ -f "$asar" && -f "$plist" && -f "$main_executable" && -d "$signature_dir" ]] ||
  die "invalid app bundle: $app"
[[ ${#old} -eq ${#new} ]] || die "internal replacement length mismatch"

exe="$app/Contents/MacOS/ChatGPT"
exe_regex=$(printf '%s' "$exe" | sed 's/[][\\.^$*+?(){}|]/\\&/g')
if pgrep -f "^${exe_regex}( |$)" >/dev/null; then
  die "quit this ChatGPT app before patching: $app"
fi

bundle_version=$(/usr/libexec/PlistBuddy -c 'Print :CFBundleVersion' "$plist")

sign_and_verify() {
  local hash
  hash=$(shasum -a 256 "$asar" | awk '{print $1}')
  /usr/libexec/PlistBuddy \
    -c "Set :ElectronAsarIntegrity:Resources/app.asar:hash $hash" "$plist"
  codesign --force --sign - "$app"
  xattr -dr com.apple.quarantine "$app" 2>/dev/null || true
  xattr -dr com.apple.provenance "$app" 2>/dev/null || true
  codesign --verify --deep --strict --verbose=2 "$app"
}

case "$mode" in
  patch)
    old_count=$(OLD="$old" perl -0777 -ne '$n = () = /\Q$ENV{OLD}\E/g; print $n' "$asar")
    new_count=$(NEW="$new" perl -0777 -ne '$n = () = /\Q$ENV{NEW}\E/g; print $n' "$asar")
    [[ "$old_count" -eq 1 ]] || {
      [[ "$old_count" -eq 0 && "$new_count" -eq 1 ]] && die "already patched"
      die "expected one model-filter expression, found $old_count"
    }

    original_hash=$(shasum -a 256 "$asar" | awk '{print $1}')
    backup="$backup_root/$bundle_version-$original_hash"
    mkdir -p "$backup"
    cp -p "$asar" "$backup/app.asar"
    cp -p "$plist" "$backup/Info.plist"
    cp -p "$main_executable" "$backup/ChatGPT"
    cp -pR "$signature_dir" "$backup/_CodeSignature"
    printf '%s\n' "$bundle_version" >"$backup/bundle-version"
    printf '%s\n' "$backup" >"$current_backup"

    OLD="$old" NEW="$new" perl -0777 -i -pe \
      'BEGIN { $old = $ENV{OLD}; $new = $ENV{NEW} } s/\Q$old\E/$new/' "$asar"
    sign_and_verify
    printf 'patched: %s\nbackup: %s\n' "$app" "$backup"
    ;;

  restore)
    [[ -f "$current_backup" ]] || die "no patch backup found"
    backup=$(<"$current_backup")
    [[ -f "$backup/app.asar" && -f "$backup/Info.plist" &&
      -f "$backup/ChatGPT" && -d "$backup/_CodeSignature" ]] ||
      die "incomplete backup: $backup"
    backup_version=$(<"$backup/bundle-version")
    [[ "$bundle_version" == "$backup_version" ]] ||
      die "app version changed; reinstall ChatGPT instead of restoring this backup"

    cp -p "$backup/app.asar" "$asar"
    cp -p "$backup/Info.plist" "$plist"
    cp -p "$backup/ChatGPT" "$main_executable"
    rm -rf "$signature_dir"
    cp -pR "$backup/_CodeSignature" "$signature_dir"
    codesign --verify --deep --strict --verbose=2 "$app"
    printf 'restored: %s\n' "$app"
    ;;
esac
