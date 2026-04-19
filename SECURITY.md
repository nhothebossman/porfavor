# Por Favor — Security Model

Version: v1.3.4  
Last updated: 2026-04-19

This document describes what Por Favor protects against, what it does not,
and the known gaps we are actively working to address. We believe honest
documentation of limitations builds more trust than overclaiming.

---

## What Por Favor Is

Por Favor is an end-to-end encrypted terminal chat application. Messages
are encrypted on the sender's device and decrypted only on the recipient's
device. The relay server routes ciphertext and cannot read message content.

---

## Cryptographic Primitives

| Purpose | Algorithm | Rationale |
|---|---|---|
| Key exchange | X25519 ECDH | Fast, simple, no weak curve parameters |
| Key derivation | HKDF-SHA256 | Standard, well-audited KDF |
| Message encryption | ChaCha20-Poly1305 | Fast on ARM/mobile, no hardware AES required |
| Nonce generation | crypto/rand (12 bytes) | OS-level CSPRNG |
| Room path derivation | SHA-256 | One-way, relay knows hash not name |

All cryptographic operations use Go's standard library (`crypto/ecdh`,
`golang.org/x/crypto/chacha20poly1305`, `golang.org/x/crypto/hkdf`).
No custom cryptographic implementations.

---

## Key Derivation Details

### Room key
```
IKM  = room name (UTF-8)
Salt = "porfavor-salt"
Info = "porfavor-room-v1"
OKM  = 32 bytes → ChaCha20-Poly1305 key
```

### Per-pair DM key
```
secret = X25519(local_priv, peer_pub)
IKM    = secret
Salt   = nil
Info   = "porfavor-dm-v1"
OKM    = 32 bytes → ChaCha20-Poly1305 key
```

### LAN peer key
```
secret = X25519(local_priv, peer_pub)
IKM    = secret
Salt   = nil
Info   = "porfavor-v1"
OKM    = 32 bytes → ChaCha20-Poly1305 key
```

---

## What Por Favor Protects Against

### ✓ Passive network eavesdropper
An attacker who can observe network traffic between a client and the relay
sees only TLS-encrypted WebSocket frames. Even if TLS were broken, they
would see ChaCha20-Poly1305 ciphertext. They cannot read message content.

### ✓ Curious relay operator
The relay (Cloudflare Worker) receives only ciphertext. It cannot decrypt
messages. It knows the room hash (not the room name), the sender's display
name, and the message size and timing. It cannot read content.

### ✓ Wrong-room participant
A user in a different room uses a different encryption key derived from
their room name. They cannot decrypt messages from another room even if
they somehow receive them.

### ✓ Stored message confidentiality
One-time messages and room topics stored in Durable Object storage are
stored as ciphertext. If Cloudflare's storage were compromised, the
attacker would see encrypted blobs only.

### ✓ No account linkage
Por Favor requires no phone number, email, or persistent identity.
Display names are ephemeral and not verified. There is no registration.

### ✓ Decryption failure oracle
Failed decryption silently drops the message. No error information is
returned to the sender. This prevents chosen-ciphertext attacks based
on decryption failure responses.

---

## What Por Favor Does NOT Protect Against

### ✗ Malicious relay operator (pubkey substitution)
**This is the most significant gap.**

The relay stores and forwards X25519 public keys as part of the join
handshake. A malicious relay operator could substitute a peer's public
key with their own, perform ECDH with both sides, and relay decrypted
and re-encrypted messages — a classic MITM attack.

*Mitigation planned: key fingerprints (see below)*

### ✗ Replay attacks
The relay could capture and replay a valid ciphertext to a recipient.
Messages have no sequence numbers or unique IDs. A replayed message
would decrypt successfully and appear legitimate.

*Mitigation planned: message IDs with deduplication*

### ✗ Low-entropy room passwords
The room key is derived directly from the room name via HKDF. If the
room name is short or predictable (e.g. "default", "work", "test"),
the derived key has low effective entropy. An attacker who captures
ciphertext could attempt to brute-force common room names.

*Mitigation planned: PBKDF2/scrypt/Argon2 for room key derivation*

### ✗ Forward secrecy
If a client's X25519 private key is compromised after a session, an
attacker who captured past traffic could decrypt it. Keys are generated
fresh per session but not rotated within a session. There is no
Double Ratchet or similar per-message key derivation.

*Not planned for immediate roadmap — significant complexity*

### ✗ Message size metadata
All messages are transmitted at their natural size. An observer who
can see ciphertext sizes could infer message length, which leaks
some information about conversation content patterns.

*Mitigation planned: message padding to fixed size buckets*

### ✗ Traffic analysis
The relay can observe who is talking to whom (display names), when,
and how frequently. The relay does not see content but does see
communication patterns.

### ✗ Endpoint compromise
If a user's device is compromised, all messages visible in their
terminal session are exposed. Por Favor provides no protection
against malware, keyloggers, or physical device access.

### ✗ Display name impersonation
Display names are not verified. Any participant can claim any name
including names already in use. There is no authentication of
identity beyond the ephemeral session keypair.

*Partial mitigation: key fingerprints would allow out-of-band
verification that a name corresponds to a specific keypair*

### ✗ Deniability
Messages encrypted with ChaCha20-Poly1305 using a shared key do not
provide cryptographic deniability. A recipient could potentially prove
to a third party that a message was sent (though the shared key
complicates this).

---

## Known Gaps — Prioritised

| Gap | Severity | Complexity | Status |
|---|---|---|---|
| Relay MITM via pubkey substitution | High | Medium | Planned — key fingerprints |
| Replay attacks | Medium | Low | Planned — message IDs |
| Low-entropy room passwords | Medium | Low | Planned — Argon2 KDF |
| Message size metadata | Low | Low | Planned — padding |
| Forward secrecy | High | Very High | Future consideration |
| Display name impersonation | Medium | Medium | Planned — key fingerprints |

---

## Planned Mitigations

### Key fingerprints
Derive a short human-readable code from the ECDH shared secret between
two peers. Both sides can read this code out-of-band (voice, in person)
to verify they are talking to the intended party and no MITM is present.

```
/verify JAY
[sys] your fingerprint with JAY: wolf-table-seven-rain
      confirm with JAY out of band
```

### Message IDs + deduplication
Add a random 8-byte message ID to each envelope. Recipients maintain a
short-window deduplication buffer. Replayed messages within the window
are silently dropped.

### Argon2id for room key derivation
Replace direct HKDF of room name with Argon2id to make brute-force
of weak room passwords computationally expensive.

```
key = Argon2id(password=roomName, salt="porfavor-room-v2", time=1, memory=64MB)
```

### Message padding
Pad all message plaintexts to the nearest fixed size bucket (64, 256,
1024, 4096 bytes) before encryption. Reduces size-based metadata leakage.

---

## Nonce Security Note

Por Favor uses random 12-byte nonces for ChaCha20-Poly1305. The
probability of nonce collision for a single key is negligible at
typical message volumes (2^48 messages before 50% collision probability).
However, for high-volume use cases, a counter-based nonce scheme would
provide stronger guarantees. This is noted for future consideration.

---

## Scope Statement

Por Favor is designed to protect casual to moderate privacy-sensitive
communications from passive surveillance, curious service providers,
and opportunistic attackers.

It is NOT designed for threat models involving:
- Nation-state adversaries with relay infrastructure access
- Targeted attacks against specific individuals
- Situations requiring legally deniable communications
- Long-term operational security (OPSEC)

For those use cases, consider tools with formal security audits,
such as Signal (with phone number) or Briar (P2P, no server).

---

## Reporting Security Issues

Found something? Open a private issue on GitHub or contact the
maintainer directly. We will respond within 48 hours and credit
responsible disclosure.

---

*This document will be updated as mitigations are implemented.*
