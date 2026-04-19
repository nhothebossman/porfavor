# Por Favor — Practical Guide

> Encrypted terminal chat. No accounts. No logs. Close the terminal and it never happened.

This guide covers five real scenarios where Por Favor is the right tool — along with exact commands for each.

---

## Before you start

Install in one command:

```bash
# macOS / Linux
curl -fsSL https://raw.githubusercontent.com/nhothebossman/porfavor/master/install.sh | bash

# Windows (PowerShell)
irm https://raw.githubusercontent.com/nhothebossman/porfavor/master/install.ps1 | iex

# Android (Termux)
pkg install curl && curl -fsSL https://raw.githubusercontent.com/nhothebossman/porfavor/master/install.sh | bash
```

First run asks for your name. It's stored locally. From then on, `porfavor --room anything` and you're in.

---

## Scenario 1: Incident response war room

**The situation:** Production is down. Your team needs a shared channel right now. Your normal tools are on the same infrastructure that's failing.

**Why Por Favor:** No server to set up. Everyone's on the line in under 30 seconds. The room auto-deletes when the incident closes.

**Person A (whoever calls it first):**
```bash
porfavor --room incident-2026-08-14 --expires 4h
```

```
[sys] booting por favor...
[sys] ⏳ room expires in 4h (at 06:00:00)
```

**Person A then tells the rest of the team the room name** — over phone, text, anything. The room name is the key. Whoever knows it can join; whoever doesn't, can't.

**Everyone else:**
```bash
porfavor --room incident-2026-08-14
```

```
[sys] SARAH joined the chat
[sys] MARCUS joined the chat
[sys] channel open · 4 peers online ✓
```

**During the incident — sharing credentials once:**
```
/burn 60 "db root password: xK9$mq — change after this"
```

Everyone sees it for 60 seconds. Then it's replaced with a burn notice for all of them simultaneously. It was never sent over email or Slack.

**When it's over:**
The room had a 4-hour timer. When it fires, the relay deletes everything and closes all connections.

```
[sys] ⚠  this room has expired — it never existed
[sys] keys wiped — this room never existed.
```

No transcript. No log. No "who has access to that Slack channel" six months from now.

---

## Scenario 2: Red team coordination

**The situation:** You're on an engagement. You need to coordinate with your team without using the client's Slack, without Signal (phone numbers), and without IRC (no server, no encryption by default).

**Why Por Favor:** Single binary. No install. No account. Runs in your terminal. Encrypted by default. When the engagement is over, the room is gone.

**Setup:**
```bash
porfavor --room [engagement-codename] --expires 8h
```

Pick a room name your team agreed on pre-engagement, not something derived from the client name.

**During the engagement — sharing a finding:**
```
/burn 120 "domain admin: corp\svc_backup / P@ssw0rd1 — don't use yet, waiting on go-ahead"
```

**Checking who's active:**
```
/peers
```

```
[sys] peers: CODY, RASHA, YOU
```

**Sending something to one person:**
```
/dm RASHA found open SMB — 192.168.10.44, share: ADMIN$
```

DMs use a separate per-pair key derived from X25519. Nobody else in the room — including the relay — can decrypt it.

**Verifying the channel isn't being MITM'd:**
```
/verify RASHA
```

Both sides run this and read the fingerprint out loud over voice. If they match, the relay didn't swap your keys.

**When the engagement ends:**
```
/nuke
```

No goodbye message. No peer notification. The relay just sees the socket close. Clean exit.

---

## Scenario 3: Sharing a secret once

**The situation:** You need to send someone a password, API key, or private key. Email is too risky. You'd rather not paste it into Slack where it lives in a log forever.

**Why Por Favor:** The message is encrypted end-to-end and stored on the relay only until it's opened — then it's permanently deleted. One delivery. Gone.

**You (sender):**
```bash
porfavor --room key-handoff-alex
```

```
/onetime ALEX "ANTHROPIC_API_KEY=sk-ant-xxxxxxxxxx"
[sys] ● onetime sealed for ALEX · will be delivered when they open it
```

You can leave. The message waits on the relay, encrypted, until Alex connects.

**Alex (recipient) — whenever they're ready:**
```bash
porfavor --room key-handoff-alex
```

```
[sys] ● onetime from YOU:
      ANTHROPIC_API_KEY=sk-ant-xxxxxxxxxx
[sys] this message has been permanently deleted from the relay.
```

That's the only delivery. The relay deletes the record the moment it's read. Alex reconnecting or someone else joining the room gets nothing.

---

## Scenario 4: Automated alerts into a private channel

**The situation:** You want your CI pipeline, cron jobs, or monitoring scripts to post into a channel that doesn't go through a third-party service — no Slack, no PagerDuty, no email.

**Why Por Favor:** Pipe mode. Read from stdin, send as a message, exit. No interactive UI. Scriptable.

**Setup — one-time, on your machine:**
```bash
porfavor --room oncall
# Set your name to something useful
# Ctrl+C to exit
```

**In your CI pipeline or cron:**
```bash
echo "deploy finished — all green" | porfavor --room oncall --send
```

```bash
./run-tests.sh | tail -5 | porfavor --room oncall --name ci-bot --send
```

```bash
# Monitor for errors and forward them
tail -f /var/log/app.log | grep --line-buffered "ERROR" | porfavor --room oncall --send
```

**On your phone or laptop — any terminal:**
```bash
porfavor --room oncall
```

You're now watching a live private feed of your infrastructure. No third party in the middle. No webhook to configure. No API keys to rotate.

**Combine with --expires for one-off deploys:**
```bash
echo "staging deploy complete" | porfavor --room deploy-$(date +%s) --expires 1h --send
```

The room and everything in it is gone in an hour.

---

## Scenario 5: Local-only — no internet, no relay

**The situation:** You're on a private network — air-gapped lab, conference, ship, field operation. No internet access or you don't want any traffic leaving the LAN.

**Why Por Favor:** LAN mode discovers peers via mDNS and connects directly over TCP. Nothing leaves your network.

**Everyone on the LAN:**
```bash
porfavor --lan
```

```
[sys] scanning network...
[sys] peer discovered → MARCUS
[sys] channel open · 2 peers online ✓
```

No relay. No internet. Peer-to-peer ECDH key exchange directly between machines. All encryption is the same — ChaCha20-Poly1305 with a per-pair X25519 key.

**If mDNS is blocked** (common on managed WiFi, VPNs, hotel networks):
```bash
# Person A — check your IP
ip addr  # or ipconfig on Windows

# Person B — connect directly
porfavor --lan
/connect 192.168.1.42
```

**For a fully self-hosted setup with your own relay:**
```bash
# Deploy once (Cloudflare free tier)
cd porfavor-relay && wrangler deploy

# Point clients at your relay
porfavor --server wss://your-relay.your-subdomain.workers.dev --room yourroom
```

Your relay, your keys, your logs (or lack of them).

---

## Command quick-reference

```
/peers                       who's online
/dm <name>                   open private session with someone
/dm <name> <message>         quick DM without opening a session
/back                        return to group from DM
/verify <name>               compare key fingerprints — MITM check
/burn <secs> "message"       self-destruct message, visible for N seconds
/onetime <name> "message"    deliver once, delete on read
/away "message"              auto-reply to DMs while you're away
/room <name>                 switch rooms without restarting
/invite                      print join command for this room
/topic "text"                set room topic shown to everyone who joins
/nick <name>                 change your display name
/me <action>                 action message  *YOU waves*
/clear                       clear the screen
/nuke                        disconnect silently, no announcement
/quit                        graceful exit — announces departure
/help                        full command list
/help <command>              detailed help for one command
```

---

## Security model in plain English

**The room name is your key.** It's run through Argon2id (slow, GPU-resistant) to derive a 32-byte encryption key. Anyone who knows the room name can decrypt messages. Treat it like a password — share it out-of-band (voice, text, anything), not in another chat app.

**The relay sees nothing useful.** It routes encrypted bytes between WebSocket connections. The URL path it sees is a SHA-256 hash of your room name, not the name itself. It never sees the encryption key. Messages arrive and leave as ciphertext.

**DMs have their own keys.** When you connect, you exchange X25519 public keys. Por Favor derives a unique ChaCha20 key for each pair. Your DMs are invisible to other room members and to the relay.

**Ephemeral by design.** Keys are never written to disk. Each session generates a fresh X25519 keypair. If the process dies, the keys die with it.

**What it doesn't do (yet):** forward secrecy within a session, message padding, or authenticated room membership beyond knowledge of the room name. A pen test is in progress — results will be published.

---

*Open source. MIT licensed. No telemetry. No accounts. No trace.*
