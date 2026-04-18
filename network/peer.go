package network

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/grandcat/zeroconf"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

const (
	ServiceType   = "_porfavor._tcp"
	ServicePort   = 47200
	ServiceDomain = "local."
)

type MsgType string

const (
	MsgChat    MsgType = "chat"
	MsgDM      MsgType = "dm"
	MsgOneTime MsgType = "onetime"
	MsgTyping  MsgType = "typing"
	MsgJoin    MsgType = "join"
	MsgLeave   MsgType = "leave"
	MsgNick    MsgType = "nick"
	MsgMe      MsgType = "me"
	MsgError   MsgType = "error"
)

type Envelope struct {
	Type    MsgType `json:"t"`
	From    string  `json:"f"`
	To      string  `json:"to,omitempty"`
	Body    string  `json:"b,omitempty"`
	Nonce   []byte  `json:"n,omitempty"`
	Payload []byte  `json:"p,omitempty"`
	PubKey  []byte  `json:"pk,omitempty"` // X25519 pubkey, sent in MsgJoin for E2E
}

type Peer struct {
	Name      string
	Conn      net.Conn
	SharedKey []byte
	mu        sync.Mutex
}

type Manager struct {
	LocalName string
	peers     map[string]*Peer
	mu        sync.RWMutex
	Incoming  chan Envelope

	privKey     *ecdh.PrivateKey
	pubKeyBytes []byte

	connecting map[string]bool
	connMu     sync.Mutex

	quit   chan struct{}
	once   sync.Once
}

func NewManager(name string) *Manager {
	curve := ecdh.X25519()
	priv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		panic("failed to generate ECDH key: " + err.Error())
	}

	return &Manager{
		LocalName:   name,
		peers:       make(map[string]*Peer),
		Incoming:    make(chan Envelope, 128),
		privKey:     priv,
		pubKeyBytes: priv.PublicKey().Bytes(),
		connecting:  make(map[string]bool),
		quit:        make(chan struct{}),
	}
}

func (m *Manager) Start() {
	go m.listenForIncoming()
	time.Sleep(100 * time.Millisecond)
	go m.announceSelf()
	go m.browsePeers()
}

// Shutdown closes all peer connections and stops all network activity.
func (m *Manager) Shutdown() {
	m.once.Do(func() {
		close(m.quit)
		m.mu.Lock()
		for _, p := range m.peers {
			p.Conn.Close()
		}
		m.mu.Unlock()
	})
}

func (m *Manager) announceSelf() {
	server, err := zeroconf.Register(m.LocalName, ServiceType, ServiceDomain, ServicePort, nil, nil)
	if err != nil {
		m.sendError("mDNS registration failed — peers may not find you: " + err.Error())
		return
	}
	defer server.Shutdown()
	<-m.quit
}

func (m *Manager) browsePeers() {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		m.sendError("mDNS discovery failed — you may not find peers: " + err.Error())
		return
	}

	entries := make(chan *zeroconf.ServiceEntry)
	go func() {
		for entry := range entries {
			if entry.Instance == m.LocalName {
				continue
			}
			ip := pickIP(entry.AddrIPv4, entry.AddrIPv6)
			if ip == nil {
				continue
			}
			go m.connectToPeer(ip, entry.Port, entry.Instance)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-m.quit
		cancel()
	}()
	_ = resolver.Browse(ctx, ServiceType, ServiceDomain, entries)
}

func pickIP(v4, v6 []net.IP) net.IP {
	for _, ip := range v4 {
		if !ip.IsLoopback() {
			return ip
		}
	}
	if len(v4) > 0 {
		return v4[0]
	}
	if len(v6) > 0 {
		return v6[0]
	}
	return nil
}

func setKeepalive(conn net.Conn) {
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetKeepAlive(true)
		_ = tc.SetKeepAlivePeriod(30 * time.Second)
	}
}

func (m *Manager) connectToPeer(ip net.IP, port int, name string) {
	m.connMu.Lock()
	if m.connecting[name] {
		m.connMu.Unlock()
		return
	}
	m.mu.RLock()
	_, already := m.peers[name]
	m.mu.RUnlock()
	if already {
		m.connMu.Unlock()
		return
	}
	m.connecting[name] = true
	m.connMu.Unlock()

	defer func() {
		m.connMu.Lock()
		delete(m.connecting, name)
		m.connMu.Unlock()
	}()

	addr := fmt.Sprintf("%s:%d", ip.String(), port)
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return
	}
	setKeepalive(conn)

	sharedKey, err := m.ecdhHandshake(conn, true)
	if err != nil {
		conn.Close()
		return
	}

	peer := &Peer{Name: name, Conn: conn, SharedKey: sharedKey}

	m.mu.Lock()
	if _, exists := m.peers[name]; exists {
		m.mu.Unlock()
		conn.Close()
		return
	}
	m.peers[name] = peer
	m.mu.Unlock()

	m.sendEnvelope(peer, Envelope{Type: MsgJoin, From: m.LocalName, Body: m.LocalName})
	m.Incoming <- Envelope{Type: MsgJoin, From: name}

	go m.readLoop(peer)
}

func (m *Manager) listenForIncoming() {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", ServicePort))
	if err != nil {
		m.sendError(fmt.Sprintf("failed to listen on port %d — incoming connections disabled: %s", ServicePort, err.Error()))
		return
	}
	defer ln.Close()

	// Close listener when shutdown is requested
	go func() {
		<-m.quit
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-m.quit:
				return
			default:
				continue
			}
		}
		setKeepalive(conn)
		go m.handleIncoming(conn)
	}
}

func (m *Manager) handleIncoming(conn net.Conn) {
	sharedKey, err := m.ecdhHandshake(conn, false)
	if err != nil {
		conn.Close()
		return
	}

	data, err := readFrame(conn)
	if err != nil {
		conn.Close()
		return
	}

	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil || env.Type != MsgJoin {
		conn.Close()
		return
	}

	name := env.Body
	if name == "" || name == m.LocalName {
		conn.Close()
		return
	}

	peer := &Peer{Name: name, Conn: conn, SharedKey: sharedKey}

	m.mu.Lock()
	if _, exists := m.peers[name]; exists {
		m.mu.Unlock()
		conn.Close()
		return
	}
	m.peers[name] = peer
	m.mu.Unlock()

	// Reply with our own join message
	m.sendEnvelope(peer, Envelope{Type: MsgJoin, From: m.LocalName, Body: m.LocalName})

	m.Incoming <- Envelope{Type: MsgJoin, From: name}

	go m.readLoop(peer)
}

func (m *Manager) ecdhHandshake(conn net.Conn, initiator bool) ([]byte, error) {
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	defer conn.SetDeadline(time.Time{})

	if err := writeFrame(conn, m.pubKeyBytes); err != nil {
		return nil, err
	}

	peerPubBytes, err := readFrame(conn)
	if err != nil {
		return nil, err
	}

	curve := ecdh.X25519()
	peerPub, err := curve.NewPublicKey(peerPubBytes)
	if err != nil {
		return nil, err
	}

	secret, err := m.privKey.ECDH(peerPub)
	if err != nil {
		return nil, err
	}

	hkdfReader := hkdf.New(sha256.New, secret, nil, []byte("porfavor-v1"))
	key := make([]byte, chacha20poly1305.KeySize)
	if _, err := io.ReadFull(hkdfReader, key); err != nil {
		return nil, err
	}

	_ = initiator
	return key, nil
}

func (m *Manager) readLoop(p *Peer) {
	defer func() {
		p.Conn.Close()
		m.mu.Lock()
		delete(m.peers, p.Name)
		m.mu.Unlock()
		m.Incoming <- Envelope{Type: MsgLeave, From: p.Name}
	}()

	for {
		data, err := readFrame(p.Conn)
		if err != nil {
			return
		}

		var env Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			continue
		}

		if len(env.Payload) > 0 && len(env.Nonce) > 0 {
			plain, err := decryptMsg(p.SharedKey, env.Nonce, env.Payload)
			if err != nil {
				continue
			}
			env.Body = string(plain)
			env.Payload = nil
			env.Nonce = nil
		}

		switch env.Type {
		case MsgNick:
			oldName := p.Name
			p.Name = env.Body
			m.mu.Lock()
			delete(m.peers, oldName)
			m.peers[p.Name] = p
			m.mu.Unlock()
		}

		m.Incoming <- env
	}
}

func (m *Manager) Send(env Envelope) {
	m.mu.RLock()
	peers := make([]*Peer, 0, len(m.peers))
	for _, p := range m.peers {
		peers = append(peers, p)
	}
	m.mu.RUnlock()

	for _, p := range peers {
		enc := env
		enc.From = m.LocalName
		if isEncrypted(enc.Type) {
			nonce, payload, err := encryptMsg(p.SharedKey, []byte(enc.Body))
			if err != nil {
				continue
			}
			enc.Nonce = nonce
			enc.Payload = payload
			enc.Body = ""
		}
		m.sendEnvelope(p, enc)
	}
}

// SendTo sends a message to a specific peer by name.
// Returns false if the peer is not found.
func (m *Manager) SendTo(name string, env Envelope) bool {
	m.mu.RLock()
	p, ok := m.peers[name]
	m.mu.RUnlock()
	if !ok {
		return false
	}

	enc := env
	enc.From = m.LocalName
	if isEncrypted(enc.Type) {
		nonce, payload, err := encryptMsg(p.SharedKey, []byte(enc.Body))
		if err != nil {
			return false
		}
		enc.Nonce = nonce
		enc.Payload = payload
		enc.Body = ""
	}
	m.sendEnvelope(p, enc)
	return true
}

func (m *Manager) SendTypingStart() {
	m.Send(Envelope{Type: MsgTyping, Body: "1"})
}

func (m *Manager) SendTypingStop() {
	m.Send(Envelope{Type: MsgTyping, Body: "0"})
}

func (m *Manager) Messages() <-chan Envelope {
	return m.Incoming
}

func (m *Manager) Peers() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.peers))
	for name := range m.peers {
		names = append(names, name)
	}
	return names
}

func (m *Manager) HasPeer(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.peers[name]
	return ok
}

// ConnectToAddr manually connects to a peer by IP address.
// Port is optional — defaults to ServicePort if not specified.
// Format: "192.168.1.5" or "192.168.1.5:47200"
func (m *Manager) ConnectToAddr(addr string) error {
	// Add default port if missing
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = fmt.Sprintf("%s:%d", addr, ServicePort)
	}

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("could not connect to %s: %w", addr, err)
	}
	setKeepalive(conn)

	sharedKey, err := m.ecdhHandshake(conn, true)
	if err != nil {
		conn.Close()
		return fmt.Errorf("handshake failed: %w", err)
	}

	// Send our join message first
	if err := m.sendEnvelopeRaw(conn, Envelope{Type: MsgJoin, From: m.LocalName, Body: m.LocalName}); err != nil {
		conn.Close()
		return fmt.Errorf("join failed: %w", err)
	}

	// Read peer's join message to learn their name
	data, err := readFrame(conn)
	if err != nil {
		conn.Close()
		return fmt.Errorf("no response from peer: %w", err)
	}

	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil || env.Type != MsgJoin {
		conn.Close()
		return fmt.Errorf("unexpected response from peer")
	}

	name := env.Body
	if name == "" {
		conn.Close()
		return fmt.Errorf("peer sent empty name")
	}

	peer := &Peer{Name: name, Conn: conn, SharedKey: sharedKey}

	m.mu.Lock()
	if _, exists := m.peers[name]; exists {
		m.mu.Unlock()
		conn.Close()
		return fmt.Errorf("already connected to %s", name)
	}
	m.peers[name] = peer
	m.mu.Unlock()

	m.Incoming <- Envelope{Type: MsgJoin, From: name}
	go m.readLoop(peer)
	return nil
}

func (m *Manager) UpdateName(newName string) {
	old := m.LocalName
	m.LocalName = newName
	m.Send(Envelope{Type: MsgNick, From: old, Body: newName})
}

func (m *Manager) sendEnvelope(p *Peer, env Envelope) {
	data, err := json.Marshal(env)
	if err != nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_ = writeFrame(p.Conn, data)
}

func (m *Manager) sendEnvelopeRaw(conn net.Conn, env Envelope) error {
	data, err := json.Marshal(env)
	if err != nil {
		return err
	}
	return writeFrame(conn, data)
}

func (m *Manager) sendError(msg string) {
	select {
	case m.Incoming <- Envelope{Type: MsgError, Body: msg}:
	default:
	}
}

func isEncrypted(t MsgType) bool {
	return t == MsgChat || t == MsgDM || t == MsgMe || t == MsgOneTime
}

func writeFrame(conn net.Conn, data []byte) error {
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, uint32(len(data)))
	if _, err := conn.Write(header); err != nil {
		return err
	}
	_, err := conn.Write(data)
	return err
}

func readFrame(conn net.Conn) ([]byte, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint32(header)
	if size == 0 || size > 1<<20 {
		return nil, fmt.Errorf("invalid frame size: %d", size)
	}
	buf := make([]byte, size)
	_, err := io.ReadFull(conn, buf)
	return buf, err
}

func encryptMsg(key, plaintext []byte) (nonce, ciphertext []byte, err error) {
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	ciphertext = aead.Seal(nil, nonce, plaintext, nil)
	return nonce, ciphertext, nil
}

func decryptMsg(key, nonce, ciphertext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, err
	}
	return aead.Open(nil, nonce, ciphertext, nil)
}
