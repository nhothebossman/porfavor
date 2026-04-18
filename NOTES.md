# Por Favor — Project Notes

---

## What's Built (v1.3.4)

### Core
- X25519 ECDH key exchange — fresh keypair every session
- ChaCha20-Poly1305 encryption on all messages
- HKDF-SHA256 key derivation
- Room passwords — room name derives both the relay URL path and encryption key
- Per-pair ECDH keys for DMs — separate from room key
- 4-byte length-prefixed JSON frames over TCP (LAN mode)
- WebSocket over WSS (online mode)

### Modes
- **Online mode** (default) — connects via Cloudflare relay, works anywhere
- **LAN mode** (`--lan`) — mDNS peer discovery, direct TCP, no internet needed
- `/connect <ip>` — manual IP connect when mDNS is blocked

### Commands
- `/dm <name>` — open a DM session (prompt changes to show target)
- `/dm <name> <message>` — one-off DM
- `/dm` or `/back` — exit DM session
- `/onetime <name> "message"` — burn-after-reading, held on relay until recipient connects
- `/burn <secs> "message"` — self-destructing room message with expiry notice
- `/away "message"` — auto-reply to DMs, shows in prompt
- `/room <name>` — switch rooms mid-session (online only)
- `/invite` — prints the join command for your current room
- `/topic "text"` — set persistent room topic, shown to new joiners
- `/topic clear` — wipe the topic
- `/nick <name>` — change display name (supports lowercase + special chars)
- `/me <action>` — action message (* NAME action)
- `/peers` — list who's online
- `/nuke` — panic exit, clears screen, no trace
- `/clear` — clear screen
- `/quit` — graceful exit
- `/help` — categorised help
- `/help <command>` — detailed help with example for any command

### UX
- ↑/↓ arrow keys — scroll through last 50 sent messages
- Tab — autocomplete commands, cycles on repeat
- DM session history — last 5 messages shown when opening a DM
- @mention detection — highlights in bright green + terminal bell
- Room topic — persistent, stored on relay, delivered on join
- Typing indicators
- Away auto-reply
- Single standard logo
- Free-form usernames (lowercase, special characters)

### Relay (Cloudflare Workers + Durable Objects)
- Per-room isolation — room hash → separate DO instance
- Stores pubkeys in session for snapshot delivery (fixes DM key exchange)
- Stores one-time messages (encrypted) until recipient connects
- Stores room topic (encrypted) until cleared
- Rate limiting — 20 messages/second per client
- Room path derived from SHA256(roomName) — relay knows hash, not name

### Platform support
- Windows (amd64, arm64) — ANSI enabled via init()
- macOS (amd64, arm64) — Gatekeeper quarantine cleared on install
- Linux (amd64, arm64, arm)
- Android / Termux — custom DNS resolver (8.8.8.8), forced IPv4
- Install scripts: install.sh (Mac/Linux/Termux), install.ps1 (Windows)
- GitHub Actions CI — cross-compiles 7 targets on tag push

---

## Phase 2 — Harden & Polish

### Security
- [ ] Key fingerprints — short code derived from shared key, verify out-of-band that nobody is MITMing
- [ ] Replay attack protection — sequence numbers / message IDs, detect and drop duplicates
- [ ] Message padding — pad all messages to fixed size before encrypting to prevent size-based metadata leaks
- [ ] Forward secrecy — Double Ratchet (per-message keys, old keys deleted) — complex, Signal-level
- [ ] Deniability — messages not cryptographically attributable to sender

### Code quality
- [ ] Unit tests — crypto round-trips, frame parser, command parsing
- [ ] Fuzz tests — frame parser, message deserialization
- [ ] Linting — staticcheck, golangci-lint
- [ ] Proper error handling — no more silent drops
- [ ] Context propagation throughout

### UX
- [ ] Word wrap — long messages that overflow terminal width
- [ ] SIGWINCH handling — redraw on terminal resize
- [ ] Left/right cursor movement in input
- [ ] Desktop notifications — notify-send (Linux), osascript (Mac) on DM/mention
- [ ] Config file — ~/.porfavor/config.toml for default room, relay, theme, etc.

### Distribution
- [ ] Reproducible builds — anyone can verify binary matches source
- [ ] Code signing — macOS notarization, Windows Authenticode
- [ ] Homebrew tap — brew install porfavor
- [ ] Winget / Scoop — Windows package managers
- [ ] Auto-update check — notify on startup if newer version exists

---

## Phase 3 — Give it a Home

- [ ] Landing page — porfavor.xyz or similar
  - Logo, tagline, install command front and center
  - One clear answer to "why should I trust this"
  - Link to GitHub, relay source, docs
- [ ] Documentation site — full command reference, how the crypto works, self-hosting guide
- [ ] Community — Discord or IRC (ironic) for users and contributors
- [ ] Hacker News / security community launch

---

## Phase 4 — Widen the Audience

- [ ] GUI frontend — same backend, same encryption, same relay
  - Cross-platform: Windows, Mac, Linux
  - Possibly mobile (iOS / Android)
  - Terminal version stays free and open source forever
- [ ] P2P hole punching — relay for signaling only, then direct connection (STUN/TURN style)
- [ ] Multiple relay fallback — if primary relay is down, try backups
- [ ] Tor support — SOCKS5 proxy option for routing through Tor
- [ ] QR code room invites — /qr prints scannable invite in terminal

---

## Monetization Options (Decide Later)

- **Managed private relay** — paid tier with private relay instance, custom domain, SLA
  - Free: public shared relay (what exists today)
  - Paid: $5–20/month personal, $50–200/month team
- **Enterprise self-hosted** — Docker image, support, SLA — highest revenue potential
- **GUI app** — paid download or freemium, terminal stays free
- **GitHub Sponsors / Open Collective** — community funding

---

## Ideas Saved for Later

- `/nuke` keyboard shortcut (Ctrl+W)
- Decoy room with dummy traffic
- `/reply <name>` — quote last message IRC style
- `/whois <name>` — joined time, online duration, away status
- `/ping <name>` — round-trip latency
- `/timestamps` toggle — hide/show timestamps
- `--compact` flag — tighter layout, no timestamps
- Terminal bell on DM/mention (partial — bell on mention exists)
- `/qr` — QR code of invite command in terminal
- File transfer — `/send <name> <file>` encrypted chunked DMs
- Pipe mode — `echo "hello" | porfavor --room x --to NAME` scriptable
- Dead drop — generate one-time room code, leave message for offline pickup
- Ghost mode — join silently, no join announcement
- Session fingerprint — short hash of shared key for out-of-band verification
- Shared notepad — synchronized text buffer for the room

---

*Built by nhothebossman. Notes last updated v1.3.4.*
