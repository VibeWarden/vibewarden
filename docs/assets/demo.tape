# VHS tape for the README quickstart demo (docs/assets/demo.gif).
# Regenerate with: vhs docs/assets/demo.tape
# Requires: vhs (brew install vhs) and a vibew binary on PATH built from this repo
# (e.g. go build -o /usr/local/bin/vibew ./cmd/vibewarden, or export PATH before running vhs).
# The recording runs in a throwaway directory; nothing touches the repo.

Output docs/assets/demo.gif

Set FontSize 17
Set Width 1000
Set Height 560
Set Theme "Catppuccin Mocha"
Set Padding 12
Set TypingSpeed 45ms

Hide
Type "cd $(mktemp -d)"
Enter
Type "clear"
Enter
Show

Type "# ship fast with AI — add the security layer you skipped"
Enter
Sleep 1.2s

Type "mkdir myapp && cd myapp"
Enter
Sleep 500ms

Type `vibew init --describe "QR menu app for restaurants"`
Enter
Sleep 4.5s

Type "ls -1"
Enter
Sleep 2.5s

Type "grep -A2 -E '^(tls|security_headers|rate_limit):' vibewarden.yaml"
Enter
Sleep 4s

Type "# TLS, security headers, rate limiting — on by default. Now: vibew dev"
Enter
Sleep 3s
