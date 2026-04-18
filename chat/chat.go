package chat

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
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
	mgr         *network.Manager
	name        string
	inputBuf    []rune
	typing      bool
	typingTimer *time.Timer
	typingFrom  string
	mu          sync.Mutex
	oldState    *term.State
	rawMode     bool
}

func New(mgr *network.Manager, name string) *Chat {
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
	for env := range c.mgr.Incoming {
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
			c.sysf("%s left the chat", env.From)

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

		case network.MsgTyping:
			if env.Body == "1" {
				c.typingFrom = env.From
				fmt.Printf("%s[sys] %s is typing...%s\r\n", dim+green, env.From, reset)
			} else if c.typingFrom == env.From {
				c.typingFrom = ""
			}

		case network.MsgNick:
			c.sysf("%s is now known as %s", env.From, env.Body)
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
	c.printMsg(c.name, line, true)
	c.mu.Unlock()
	c.mgr.Send(network.Envelope{Type: network.MsgChat, Body: line})
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
		fmt.Print("  /dm <name> <message>   — private message\r\n")
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

	case "/dm":
		if len(parts) < 3 {
			c.mu.Lock()
			c.sysf("usage: /dm <name> <message>")
			c.mu.Unlock()
			return
		}
		target := parts[1]
		msg := strings.Join(parts[2:], " ")
		c.mu.Lock()
		fmt.Printf("%s[DM → %s] %s%s\r\n", brightGreen, target, msg, reset)
		c.mu.Unlock()
		c.mgr.SendTo(target, network.Envelope{Type: network.MsgDM, To: target, Body: msg})

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
	fmt.Printf("%s%s [%s]  %s█%s", green, ts, c.name, buf, reset)
}

func (c *Chat) redrawInput() {
	ts := time.Now().Format("15:04:05")
	buf := string(c.inputBuf)
	fmt.Printf("%s%s%s [%s]  %s█%s", clearLine, green, ts, c.name, buf, reset)
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
	time.Sleep(100 * time.Millisecond)
	fmt.Printf("\r\n%s[sys] goodbye.%s\r\n", green, reset)
	os.Exit(0)
}
