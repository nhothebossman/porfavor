package network

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const DefaultRelayURL = "wss://porfavor-relay.relayporfavor.workers.dev"

type OnlineManager struct {
	LocalName string

	serverURL string
	wsConn    *websocket.Conn
	writeMu   sync.Mutex  // gorilla requires single concurrent writer
	connMu    sync.RWMutex

	incoming chan Envelope
	peers    map[string]bool
	peersMu  sync.RWMutex

	quit chan struct{}
	once sync.Once
}

func NewOnlineManager(name, serverURL string) *OnlineManager {
	if serverURL == "" {
		serverURL = DefaultRelayURL
	}
	return &OnlineManager{
		LocalName: name,
		serverURL: serverURL,
		incoming:  make(chan Envelope, 128),
		peers:     make(map[string]bool),
		quit:      make(chan struct{}),
	}
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
		default:
		}

		conn, _, err := websocket.DefaultDialer.Dial(m.serverURL, nil)
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

		// Announce ourselves
		m.sendRaw(Envelope{Type: MsgJoin, From: m.LocalName, Body: m.LocalName})

		// Block until connection drops
		m.readLoop(conn)

		m.connMu.Lock()
		m.wsConn = nil
		m.connMu.Unlock()

		// Clear peer list on disconnect
		m.peersMu.Lock()
		m.peers = make(map[string]bool)
		m.peersMu.Unlock()

		select {
		case <-m.quit:
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

		// Maintain local peer list
		switch env.Type {
		case MsgJoin:
			m.peersMu.Lock()
			m.peers[env.From] = true
			m.peersMu.Unlock()
		case MsgLeave:
			m.peersMu.Lock()
			delete(m.peers, env.From)
			m.peersMu.Unlock()
		case MsgNick:
			m.peersMu.Lock()
			delete(m.peers, env.From)
			if env.Body != "" {
				m.peers[env.Body] = true
			}
			m.peersMu.Unlock()
		}

		m.incoming <- env
	}
}

func (m *OnlineManager) Send(env Envelope) {
	env.From = m.LocalName
	m.sendRaw(env)
}

func (m *OnlineManager) SendTo(name string, env Envelope) bool {
	env.From = m.LocalName
	env.To = name
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
	return m.peers[name]
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
