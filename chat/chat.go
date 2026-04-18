package chat

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"porfavor/network"

	"golang.org/x/term"
)

const (
	green       = "\033[32m"
	brightGreen = "\033[92m"
	dim         = "\033[2m"
	reset       = "\033[0m"
	clearLine   = "\033[2K\r"
	clearScreen = "\033[2J\033[H"
)

type Chat struct {
	mgr         network.Backend
	name        string
	inputBuf    []rune
	typing      bool
	typingTimer *time.Timer
	typingFrom  string
	dmTarget    string // non-empty = we're in a DM session with this peer
	mu          sync.Mutex
	oldState    *term.State
	rawMode     bool
}

func New(mgr network.Backend, name string) *Chat {
	return &Chat{mgr: mgr, name: name}
}

func (c *Chat) Run() {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
		if err == nil {
			c.oldState = oldState
			c.rawMode = true
		}
	}
	defer c.restore()

	// Handle Ctrl+C and kill signals gracefully
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		c.quit()
	}()

	c.bootSequence()
	go c.receiveLoop()
	c.inputLoop()
}

func (c *Chat) bootSequence() {
	c.sysf("booting por favor...")
	time.Sleep(80 * time.Millisecond)
	c.sysf("scanning network...")
	time.Sleep(1500 * time.Millisecond)

	peers := c.mgr.Peers()
	if len(peers) == 0 {
		c.sysf("no peers found. waiting...")
	} else {
		for _, p := range peers {
			c.sysf("peer discovered → %s", p)
		}
		c.sysf("channel open · %d peer(s) online ✓", len(peers))
	}
	c.printPrompt()
}

func (c *Chat) receiveLoop() {
	for env := range c.mgr.Messages() {
		c.mu.Lock()
		c.clearInput()

		switch env.Type {
		case network.MsgJoin:
			c.sysf("%s joined the chat", env.From)
			peers := c.mgr.Peers()
			c.sysf("channel open · %d peer(s) online ✓", len(peers))

		case network.MsgLeave:
			if c.typingFrom == env.From {
				c.typingFrom = ""
			}
			if c.dmTarget == env.From {
				c.dmTarget = ""
				c.sysf("%s left · DM session closed", env.From)
			} else {
				c.sysf("%s left the chat", env.From)
			}

		case network.MsgChat:
			if c.typingFrom == env.From {
				c.typingFrom = ""
			}
			c.printMsg(env.From, env.Body, false)

		case network.MsgMe:
			if c.typingFrom == env.From {
				c.typingFrom = ""
			}
			fmt.Printf("%s* %s %s%s\r\n", green, env.From, env.Body, reset)

		case network.MsgDM:
			fmt.Printf("%s[DM from %s] %s%s\r\n", brightGreen, env.From, env.Body, reset)

		case network.MsgOneTime:
			fmt.Printf("%s[● onetime from %s] %s%s\r\n", brightGreen, env.From, env.Body, reset)
			fmt.Printf("%s[sys] ● burned — this message no longer exists%s\r\n", dim+green, reset)

		case network.MsgTyping:
			if env.Body == "1" {
				c.typingFrom = env.From
				fmt.Printf("%s[sys] %s is typing...%s\r\n", dim+green, env.From, reset)
			} else if c.typingFrom == env.From {
				c.typingFrom = ""
			}

		case network.MsgNick:
			c.sysf("%s is now known as %s", env.From, env.Body)

		case network.MsgError:
			c.sysf("⚠  %s", env.Body)
		}

		c.printPrompt()
		c.mu.Unlock()
	}
}

func (c *Chat) inputLoop() {
	if !c.rawMode {
		c.inputLoopBuffered()
		return
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		r, _, err := reader.ReadRune()
		if err != nil {
			return
		}

		c.mu.Lock()

		switch r {
		case '\r', '\n':
			line := strings.TrimSpace(string(c.inputBuf))
			c.inputBuf = c.inputBuf[:0]
			c.stopTyping()
			fmt.Print(clearLine)
			c.mu.Unlock()

			if line != "" {
				c.dispatch(line)
			}

			c.mu.Lock()
			c.printPrompt()
			c.mu.Unlock()

		case 127, '\b':
			if len(c.inputBuf) > 0 {
				c.inputBuf = c.inputBuf[:len(c.inputBuf)-1]
			}
			c.redrawInput()
			c.mu.Unlock()

		case 3, 4: // Ctrl+C, Ctrl+D
			c.mu.Unlock()
			c.quit()
			return

		default:
			if utf8.ValidRune(r) && r >= 32 {
				c.inputBuf = append(c.inputBuf, r)
				c.startTyping()
				c.redrawInput()
			}
			c.mu.Unlock()
		}
	}
}

func (c *Chat) inputLoopBuffered() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			c.dispatch(line)
		}
	}
}

func (c *Chat) dispatch(line string) {
	if strings.HasPrefix(line, "/") {
		c.handleCommand(line)
		return
	}
	c.mu.Lock()
	target := c.dmTarget
	c.mu.Unlock()

	if target != "" {
		// In a DM session — send as DM
		c.mu.Lock()
		fmt.Printf("%s[DM → %s] %s%s\r\n", brightGreen, target, line, reset)
		c.mu.Unlock()
		c.mgr.SendTo(target, network.Envelope{Type: network.MsgDM, To: target, Body: line})
	} else {
		c.mu.Lock()
		c.printMsg(c.name, line, true)
		c.mu.Unlock()
		c.mgr.Send(network.Envelope{Type: network.MsgChat, Body: line})
	}
}

func (c *Chat) handleCommand(line string) {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return
	}
	cmd := strings.ToLower(parts[0])

	switch cmd {
	case "/help":
		c.mu.Lock()
		fmt.Printf("%s", green)
		fmt.Print("  /help                  — list commands\r\n")
		fmt.Print("  /peers                 — list who is online\r\n")
		fmt.Print("  /connect <ip>          — connect directly by IP (mDNS fallback)\r\n")
		fmt.Print("  /dm <name>             — open a private DM session\r\n")
		fmt.Print("  /dm <name> <message>   — send a one-off private message\r\n")
		fmt.Print("  /back                  — return to group chat from DM session\r\n")
		fmt.Print("  /onetime <name> \"msg\"  — burn-after-reading message (held until they connect)\r\n")
		fmt.Print("  /nick <newname>        — change your name\r\n")
		fmt.Print("  /me <action>           — action message\r\n")
		fmt.Print("  /clear                 — clear screen\r\n")
		fmt.Print("  /quit                  — exit\r\n")
		fmt.Printf("%s", reset)
		c.mu.Unlock()

	case "/peers":
		peers := c.mgr.Peers()
		c.mu.Lock()
		if len(peers) == 0 {
			c.sysf("no peers online")
		} else {
			for _, p := range peers {
				fmt.Printf("%s  · %s%s\r\n", green, p, reset)
			}
		}
		c.mu.Unlock()

	case "/connect":
		type connector interface {
			ConnectToAddr(addr string) error
		}
		conn, isLAN := c.mgr.(connector)
		if !isLAN {
			c.mu.Lock()
			c.sysf("/connect is only available in LAN mode (--lan)")
			c.mu.Unlock()
			return
		}
		if len(parts) < 2 {
			c.mu.Lock()
			c.sysf("usage: /connect <ip>  or  /connect <ip:port>")
			c.mu.Unlock()
			return
		}
		addr := parts[1]
		c.mu.Lock()
		c.sysf("connecting to %s...", addr)
		c.mu.Unlock()
		go func() {
			if err := conn.ConnectToAddr(addr); err != nil {
				c.mu.Lock()
				c.clearInput()
				c.sysf("connect failed: %s", err.Error())
				c.printPrompt()
				c.mu.Unlock()
			}
		}()

	case "/dm":
		if len(parts) < 2 {
			c.mu.Lock()
			c.sysf("usage: /dm <name>  or  /dm <name> <message>")
			c.mu.Unlock()
			return
		}
		target := parts[1]

		if !c.mgr.HasPeer(target) {
			c.mu.Lock()
			c.sysf("unknown peer: %s — try /peers", target)
			c.mu.Unlock()
			return
		}

		if len(parts) >= 3 {
			// One-liner DM: /dm <name> <message>
			msg := strings.Join(parts[2:], " ")
			c.mu.Lock()
			c.dmTarget = target
			fmt.Printf("%s[DM → %s] %s%s\r\n", brightGreen, target, msg, reset)
			c.mu.Unlock()
			c.mgr.SendTo(target, network.Envelope{Type: network.MsgDM, To: target, Body: msg})
		} else {
			// Session mode: /dm <name> — enter DM session
			c.mu.Lock()
			c.dmTarget = target
			c.sysf("DM session with %s · type normally to chat · /back to return", target)
			c.mu.Unlock()
		}

	case "/back":
		c.mu.Lock()
		if c.dmTarget == "" {
			c.sysf("not in a DM session")
		} else {
			c.sysf("back to group chat (was DM with %s)", c.dmTarget)
			c.dmTarget = ""
		}
		c.mu.Unlock()

	case "/nick":
		if len(parts) < 2 {
			c.mu.Lock()
			c.sysf("usage: /nick <newname>")
			c.mu.Unlock()
			return
		}
		newName := strings.ToUpper(parts[1])
		c.mu.Lock()
		old := c.name
		c.name = newName
		c.sysf("you are now known as %s (was %s)", newName, old)
		c.mu.Unlock()
		c.mgr.UpdateName(newName)

	case "/me":
		if len(parts) < 2 {
			return
		}
		action := strings.Join(parts[1:], " ")
		c.mu.Lock()
		fmt.Printf("%s* %s %s%s\r\n", green, c.name, action, reset)
		c.mu.Unlock()
		c.mgr.Send(network.Envelope{Type: network.MsgMe, Body: action})

	case "/clear":
		c.mu.Lock()
		fmt.Print(clearScreen)
		c.mu.Unlock()

	case "/onetime":
		// Parse: /onetime <name> "message in quotes"
		if len(parts) < 2 {
			c.mu.Lock()
			c.sysf(`usage: /onetime <name> "your message"`)
			c.mu.Unlock()
			return
		}
		target := parts[1]
		// Extract quoted message from the raw line
		q1 := strings.Index(line, `"`)
		q2 := strings.LastIndex(line, `"`)
		if q1 == -1 || q1 == q2 {
			c.mu.Lock()
			c.sysf(`message must be in quotes: /onetime %s "your message"`, target)
			c.mu.Unlock()
			return
		}
		msg := line[q1+1 : q2]
		if msg == "" {
			c.mu.Lock()
			c.sysf("message cannot be empty")
			c.mu.Unlock()
			return
		}
		c.mu.Lock()
		c.sysf("● onetime sealed for %s · will be delivered when they open it", target)
		c.mu.Unlock()
		c.mgr.SendTo(target, network.Envelope{Type: network.MsgOneTime, To: target, Body: msg})

	case "/quit", "/exit":
		c.quit()

	default:
		c.mu.Lock()
		c.sysf("unknown command: %s  (try /help)", cmd)
		c.mu.Unlock()
	}
}

// startTyping must be called with c.mu held.
func (c *Chat) startTyping() {
	if !c.typing {
		c.typing = true
		go c.mgr.SendTypingStart()
		c.typingTimer = time.AfterFunc(3*time.Second, func() {
			c.mu.Lock()
			c.stopTyping()
			c.mu.Unlock()
		})
	} else if c.typingTimer != nil {
		c.typingTimer.Reset(3 * time.Second)
	}
}

// stopTyping must be called with c.mu held.
func (c *Chat) stopTyping() {
	if c.typing {
		c.typing = false
		if c.typingTimer != nil {
			c.typingTimer.Stop()
			c.typingTimer = nil
		}
		go c.mgr.SendTypingStop()
	}
}

func (c *Chat) printMsg(name, body string, isSelf bool) {
	ts := time.Now().Format("15:04:05")
	if isSelf {
		fmt.Printf("%s%s [%s]  %s%s\r\n", brightGreen, ts, name, body, reset)
	} else {
		fmt.Printf("%s%s [%s]  %s%s\r\n", green, ts, name, body, reset)
	}
}

// sysf prints a [sys] line. Must be called with c.mu held (or before goroutines start).
func (c *Chat) sysf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Printf("%s[sys] %s%s\r\n", dim+green, msg, reset)
}

func (c *Chat) printPrompt() {
	ts := time.Now().Format("15:04:05")
	buf := string(c.inputBuf)
	if c.dmTarget != "" {
		fmt.Printf("%s%s [%s → %s]  %s█%s", brightGreen, ts, c.name, c.dmTarget, buf, reset)
	} else {
		fmt.Printf("%s%s [%s]  %s█%s", green, ts, c.name, buf, reset)
	}
}

func (c *Chat) redrawInput() {
	ts := time.Now().Format("15:04:05")
	buf := string(c.inputBuf)
	if c.dmTarget != "" {
		fmt.Printf("%s%s%s [%s → %s]  %s█%s", clearLine, brightGreen, ts, c.name, c.dmTarget, buf, reset)
	} else {
		fmt.Printf("%s%s%s [%s]  %s█%s", clearLine, green, ts, c.name, buf, reset)
	}
}

func (c *Chat) clearInput() {
	fmt.Print(clearLine)
}

func (c *Chat) restore() {
	if c.rawMode && c.oldState != nil {
		_ = term.Restore(int(os.Stdin.Fd()), c.oldState)
	}
}

func (c *Chat) quit() {
	c.restore()
	c.mgr.Send(network.Envelope{Type: network.MsgLeave})
	c.mgr.Shutdown()
	time.Sleep(150 * time.Millisecond)
	fmt.Printf("\r\n%s[sys] goodbye.%s\r\n", green, reset)
	os.Exit(0)
}
