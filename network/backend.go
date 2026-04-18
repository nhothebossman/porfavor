package network

// Backend is the interface both LANManager and OnlineManager implement.
// chat.go talks only to this interface — it doesn't care which mode is active.
type Backend interface {
	Start()
	Send(env Envelope)
	SendTo(name string, env Envelope) bool
	SendTypingStart()
	SendTypingStop()
	Peers() []string
	HasPeer(name string) bool
	UpdateName(newName string)
	Shutdown()
	Messages() <-chan Envelope
}
