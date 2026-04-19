# Por Favor

```
██████╗  ██████╗ ██████╗     ███████╗ █████╗ ██╗   ██╗ ██████╗ ██████╗
██╔══██╗██╔═══██╗██╔══██╗    ██╔════╝██╔══██╗██║   ██║██╔═══██╗██╔══██╗
██████╔╝██║   ██║██████╔╝    █████╗  ███████║██║   ██║██║   ██║██████╔╝
██╔═══╝ ██║   ██║██╔══██╗    ██╔══╝  ██╔══██║╚██╗ ██╔╝██║   ██║██╔══██╗
██║     ╚██████╔╝██║  ██║    ██║     ██║  ██║ ╚████╔╝ ╚██████╔╝██║  ██║
╚═╝      ╚═════╝ ╚═╝  ╚═╝    ╚═╝     ╚═╝  ╚═╝  ╚═══╝   ╚═════╝ ╚═╝  ╚═╝
```

**Encrypted. Ephemeral. No accounts. Works anywhere.**

Terminal chat with end-to-end encryption. No sign-up. No logs. Close the app and the conversation never happened. Set an expiry and the room itself gets deleted — messages, keys, everything.

---

## Install

**Windows** — open PowerShell and run:
```powershell
irm https://raw.githubusercontent.com/nhothebossman/porfavor/master/install.ps1 | iex
```

**macOS / Linux:**
```bash
curl -fsSL https://raw.githubusercontent.com/nhothebossman/porfavor/master/install.sh | bash
```

**Android (Termux):**
```bash
pkg install curl && curl -fsSL https://raw.githubusercontent.com/nhothebossman/porfavor/master/install.sh | bash
```

No dependencies. No config files. Single binary.

---

## Quick start

```bash
porfavor --room warroom
```

First run asks for your name. After that it goes straight to chat.

```
[sys] booting por favor...
[sys] scanning network...
[sys] JAY joined the chat
[sys] channel open · 2 peers online ✓

18:42:13 [JAY]   yo
18:42:45 [YOU]   hey, finally
18:43:01 [YOU]   █
```

Only people with the same `--room` name can connect — and the room name is your encryption key, so anyone with a different name can't decrypt a single byte.

---

## Disappearing Rooms

```bash
porfavor --room warroom --expires 2h
```

```
[sys] ⏳ room expires in 2h (at 18:30:00)
```

When the timer runs out, the relay broadcasts an expiry notice to every connected client, deletes all stored data from its Durable Object storage, and closes all WebSockets. Clients wipe their in-memory encryption keys and exit.

Anyone who tries to join after expiry gets rejected immediately.

```
[sys] ⚠  this room has expired — it never existed
[sys] keys wiped — this room never existed.
```

Use any Go duration: `30m`, `2h`, `1h30m`, `45m`.

This is the whole point. A war room that closes when the incident does.

---

## Commands

| Command | What it does |
|---|---|
| `/peers` | See who's online |
| `/dm <name>` | Open a private encrypted session |
| `/dm <name> <message>` | Send a one-off private message |
| `/back` | Return to group chat from a DM session |
| `/onetime <name> "message"` | Burn-after-reading — held until they connect, delivered once, gone |
| `/burn <secs> "message"` | Self-destruct message — visible for N seconds, then gone for everyone |
| `/away "message"` | Auto-reply to incoming DMs |
| `/away` | Clear away status |
| `/room <name>` | Switch rooms without restarting |
| `/invite` | Print the join command for your current room |
| `/topic "text"` | Set a room topic (shown to everyone who joins) |
| `/topic clear` | Clear the topic |
| `/verify <name>` | Show the DM key fingerprint for out-of-band MITM check |
| `/nick <newname>` | Change your display name |
| `/me <action>` | Action message — `* JAY waves` |
| `/connect <ip>` | Connect directly by IP (LAN mode) |
| `/clear` | Clear the screen |
| `/nuke` | Disconnect and vanish — no goodbye, no trace |
| `/help` | List all commands |
| `/help <command>` | Detailed help for one command |
| `/quit` | Exit gracefully |

### DM sessions

`/dm` without a message opens a session. Your prompt changes so you always know who you're talking to:

```
/dm JAY
[sys] DM session with JAY · type normally to chat · /back to return

18:44:01 [YOU → JAY]  can you talk?█
```

### Burn-after-reading

```
/onetime MARK "the key is under the mat"
[sys] ● onetime sealed for MARK · will be delivered when they open it
```

If MARK is offline, the encrypted message waits on the relay. The moment it's delivered, it's deleted from storage permanently.

### Self-destruct messages

```
/burn 30 "stand down, false alarm"
[sys] ● burn message sent · expires in 30s
```

Everyone in the room sees it with a countdown. After 30 seconds a burn notice replaces it for everyone simultaneously.

### Away mode

```
/away "on a call, back in 20"
[sys] away: on a call, back in 20
```

Anyone who DMs you gets an automatic reply. Your prompt shows `· away` while active.

### Code highlighting

Wrap anything in backticks for syntax highlighting — useful when sharing commands, scripts, or payloads:

```
run `nmap -sV 192.168.1.0/24` and paste the output
```

The backtick-wrapped portion renders in cyan, visually distinct from surrounding text.

---

## Pipe mode

Read from stdin and send as a message, then exit. No interactive UI.

```bash
echo "deploy finished — all green" | porfavor --room ops --send

tail -f /var/log/app.log | grep ERROR | porfavor --room alerts --send

./run-checks.sh | porfavor --room oncall --send
```

Combine with `--expires` to create a room, post the result, and let the room disappear on its own schedule.

---

## Flags

| Flag | Default | Description |
|---|---|---|
| `--room <name>` | `default` | Room name and encryption key (shared with peers) |
| `--expires <duration>` | off | Room lifetime — `30m`, `2h`, `1h30m`. Room is deleted after this duration. |
| `--name <name>` | saved | Override your display name for this session |
| `--send` | off | Read from stdin and send as a message, then exit |
| `--lan` | off | Skip the relay — direct LAN/mDNS peer discovery |
| `--server <url>` | built-in | Point to your own self-hosted relay |
| `--version` | — | Print version |

---

## How it works

### The relay sees nothing

Por Favor uses a relay to connect peers across the internet — but the relay only ever sees encrypted bytes.

```
YOU ──── encrypted ────► relay ◄──── encrypted ──── PEER
         (room key)                   (room key)
```

1. Your `--room` name is hashed with SHA-256 → becomes the relay URL path (the relay knows a hash, not your name)
2. The same room name is run through **Argon2id** (3 passes, 64 MB) → becomes your 32-byte encryption key (the relay never sees this)
3. Every message is encrypted with ChaCha20-Poly1305 before it leaves your machine
4. The relay routes ciphertext. That's all it does.

Argon2id means brute-forcing weak room names is expensive even on GPU hardware — buying time against offline dictionary attacks.

### DMs are end-to-end between the two of you

When you connect, you broadcast your X25519 public key. Every peer does the same. Por Favor uses ECDH to derive a **unique key for each pair** — completely separate from the room key. Your DMs cannot be decrypted by anyone else in the room, including the relay operator.

To verify no one swapped pubkeys during connection (MITM check):

```
/verify JAY
  key fingerprint with JAY:
  a3:f1:9c:02:7e:44:bb:d8
  ask JAY to run /verify YOU and compare — if they match, no MITM
```

Both sides independently derive the same fingerprint. If they don't match, a relay substituted a key.

### Burn-after-reading survives offline

The relay stores encrypted one-time messages in Cloudflare Durable Object storage until the recipient connects. It stores ciphertext only. On delivery, the record is deleted from storage permanently.

### Disappearing Rooms are enforced server-side

When you set `--expires`, the relay stores the Unix expiry timestamp in its Durable Object and schedules an alarm. When the alarm fires:

1. The relay broadcasts the expiry notice to every connected WebSocket
2. `deleteAll()` wipes every key in the DO storage — topic, pending messages, expiry timestamp, everything
3. All WebSocket connections are closed

The client zeroes its encryption keys in memory and exits. A room created with `--expires 2h` has a hard two-hour lifespan enforced at the infrastructure level, not just in the client.

### LAN mode

```bash
porfavor --lan
```

Skips the relay entirely. Discovers peers on your local network via mDNS, connects directly over TCP, does ECDH key exchange peer-to-peer. Nothing leaves your network.

If mDNS is blocked (company WiFi, hotel, VPN) connect manually:
```
/connect 192.168.1.42
```

### Encryption at a glance

| Layer | Algorithm | Key |
|---|---|---|
| Group messages | ChaCha20-Poly1305 | Argon2id(room name, deterministic salt) |
| Direct messages | ChaCha20-Poly1305 | HKDF-SHA256(X25519 shared secret) |
| One-time messages | ChaCha20-Poly1305 | Argon2id(room name) |
| Key exchange | X25519 ECDH | Generated fresh every session |

Keys are never stored to disk. Every session starts with a fresh keypair.

---

## Build from source

Requires [Go 1.21+](https://go.dev/dl/)

```bash
git clone https://github.com/nhothebossman/porfavor
cd porfavor
go mod tidy
go build -o porfavor .
./porfavor
```

Cross-compile for any platform:
```bash
GOOS=linux   GOARCH=arm64 go build -o porfavor-linux-arm64 .
GOOS=darwin  GOARCH=arm64 go build -o porfavor-mac-arm64 .
GOOS=windows GOARCH=amd64 go build -o porfavor.exe .
```

---

## Self-hosting the relay

Por Favor ships with a public relay. If you want your own — takes about 5 minutes:

```bash
# Install Wrangler (Cloudflare's CLI)
npm install -g wrangler
wrangler login

# Clone and deploy
git clone https://github.com/nhothebossman/porfavor
cd porfavor-relay
npm install
wrangler deploy
```

Then point your clients at it:
```bash
porfavor --server wss://your-relay.workers.dev
```

Runs on Cloudflare's free tier. Zero server to maintain. Disappearing Rooms use Cloudflare Durable Object alarms — included in the free tier.

---

## Troubleshooting

**"no peers found" in LAN mode**
Your network might be blocking mDNS multicast — common on company WiFi, hotels, hotspots. Either use online mode (default) or `/connect <ip>` to connect manually.

**Can't connect on Android / Termux**
Por Favor bypasses Termux's broken DNS resolver automatically and forces IPv4, but it still needs a working internet connection.

**macOS says the app can't be opened**
The installer handles this automatically with `xattr`. If you're on an older install, run:
```bash
xattr -d com.apple.quarantine ~/.local/bin/porfavor
```

**porfavor not found after install**
Add `~/.local/bin` to your PATH:
```bash
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc && source ~/.zshrc
```

---

---

## Documentation

**[Practical Guide →](docs/guide.md)** — Five real scenarios with exact commands: incident response war rooms, red team coordination, one-time secret sharing, automated alerts, and LAN-only ops.

---

*No server reads your messages. No account required. No trace.*
