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

Por Favor is a terminal chat app built for people who don't want a server reading their messages. End-to-end encrypted, no sign-up, no logs. Close the app and the conversation never happened.

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

That's it. No dependencies, no config files, single binary.

---

## Quick start

```bash
porfavor
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

To chat with a specific group of people, give your room a name. Only people with the same room name can see each other — and read each other's messages.

```bash
porfavor --room fridaynight
```

Share the room name with whoever you want in. Everyone else is locked out at the encryption level.

---

## Commands

| Command | What it does |
|---|---|
| `/peers` | See who's online in your room |
| `/dm <name>` | Open a private conversation with someone |
| `/dm <name> <message>` | Send a one-off private message |
| `/back` | Return to group chat from a DM session |
| `/onetime <name> "message"` | Burn-after-reading message — held until they connect, delivered once, gone forever |
| `/nick <newname>` | Change your display name |
| `/me <action>` | Action message — `* JAY waves` |
| `/connect <ip>` | Connect directly by IP (LAN mode fallback) |
| `/clear` | Clear the screen |
| `/help` | List all commands |
| `/quit` | Exit |

### DM sessions

`/dm` without a message opens a session. Your prompt changes so you always know who you're talking to:

```
/dm JAY
[sys] DM session with JAY · type normally to chat · /back to return

18:44:01 [YOU → JAY]  can you talk?█
```

Type `/back` to return to the group.

### Burn-after-reading

```
/onetime MARK "the key is under the mat"
[sys] ● onetime sealed for MARK · will be delivered when they open it
```

If MARK is offline, the message waits on the relay — encrypted — until they connect. The moment it's delivered, it's deleted. It can never be sent twice.

MARK sees:
```
[● onetime from YOU] the key is under the mat
[sys] ● burned — this message no longer exists
```

---

## Flags

| Flag | Default | Description |
|---|---|---|
| `--room <name>` | `default` | Room name / shared password |
| `--name <name>` | saved | Override your display name for this session |
| `--lan` | off | Skip the relay, use direct LAN/mDNS peer discovery |
| `--server <url>` | built-in | Point to your own relay |
| `--version` | — | Print version |

---

## How it works

### The relay sees nothing

Por Favor uses a relay to connect peers across the internet — but the relay only ever sees encrypted bytes. Here's what actually happens:

```
YOU ──── encrypted ────► relay ◄──── encrypted ──── PEER
         (room key)                   (room key)
```

1. Your `--room` name is hashed with SHA-256 → becomes the relay URL path (the relay knows a hash, not your room name)
2. The same room name is run through HKDF-SHA256 → becomes your 32-byte encryption key (the relay never sees this)
3. Every message is encrypted with ChaCha20-Poly1305 before it leaves your machine
4. The relay routes ciphertext. That's all it does.

### DMs are even more private

When you connect, you broadcast your X25519 public key. Every peer does the same. Por Favor uses ECDH to derive a **unique key for each pair** — completely separate from the room key. Your DMs can't be decrypted by anyone else in the room, even in theory.

### One-time messages survive offline

The relay stores encrypted one-time messages in Cloudflare Durable Object storage until the recipient connects. It stores ciphertext only — no metadata, no sender identity beyond what's in the envelope. On delivery, it's deleted from storage permanently.

### LAN mode

```bash
porfavor --lan
```

Skips the relay entirely. Discovers peers on your local network via mDNS, connects directly over TCP, does ECDH key exchange peer-to-peer. Nothing leaves your network.

If mDNS is blocked (company WiFi, hotel, etc.) connect manually:
```
/connect 192.168.1.42
```

### Encryption at a glance

| What | Algorithm | Key |
|---|---|---|
| Group messages | ChaCha20-Poly1305 | HKDF(room name) |
| Direct messages | ChaCha20-Poly1305 | HKDF(X25519 shared secret) |
| One-time messages | ChaCha20-Poly1305 | HKDF(room name) |
| Key exchange | X25519 ECDH | Generated fresh every session |
| Key derivation | HKDF-SHA256 | — |

Keys are never stored. Every session starts fresh.

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

Runs on Cloudflare's free tier. Zero server to maintain.

---

## Troubleshooting

**"no peers found" on LAN mode**
Your network might be blocking mDNS (multicast). Common on company WiFi, hotels, hotspots. Either switch to online mode (default) or use `/connect <ip>` to connect manually.

**Can't connect on Android / Termux**
Make sure you have internet access. Por Favor bypasses Termux's broken DNS resolver automatically and forces IPv4 — but it still needs a working connection.

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

*No server reads your messages. No account required. No trace.*
