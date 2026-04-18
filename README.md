# Por Favor

```
██████╗  ██████╗ ██████╗     ███████╗ █████╗ ██╗   ██╗ ██████╗ ██████╗
██╔══██╗██╔═══██╗██╔══██╗    ██╔════╝██╔══██╗██║   ██║██╔═══██╗██╔══██╗
██████╔╝██║   ██║██████╔╝    █████╗  ███████║██║   ██║██║   ██║██████╔╝
██╔═══╝ ██║   ██║██╔══██╗    ██╔══╝  ██╔══██║╚██╗ ██╔╝██║   ██║██╔══██╗
██║     ╚██████╔╝██║  ██║    ██║     ██║  ██║ ╚████╔╝ ╚██████╔╝██║  ██║
╚═╝      ╚═════╝ ╚═╝  ╚═╝    ╚═╝     ╚═╝  ╚═╝  ╚═══╝   ╚═════╝ ╚═╝  ╚═╝
```

> p2p · lan only · encrypted · no trace

Serverless, encrypted peer-to-peer chat over any shared WiFi. No internet required. No server. No accounts. Messages are ephemeral — gone when you close the app.

---

## Features

- **Zero config** — discovers peers automatically via mDNS
- **End-to-end encrypted** — X25519 key exchange + ChaCha20-Poly1305 per message
- **No server** — direct peer-to-peer over your local network
- **No logs** — nothing is stored anywhere
- **Typing indicators** — see when someone is typing
- **Private messages** — `/dm` for direct messages
- **Works on** — Windows, Mac, Linux, Android (Termux)

---

## Install

**Windows** (PowerShell):
```powershell
irm https://raw.githubusercontent.com/nhothebossman/porfavor/master/install.ps1 | iex
```

**Mac / Linux**:
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
porfavor
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

---

## Commands

| Command | Description |
|---|---|
| `/help` | List all commands |
| `/peers` | Show who is online |
| `/dm <name> <message>` | Send a private message |
| `/nick <newname>` | Change your name |
| `/me <action>` | Action message |
| `/clear` | Clear the screen |
| `/quit` | Exit |

---

## How it works

1. On startup, Por Favor announces itself on the network via **mDNS** (no hardcoded IPs)
2. When a peer is discovered, a **TCP connection** is established
3. Both sides exchange **X25519 public keys** and derive a shared secret via HKDF-SHA256
4. All messages are encrypted with **ChaCha20-Poly1305** using that shared key
5. Keys are generated fresh every session — nothing persists

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

---

## Requirements

- Same WiFi / LAN network as your peers
- No firewall blocking TCP port `47200`

---

*No internet. No server. No trace.*
