# Por Favor

```
██████╗  ██████╗ ██████╗     ███████╗ █████╗ ██╗   ██╗ ██████╗ ██████╗
██╔══██╗██╔═══██╗██╔══██╗    ██╔════╝██╔══██╗██║   ██║██╔═══██╗██╔══██╗
██████╔╝██║   ██║██████╔╝    █████╗  ███████║██║   ██║██║   ██║██████╔╝
██╔═══╝ ██║   ██║██╔══██╗    ██╔══╝  ██╔══██║╚██╗ ██╔╝██║   ██║██╔══██╗
██║     ╚██████╔╝██║  ██║    ██║     ██║  ██║ ╚████╔╝ ╚██████╔╝██║  ██║
╚═╝      ╚═════╝ ╚═╝  ╚═╝    ╚═╝     ╚═╝  ╚═╝  ╚═══╝   ╚═════╝ ╚═╝  ╚═╝
```

> encrypted · ephemeral · no accounts · works anywhere

Anonymous, end-to-end encrypted chat. No server stores your messages. No accounts. No logs. Works over the internet (default) or directly on your local network.

---

## Features

- **End-to-end encrypted** — X25519 key exchange + ChaCha20-Poly1305. The relay never sees plaintext.
- **Room passwords** — `--room <name>` lets only people with the same room name/password join your channel
- **Private DMs** — per-pair ECDH keys for direct messages, encrypted separately from the group
- **Online by default** — connects via a Cloudflare-hosted relay so peers don't need to be on the same network
- **LAN mode** — `--lan` for direct peer-to-peer over mDNS, no relay, no internet needed
- **Ephemeral** — keys are generated fresh every session, nothing is stored anywhere
- **Typing indicators** — see when someone is composing a message
- **Works on** — Windows, macOS, Linux, Android (Termux)

---

## Install

**Windows** (PowerShell):
```powershell
irm https://raw.githubusercontent.com/nhothebossman/porfavor/master/install.ps1 | iex
```

**macOS / Linux**:
```bash
curl -fsSL https://raw.githubusercontent.com/nhothebossman/porfavor/master/install.sh | bash
```

**Android (Termux)**:
```bash
pkg install curl
curl -fsSL https://raw.githubusercontent.com/nhothebossman/porfavor/master/install.sh | bash
```

---

## Usage

```
porfavor [options]
```

On first run you'll be asked for a name. After that it boots straight into chat.

```
[sys] booting por favor...
[sys] scanning network...
[sys] JAY joined the chat
[sys] channel open · 2 peers online ✓

18:42:13 [JAY]   yo you on
18:42:45 [MARK]  yeah lets go
18:44:01 [YOU]   █
```

### Flags

| Flag | Default | Description |
|---|---|---|
| `--room <name>` | `default` | Room name (acts as a shared password — only peers with the same room can see each other or read messages) |
| `--name <name>` | saved name | Override your display name for this session |
| `--lan` | off | Use LAN/mDNS mode instead of the online relay |
| `--server <url>` | built-in relay | Use a custom relay server (advanced) |
| `--version` | — | Print version and exit |

### Example: private room

```bash
# All three friends run the same room name:
porfavor --room supersecretclub
```

Anyone outside that room gets a different encryption key and can't read the messages — even if they're connected to the same relay.

---

## Commands

| Command | Description |
|---|---|
| `/help` | List all commands |
| `/peers` | Show who is online in your room |
| `/dm <name> <message>` | Send an end-to-end encrypted private message |
| `/nick <newname>` | Change your display name |
| `/me <action>` | Action message (e.g. `/me waves`) |
| `/clear` | Clear the screen |
| `/quit` | Exit gracefully |

LAN mode only:

| Command | Description |
|---|---|
| `/connect <ip>` | Manually connect to a peer by IP address (useful when mDNS is blocked) |

---

## How it works

### Online mode (default)

```
[YOU] ──── WSS ────► [Cloudflare Relay] ◄──── WSS ──── [PEER]
              encrypted with room key + per-pair ECDH DM keys
```

1. Both peers connect to the relay over WSS (TLS)
2. The relay only sees **ciphertext** — it routes packets but cannot read them
3. Your `--room` value is hashed (SHA-256) to produce the room's URL path **and** the encryption key — peers in different rooms can't decrypt each other's messages even if they share the same relay
4. On connect, each client announces its **X25519 public key** in the join message
5. Every peer derives a **per-pair shared key** for DMs (ECDH + HKDF), separate from the group key
6. Group chat uses the **room key** (HKDF of the room name)
7. DMs use the **per-pair ECDH key**

### LAN mode (`--lan`)

```
[YOU] ──── TCP ────► [PEER]  (direct, no relay)
```

1. On startup, Por Favor announces itself via **mDNS** (`_porfavor._tcp`)
2. On peer discovery, a **TCP connection** is established on port 47200
3. Both sides exchange X25519 public keys directly, derive a shared secret via HKDF-SHA256
4. All messages use **ChaCha20-Poly1305** with that shared key
5. If mDNS is blocked (company WiFi, etc.) use `/connect <ip>` to connect manually

### Encryption summary

| Layer | Algorithm | Key source |
|---|---|---|
| Key exchange | X25519 ECDH | Generated fresh each session |
| Key derivation | HKDF-SHA256 | `porfavor-v1` / `porfavor-dm-v1` / `porfavor-room-v1` |
| Message encryption | ChaCha20-Poly1305 | Derived shared key |
| Room isolation | SHA-256 | Room name → URL path + encryption key |

The relay server (Cloudflare Workers) **never has access to plaintext**. It only sees the room hash (used to route WebSocket connections) and encrypted byte payloads.

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

Cross-compile for another platform:
```bash
GOOS=linux GOARCH=arm64 go build -o porfavor-linux-arm64 .
```

---

## Self-hosting the relay (optional)

Por Favor ships with a built-in public relay. If you want your own:

1. Install [Wrangler](https://developers.cloudflare.com/workers/wrangler/install-and-update/)
2. Clone the relay source:
   ```bash
   git clone https://github.com/nhothebossman/porfavor
   cd porfavor-relay
   npm install
   wrangler deploy
   ```
3. Point clients at your relay:
   ```bash
   porfavor --server wss://your-worker.workers.dev
   ```

The relay is a single Cloudflare Worker with Durable Objects — zero infrastructure, free tier covers typical usage.

---

## Troubleshooting

**Peers not finding each other (LAN mode)**
- Make sure both devices are on the same WiFi/LAN and not in a "client isolation" SSID
- Try `/connect <ip>` as a fallback — works even when mDNS is blocked
- Company/hotel WiFi often blocks mDNS — use online mode instead

**Can't connect from Termux / Android**
- Por Favor uses Google's DNS (8.8.8.8) to work around Termux's broken resolver — make sure you have internet access
- Online mode works better than LAN mode from mobile

**Port 47200 blocked (LAN mode)**
- Ask your network admin or use online mode instead

---

## Requirements

- **Online mode**: any internet connection
- **LAN mode**: same WiFi/LAN, TCP port `47200` open between peers

---

*No server reads your messages. No account required. No trace.*
