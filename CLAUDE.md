# Por Favor — Claude Code Guide

Encrypted ephemeral terminal chat. No accounts. No logs. Messages disappear when you close the terminal.

---

## Build

```bash
go build -o porfavor.exe .          # Windows
go build -o porfavor .              # macOS / Linux
go mod tidy                         # resolve deps after changes
```

Requires Go 1.21+. No other tools needed.

---

## Project Layout

```
porfavor/
├── main.go                  # CLI flags, startup, wires mgr → chat
├── console_windows.go       # Enables ANSI/VT processing on Windows (init())
├── go.mod / go.sum
│
├── network/
│   ├── peer.go              # LAN mode: mDNS, TCP, ECDH, framing, Backend interface
│   └── online.go            # Online mode: WebSocket relay, Argon2id room keys, MsgExpiry
│
├── chat/
│   └── chat.go              # All terminal UI: input loop, commands, receive loop, lock
│
├── logo/
│   └── logo.go              # ASCII logo, boot animation
│
├── docs/
│   └── guide.md             # Use-case guide (incident response, red team, etc.)
│
├── install.sh               # macOS / Linux / Termux installer
├── install.ps1              # Windows PowerShell installer
└── README.md
```

---

## Key Architecture Points

### Two modes, one interface

`network.Backend` (defined in `peer.go`) is the interface both modes implement:
- `LAN mode` (`network.Manager`) — mDNS peer discovery, direct TCP, ECDH handshake per peer
- `Online mode` (`network.OnlineManager`) — WebSocket to Cloudflare relay, room key from Argon2id

`chat.go` only knows about `network.Backend`. Mode selection happens in `main.go`.

### Encryption layers

| Message type | Key | Algorithm |
|---|---|---|
| Group chat | Argon2id(room name, deterministic salt) | ChaCha20-Poly1305 |
| DMs | HKDF-SHA256(X25519 shared secret) | ChaCha20-Poly1305 |
| One-time messages | Argon2id(room name) | ChaCha20-Poly1305 |

Room key is derived once on connect. DM keys are derived per-pair from X25519 ECDH during join.

### Relay (Cloudflare Workers + Durable Objects)

Source: `C:\Users\User\Desktop\porfavor-relay\src\index.ts`

Deployed at: `wss://porfavor-relay.relayporfavor.workers.dev`

The relay:
- Routes ciphertext — never sees plaintext
- Stores pubkeys per session for late-joiner key exchange
- Stores encrypted one-time messages until delivery then deletes
- Stores room topic (encrypted)
- Enforces Disappearing Rooms via DO alarm → `alarm()` broadcasts MsgExpiry, calls `deleteAll()`

Deploy: `cd porfavor-relay && wrangler deploy`

---

## Message Types (`network/peer.go`)

```
MsgChat    — broadcast room message (encrypted)
MsgDM      — direct message (per-pair ECDH key)
MsgOneTime — burn-after-reading (relay holds until delivery, then deletes)
MsgBurn    — self-destruct with TTL countdown
MsgTopic   — room topic (persistent on relay)
MsgTyping  — typing indicator
MsgJoin    — peer joined (carries X25519 pubkey for key exchange)
MsgLeave   — graceful departure
MsgNick    — display name change
MsgMe      — /me action message
MsgError   — system notification (relay unreachable, relay info, etc.)
MsgExpiry  — room expired (relay alarm fires → broadcast → clients wipe keys + exit)
```

---

## Commands

```
/dm <name>              open DM session
/dm <name> <msg>        one-off DM
/back                   exit DM session
/onetime <name> "msg"   burn-after-reading — arrives MASKED, /reveal to read
/reveal [name]          show pending onetime privately — screen clears in 15s
/burn <secs> "msg"      self-destruct message — screen clears when it expires
/away "msg"             DM auto-reply
/room <name>            switch rooms mid-session
/invite                 print join command
/topic "text"           set room topic
/verify <name>          compare DM key fingerprints (MITM check)
/nick <name>            change display name
/me <action>            action message
/peers                  list who's online
/connect <ip>           manual IP connect (LAN mode only)
/clear                  clear screen
/nuke                   panic exit — no announcement, clear screen
/quit                   graceful exit
/help                   full command list
/help <command>         detailed help
```

Keyboard:
- `↑ / ↓` — command history
- `Tab` — autocomplete
- `?` while typing `/command` — show usage hint without inserting `?`

---

## Flags

```
--room <name>       room name and encryption key (default: "default")
--expires <dur>     room lifetime: 30m, 2h, 1h30m — relay deletes on expiry
--name <name>       override display name for this session
--send              pipe mode: read stdin, send as message, exit
--lan               LAN/mDNS mode — skips relay
--server <url>      point to a self-hosted relay
--version           print version
```

---

## Security Notes

- Room name = encryption key. Treat it like a password. Share out-of-band.
- `/nuke` disconnects without announcing. Use when you need to vanish.
- `/verify` outputs a fingerprint both sides should compare over voice — detects relay MITM.
- Idle lock: 5 minutes of no input locks the screen and requires the room name to restore.
- Pen test pending (Red Hat contact). Results will be published.

Known gaps: no forward secrecy, no message padding, no replay protection (message IDs planned post-launch).

---

## Current Version

v1.7.0

Recent: /reveal for masked onetimes, burn auto-clear, idle screen lock, ? command hints, Disappearing Rooms (v1.6.0), Argon2id + /verify (v1.5.0).
