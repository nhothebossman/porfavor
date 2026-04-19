package network

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

const DefaultRelayURL = "wss://porfavor-relay.relayporfavor.workers.dev"

type onlinePeer struct {
	dmKey []byte // per-pair ECDH key for DMs
}

type OnlineManager struct {
	LocalName string

	serverURL string // full URL including room path
	roomName  string // original room name (for /invite and /room)
	roomKey   []byte // 32-byte key derived from room password (group encryption)
	baseURL   string // relay URL without room path (for switching rooms)
	expiresAt int64  // unix timestamp (seconds) when room expires; 0 = no expiry

	privKey     *ecdh.PrivateKey
	pubKeyBytes []byte

	wsConn  *websocket.Conn
	writeMu sync.Mutex
	connMu  sync.RWMutex

	incoming chan Envelope
	peers    map[string]*onlinePeer
	peersMu  sync.RWMutex

	quit    chan struct{}
	expired chan struct{} // closed when relay broadcasts MsgExpiry
	once    sync.Once
}

// NewOnlineManager creates a manager that connects to the relay.
// roomName is used to derive both the room encryption key and the room path.
// Defaults to "default" if empty.
// expiresAt is a unix timestamp (seconds) for room expiry; pass 0 for no expiry.
func NewOnlineManager(name, serverURL, roomName string, expiresAt int64) *OnlineManager {
	if serverURL == "" {
		serverURL = DefaultRelayURL
	}
	if roomName == "" {
		roomName = "default"
	}

	// Derive room path — hash of room name, used as WebSocket URL path
	pathHash := sha256.Sum256([]byte("porfavor-path:" + roomName))
	roomPath := hex.EncodeToString(pathHash[:16])

	// Derive room key — used for group message encryption
	roomKey := deriveRoomKey(roomName)

	// Generate fresh X25519 keypair for this session
	curve := ecdh.X25519()
	priv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		panic("failed to generate ECDH key: " + err.Error())
	}

	return &OnlineManager{
		LocalName:   name,
		serverURL:   serverURL + "/room/" + roomPath,
		roomName:    roomName,
		roomKey:     roomKey,
		baseURL:     serverURL,
		expiresAt:   expiresAt,
		privKey:     priv,
		pubKeyBytes: priv.PublicKey().Bytes(),
		incoming:    make(chan Envelope, 128),
		peers:       make(map[string]*onlinePeer),
		quit:        make(chan struct{}),
		expired:     make(chan struct{}),
	}
}

// RoomName returns the current room name (used by /invite).
func (m *OnlineManager) RoomName() string {
	return m.roomName
}

// SwitchRoom leaves the current room and joins a new one mid-session.
func (m *OnlineManager) SwitchRoom(newRoom string) {
	if newRoom == "" {
		newRoom = "default"
	}

	// Leave current room
	m.sendRaw(Envelope{Type: MsgLeave, From: m.LocalName, Body: m.LocalName})

	// Derive new room path and key
	pathHash := sha256.Sum256([]byte("porfavor-path:" + newRoom))
	roomPath := hex.EncodeToString(pathHash[:16])

	// Fresh keypair for the new room session
	curve := ecdh.X25519()
	if priv, err := curve.GenerateKey(rand.Reader); err == nil {
		m.privKey = priv
		m.pubKeyBytes = priv.PublicKey().Bytes()
	}

	// Update room state
	m.roomName = newRoom
	m.roomKey = deriveRoomKey(newRoom)
	m.serverURL = m.baseURL + "/room/" + roomPath
	m.expiresAt = 0 // new room does not inherit expiry

	// Clear peers
	m.peersMu.Lock()
	m.peers = make(map[string]*onlinePeer)
	m.peersMu.Unlock()

	// Force reconnect — connectLoop will pick up the new serverURL
	m.connMu.Lock()
	if m.wsConn != nil {
		m.wsConn.Close()
	}
	m.connMu.Unlock()
}

// deriveRoomKey derives a 32-byte room encryption key from the room name.
// Argon2id makes brute-forcing weak room names expensive even on GPU hardware.
// Parameters: time=3, memory=64MB, threads=4 — ~200ms on typical hardware.
// Salt is deterministic so all peers independently derive the same key.
func deriveRoomKey(roomName string) []byte {
	salt := sha256.Sum256([]byte("porfavor-room-salt-v2:" + roomName))
	return argon2.IDKey([]byte(roomName), salt[:], 3, 64*1024, 4, chacha20poly1305.KeySize)
}

func deriveSharedKey(priv *ecdh.PrivateKey, peerPubBytes []byte) ([]byte, error) {
	curve := ecdh.X25519()
	peerPub, err := curve.NewPublicKey(peerPubBytes)
	if err != nil {
		return nil, err
	}
	secret, err := priv.ECDH(peerPub)
	if err != nil {
		return nil, err
	}
	r := hkdf.New(sha256.New, secret, nil, []byte("porfavor-dm-v1"))
	key := make([]byte, chacha20poly1305.KeySize)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, err
	}
	return key, nil
}

func (m *OnlineManager) Messages() <-chan Envelope {
	return m.incoming
}

func (m *OnlineManager) Start() {
	go m.connectLoop()
}

func (m *OnlineManager) connectLoop() {
	first := true
	for {
		select {
		case <-m.quit:
			return
		case <-m.expired:
			return
		default:
		}

		dialer := websocket.Dialer{
			NetDial: func(network, addr string) (net.Conn, error) {
				// Bypass broken system DNS (common on Termux), use Google DNS
				// and force IPv4 (IPv6 unreachable on many mobile networks)
				resolver := &net.Resolver{
					PreferGo: true,
					Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
						return net.Dial("udp", "8.8.8.8:53")
					},
				}
				host, port, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				addrs, err := resolver.LookupHost(context.Background(), host)
				if err != nil {
					return nil, err
				}
				for _, a := range addrs {
					if net.ParseIP(a).To4() != nil {
						return net.Dial("tcp4", net.JoinHostPort(a, port))
					}
				}
				return net.Dial("tcp4", net.JoinHostPort(addrs[0], port))
			},
		}

		conn, _, err := dialer.Dial(m.serverURL, nil)
		if err != nil {
			m.sendError("relay unreachable — retrying in 5s: " + err.Error())
			select {
			case <-m.quit:
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}

		m.connMu.Lock()
		m.wsConn = conn
		m.connMu.Unlock()

		if !first {
			m.incoming <- Envelope{Type: MsgError, Body: "reconnected to relay ✓"}
		}
		first = false

		// Announce ourselves with our public key for E2E key exchange.
		// Include ExpiresAt if this session created the room with --expires.
		joinEnv := Envelope{
			Type:   MsgJoin,
			From:   m.LocalName,
			Body:   m.LocalName,
			PubKey: m.pubKeyBytes,
		}
		if m.expiresAt != 0 {
			joinEnv.ExpiresAt = m.expiresAt
			remaining := time.Until(time.Unix(m.expiresAt, 0))
			if remaining > 0 {
				m.incoming <- Envelope{
					Type: MsgError,
					Body: fmt.Sprintf("⏳ room expires in %s (at %s)",
						formatDuration(remaining),
						time.Unix(m.expiresAt, 0).Format("15:04:05")),
				}
			}
		}
		m.sendRaw(joinEnv)

		m.readLoop(conn)

		m.connMu.Lock()
		m.wsConn = nil
		m.connMu.Unlock()

		// Clear peer state on disconnect
		m.peersMu.Lock()
		m.peers = make(map[string]*onlinePeer)
		m.peersMu.Unlock()

		select {
		case <-m.quit:
			return
		case <-m.expired:
			return
		case <-time.After(3 * time.Second):
			m.sendError("relay disconnected, reconnecting...")
		}
	}
}

func (m *OnlineManager) readLoop(conn *websocket.Conn) {
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var env Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			continue
		}

		// Decrypt encrypted messages before pushing to UI
		if len(env.Payload) > 0 && len(env.Nonce) > 0 {
			key := m.decryptKey(env)
			plain, err := decryptMsg(key, env.Nonce, env.Payload)
			if err != nil {
				// Wrong room key = wrong room — silently drop
				continue
			}
			env.Body = string(plain)
			env.Payload = nil
			env.Nonce = nil
		}

		// Maintain peer state
		switch env.Type {
		case MsgJoin:
			peer := &onlinePeer{}
			if len(env.PubKey) > 0 {
				if dmKey, err := deriveSharedKey(m.privKey, env.PubKey); err == nil {
					peer.dmKey = dmKey
				}
			}
			m.peersMu.Lock()
			m.peers[env.From] = peer
			m.peersMu.Unlock()

		case MsgLeave:
			m.peersMu.Lock()
			delete(m.peers, env.From)
			m.peersMu.Unlock()

		case MsgNick:
			m.peersMu.Lock()
			peer := m.peers[env.From]
			if peer == nil {
				peer = &onlinePeer{}
			}
			delete(m.peers, env.From)
			m.peers[env.Body] = peer
			m.peersMu.Unlock()

		case MsgExpiry:
			// Signal that the room has expired — connectLoop will not attempt to reconnect.
			select {
			case <-m.expired: // already closed
			default:
				close(m.expired)
			}
		}

		m.incoming <- env
	}
}

// decryptKey returns the right key for decrypting an envelope.
// DMs use the per-pair ECDH key; everything else uses the room key.
func (m *OnlineManager) decryptKey(env Envelope) []byte {
	if env.Type == MsgDM {
		m.peersMu.RLock()
		peer := m.peers[env.From]
		m.peersMu.RUnlock()
		if peer != nil && peer.dmKey != nil {
			return peer.dmKey
		}
	}
	return m.roomKey
}

func (m *OnlineManager) Send(env Envelope) {
	env.From = m.LocalName
	if isEncrypted(env.Type) {
		nonce, payload, err := encryptMsg(m.roomKey, []byte(env.Body))
		if err == nil {
			env.Nonce = nonce
			env.Payload = payload
			env.Body = ""
		}
	}
	m.sendRaw(env)
}

func (m *OnlineManager) SendTo(name string, env Envelope) bool {
	env.From = m.LocalName
	env.To = name

	if isEncrypted(env.Type) {
		// Use per-pair DM key if available, fall back to room key
		key := m.roomKey
		m.peersMu.RLock()
		peer := m.peers[name]
		m.peersMu.RUnlock()
		if peer != nil && peer.dmKey != nil {
			key = peer.dmKey
		}
		nonce, payload, err := encryptMsg(key, []byte(env.Body))
		if err == nil {
			env.Nonce = nonce
			env.Payload = payload
			env.Body = ""
		}
	}
	m.sendRaw(env)
	return true
}

func (m *OnlineManager) sendRaw(env Envelope) {
	data, err := json.Marshal(env)
	if err != nil {
		return
	}
	m.connMu.RLock()
	conn := m.wsConn
	m.connMu.RUnlock()
	if conn == nil {
		return
	}
	m.writeMu.Lock()
	_ = conn.WriteMessage(websocket.TextMessage, data)
	m.writeMu.Unlock()
}

func (m *OnlineManager) SendTypingStart() {
	m.Send(Envelope{Type: MsgTyping, Body: "1"})
}

func (m *OnlineManager) SendTypingStop() {
	m.Send(Envelope{Type: MsgTyping, Body: "0"})
}

func (m *OnlineManager) Peers() []string {
	m.peersMu.RLock()
	defer m.peersMu.RUnlock()
	names := make([]string, 0, len(m.peers))
	for name := range m.peers {
		names = append(names, name)
	}
	return names
}

func (m *OnlineManager) HasPeer(name string) bool {
	m.peersMu.RLock()
	defer m.peersMu.RUnlock()
	_, ok := m.peers[name]
	return ok
}

// DMKeyFingerprint returns a short human-readable fingerprint of the DM key
// shared with peer. Both sides display the same fingerprint if the relay did
// not substitute any pubkeys during key exchange (MITM check).
// Returns "" if no DM key exists yet for that peer.
func (m *OnlineManager) DMKeyFingerprint(peer string) string {
	m.peersMu.RLock()
	p := m.peers[peer]
	m.peersMu.RUnlock()
	if p == nil || p.dmKey == nil {
		return ""
	}
	hash := sha256.Sum256(p.dmKey)
	parts := make([]string, 8)
	for i := range parts {
		parts[i] = fmt.Sprintf("%02x", hash[i])
	}
	return strings.Join(parts, ":")
}

func (m *OnlineManager) UpdateName(newName string) {
	old := m.LocalName
	m.LocalName = newName
	m.Send(Envelope{Type: MsgNick, From: old, Body: newName})
}

func (m *OnlineManager) Shutdown() {
	m.once.Do(func() {
		m.sendRaw(Envelope{Type: MsgLeave, From: m.LocalName, Body: m.LocalName})
		close(m.quit)
		m.connMu.Lock()
		if m.wsConn != nil {
			m.wsConn.Close()
		}
		m.connMu.Unlock()
	})
}

func (m *OnlineManager) sendError(msg string) {
	select {
	case m.incoming <- Envelope{Type: MsgError, Body: msg}:
	default:
	}
}

// WipeKeys zeroes out all encryption keys held in memory.
// Called when the room expires so keys can't be reused after exit.
func (m *OnlineManager) WipeKeys() {
	for i := range m.roomKey {
		m.roomKey[i] = 0
	}
	m.peersMu.Lock()
	for _, p := range m.peers {
		for i := range p.dmKey {
			p.dmKey[i] = 0
		}
	}
	m.peersMu.Unlock()
}

// formatDuration formats a duration as a human-readable string (e.g. "1h30m", "45m", "30s").
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%dh%dm", h, m)
	case h > 0:
		return fmt.Sprintf("%dh", h)
	case m > 0 && s > 0:
		return fmt.Sprintf("%dm%ds", m, s)
	case m > 0:
		return fmt.Sprintf("%dm", m)
	default:
		return fmt.Sprintf("%ds", s)
	}
}
