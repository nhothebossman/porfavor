package chat

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
	"strconv"
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
	cyan        = "\033[36m"
	dim         = "\033[2m"
	reset       = "\033[0m"
	clearLine   = "\033[2K\r"
	clearScreen = "\033[2J\033[H"
)

// highlightCode replaces `backtick` segments with cyan ANSI colour, then
// restores baseColor. Unmatched backticks are left as-is.
func highlightCode(body, baseColor string) string {
	if !strings.Contains(body, "`") {
		return body
	}
	var out strings.Builder
	rest := body
	for {
		start := strings.Index(rest, "`")
		if start == -1 {
			out.WriteString(rest)
			break
		}
		out.WriteString(rest[:start])
		rest = rest[start+1:]
		end := strings.Index(rest, "`")
		if end == -1 {
			// No closing backtick — emit the opening backtick and continue
			out.WriteByte('`')
			out.WriteString(rest)
			break
		}
		out.WriteString(cyan)
		out.WriteString(rest[:end])
		out.WriteString(baseColor)
		rest = rest[end+1:]
	}
	return out.String()
}

// wrapText splits text into lines of at most width runes, breaking at spaces
// where possible. Returns the original text as a single element if width <= 0.
func wrapText(text string, width int) []string {
	runes := []rune(text)
	if width <= 0 || len(runes) <= width {
		return []string{text}
	}
	var lines []string
	for len(runes) > width {
		split := width
		// Walk back from the limit looking for a space to break on
		for split > width/2 && runes[split] != ' ' {
			split--
		}
		if runes[split] == ' ' {
			lines = append(lines, string(runes[:split]))
			runes = runes[split+1:] // consume the space
		} else {
			// No space in back half — hard break
			lines = append(lines, string(runes[:width]))
			runes = runes[width:]
		}
	}
	if len(runes) > 0 {
		lines = append(lines, string(runes))
	}
	return lines
}

const historyMax = 50
const dmHistoryMax = 5

type dmMessage struct {
	from string
	body string
	ts   time.Time
}

type Chat struct {
	mgr         network.Backend
	name        string
	inputBuf    []rune
	cursorPos   int // position within inputBuf; 0=start, len=end
	typing      bool
	typingTimer *time.Timer
	typingFrom  string
	dmTarget    string // non-empty = we're in a DM session with this peer
	awayMsg     string // non-empty = auto-reply this to incoming DMs

	// per-peer DM history (in-memory, current session only)
	dmHistory map[string][]dmMessage

	// history — ring buffer of sent lines, navigated with ↑/↓
	history   []string
	histIdx   int    // -1 = not navigating; 0 = most recent
	histDraft string // saved draft while navigating history

	// tab completion
	tabMatches []string
	tabIdx     int

	currentTopic string // room topic, shown on join and with /topic

	// Pending oneties — masked on arrival, shown only on /reveal
	pendingOnetime map[string]string // from → decrypted body

	// Idle screen lock
	lastInput time.Time
	locked    bool
	lockBuf   []rune

	mu       sync.Mutex
	oldState *term.State
	rawMode  bool
}

func New(mgr network.Backend, name string) *Chat {
	return &Chat{
		mgr:            mgr,
		name:           name,
		dmHistory:      make(map[string][]dmMessage),
		pendingOnetime: make(map[string]string),
		lastInput:      time.Now(),
	}
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
	go c.idleLockLoop()
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
	c.sysf("/help for commands · Tab to autocomplete · ? for usage hints")
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
			mentioned := strings.Contains(strings.ToLower(env.Body), "@"+strings.ToLower(c.name))
			c.printMsg(env.From, env.Body, false, mentioned)

		case network.MsgMe:
			if c.typingFrom == env.From {
				c.typingFrom = ""
			}
			fmt.Printf("%s* %s %s%s\r\n", green, env.From, env.Body, reset)

		case network.MsgDM:
			fmt.Printf("%s[DM from %s] %s%s\r\n", brightGreen, env.From, highlightCode(env.Body, brightGreen), reset)
			c.appendDMHistory(env.From, env.From, env.Body)
			if c.awayMsg != "" {
				from := env.From
				reply := c.awayMsg
				go c.mgr.SendTo(from, network.Envelope{Type: network.MsgDM, To: from, Body: "[away] " + reply})
			}

		case network.MsgBurn:
			fmt.Printf("%s[● %ds burn from %s] %s%s\r\n", brightGreen, env.TTL, env.From, env.Body, reset)
			ttl := env.TTL
			from := env.From
			go func() {
				time.Sleep(time.Duration(ttl) * time.Second)
				c.mu.Lock()
				c.clearInput()
				// Clear the screen when the burn expires — the content shouldn't linger
				fmt.Print(clearScreen)
				fmt.Printf("%s[sys] ● burn message from %s has expired · screen cleared%s\r\n", dim+green, from, reset)
				c.printPrompt()
				c.mu.Unlock()
			}()

		case network.MsgOneTime:
			// Store masked — do NOT display the content until /reveal is called.
			// The user may not be alone. They choose when to read it.
			c.pendingOnetime[env.From] = env.Body
			fmt.Printf("%s[sys] ● onetime from %s — type /reveal when you're alone%s\r\n", brightGreen, env.From, reset)
			fmt.Printf("%s[sys] ● message deleted from relay · exists only in this session%s\r\n", dim+green, reset)

		case network.MsgTyping:
			if env.Body == "1" {
				c.typingFrom = env.From
				fmt.Printf("%s[sys] %s is typing...%s\r\n", dim+green, env.From, reset)
			} else if c.typingFrom == env.From {
				c.typingFrom = ""
			}

		case network.MsgTopic:
			c.currentTopic = env.Body
			if env.From == "" {
				// Delivered by relay on join — show as room context
				c.sysf("topic: %s", env.Body)
			} else {
				c.sysf("%s set the topic: %s", env.From, env.Body)
			}

		case network.MsgNick:
			c.sysf("%s is now known as %s", env.From, env.Body)

		case network.MsgError:
			c.sysf("⚠  %s", env.Body)

		case network.MsgExpiry:
			// Room has been deleted by the relay. Wipe keys, show message, exit.
			type keyWiper interface{ WipeKeys() }
			if w, ok := c.mgr.(keyWiper); ok {
				w.WipeKeys()
			}
			fmt.Printf("\033[1m%s[sys] ⚠  %s%s\r\n", green, env.Body, reset)
			fmt.Printf("%s[sys] keys wiped — this room never existed.%s\r\n", dim+green, reset)
			c.printPrompt()
			c.mu.Unlock()
			go func() {
				time.Sleep(3 * time.Second)
				c.restore()
				fmt.Printf("\r\n%s[sys] connection closed.%s\r\n", green, reset)
				os.Exit(0)
			}()
			continue
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
	c.histIdx = -1

	for {
		r, _, err := reader.ReadRune()
		if err != nil {
			return
		}

		c.mu.Lock()

		// Update activity timestamp on every keypress
		c.lastInput = time.Now()

		// ── Locked: route all input to the unlock handler ─────────────────
		if c.locked {
			switch r {
			case '\r', '\n':
				attempt := strings.TrimSpace(string(c.lockBuf))
				c.lockBuf = c.lockBuf[:0]
				fmt.Print("\r\n")
				if attempt == c.lockRoomName() {
					c.locked = false
					fmt.Print(clearScreen)
					c.sysf("session restored · welcome back")
					c.printPrompt()
				} else {
					fmt.Printf("%s[sys] incorrect · try again%s\r\n\r\n", dim+green, reset)
					fmt.Printf("%s  unlock: %s", green, reset)
				}
			case 127, '\b':
				if len(c.lockBuf) > 0 {
					c.lockBuf = c.lockBuf[:len(c.lockBuf)-1]
					masked := strings.Repeat("*", len(c.lockBuf))
					fmt.Printf("\r%s  unlock: %s%s", green, masked, reset)
				}
			case 3, 4: // Ctrl+C / Ctrl+D — allow quit even when locked
				c.mu.Unlock()
				c.quit()
				return
			case '\x1b': // discard escape sequences (arrow keys etc.)
				c.mu.Unlock()
				reader.ReadByte() //nolint
				reader.ReadByte() //nolint
				continue
			default:
				if utf8.ValidRune(r) && r >= 32 {
					c.lockBuf = append(c.lockBuf, r)
					fmt.Printf("%s*%s", dim+green, reset)
				}
			}
			c.mu.Unlock()
			continue
		}

		// ── Normal input ──────────────────────────────────────────────────
		switch r {
		case '\r', '\n':
			line := strings.TrimSpace(string(c.inputBuf))
			c.inputBuf = c.inputBuf[:0]
			c.cursorPos = 0
			c.histIdx = -1
			c.histDraft = ""
			c.tabMatches = nil
			c.stopTyping()
			fmt.Print(clearLine)
			c.mu.Unlock()

			if line != "" {
				c.pushHistory(line)
				c.dispatch(line)
			}

			c.mu.Lock()
			c.printPrompt()
			c.mu.Unlock()

		case 127, '\b': // backspace — delete character left of cursor
			if c.cursorPos > 0 {
				copy(c.inputBuf[c.cursorPos-1:], c.inputBuf[c.cursorPos:])
				c.inputBuf = c.inputBuf[:len(c.inputBuf)-1]
				c.cursorPos--
			}
			c.tabMatches = nil
			c.redrawInput()
			c.mu.Unlock()

		case '\t': // tab completion
			c.doTabComplete()
			c.mu.Unlock()

		case 3, 4: // Ctrl+C, Ctrl+D
			c.mu.Unlock()
			c.quit()
			return

		case '\x1b': // ESC — start of an escape sequence
			c.mu.Unlock()
			// Read the rest of the sequence without holding the lock
			b1, err1 := reader.ReadByte()
			b2, err2 := reader.ReadByte()
			if err1 != nil || err2 != nil {
				continue
			}
			c.mu.Lock()
			if b1 == '[' {
				switch b2 {
				case 'A': // up arrow — older history
					c.historyUp()
				case 'B': // down arrow — newer history
					c.historyDown()
				case 'C': // right arrow — move cursor right
					if c.cursorPos < len(c.inputBuf) {
						c.cursorPos++
						c.redrawInput()
					}
				case 'D': // left arrow — move cursor left
					if c.cursorPos > 0 {
						c.cursorPos--
						c.redrawInput()
					}
				case 'H': // Home — jump to start
					c.cursorPos = 0
					c.redrawInput()
				case 'F': // End — jump to end
					c.cursorPos = len(c.inputBuf)
					c.redrawInput()
				}
			}
			c.mu.Unlock()

		default:
			// ? — show command usage hint.
			// Fires when: buffer is empty (show all), or buffer starts with / (show matches).
			// If the buffer has regular message text, ? is inserted normally.
			if r == '?' && (len(c.inputBuf) == 0 || c.inputBuf[0] == '/') {
				c.showCommandHint(string(c.inputBuf))
				c.redrawInput()
				c.mu.Unlock()
				continue
			}
			if utf8.ValidRune(r) && r >= 32 {
				// Insert at cursor position
				c.inputBuf = append(c.inputBuf, 0)
				copy(c.inputBuf[c.cursorPos+1:], c.inputBuf[c.cursorPos:])
				c.inputBuf[c.cursorPos] = r
				c.cursorPos++
				c.tabMatches = nil
				c.startTyping()
				c.redrawInput()
			}
			c.mu.Unlock()
		}
	}
}

// pushHistory adds a line to the history ring buffer.
// Must be called without the lock held.
func (c *Chat) pushHistory(line string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Don't duplicate consecutive identical entries
	if len(c.history) > 0 && c.history[0] == line {
		return
	}
	c.history = append([]string{line}, c.history...)
	if len(c.history) > historyMax {
		c.history = c.history[:historyMax]
	}
}

// historyUp moves back in history. Must be called with c.mu held.
func (c *Chat) historyUp() {
	if len(c.history) == 0 {
		return
	}
	if c.histIdx == -1 {
		// Save whatever is currently typed
		c.histDraft = string(c.inputBuf)
	}
	next := c.histIdx + 1
	if next >= len(c.history) {
		return
	}
	c.histIdx = next
	c.inputBuf = []rune(c.history[c.histIdx])
	c.cursorPos = len(c.inputBuf)
	c.redrawInput()
}

// historyDown moves forward in history. Must be called with c.mu held.
func (c *Chat) historyDown() {
	if c.histIdx == -1 {
		return
	}
	c.histIdx--
	if c.histIdx == -1 {
		c.inputBuf = []rune(c.histDraft)
	} else {
		c.inputBuf = []rune(c.history[c.histIdx])
	}
	c.cursorPos = len(c.inputBuf)
	c.redrawInput()
}

var allCommands = []string{
	"/help", "/peers", "/dm", "/back", "/onetime", "/reveal", "/burn", "/away",
	"/room", "/invite", "/nick", "/me", "/connect", "/clear", "/nuke", "/quit",
	"/verify", "/topic",
}

// commandHints provides one-line usage for each command, shown when ? is pressed.
var commandHints = map[string]string{
	"/help":    "/help  or  /help <command>",
	"/peers":   "/peers — list who is online",
	"/dm":      "/dm <name>  or  /dm <name> <message>",
	"/back":    "/back — return to group from a DM session",
	"/onetime": `/onetime <name> "message" — burn-after-reading, one delivery then gone`,
	"/reveal":  "/reveal  or  /reveal <name> — show a pending onetime privately",
	"/burn":    `/burn <seconds> "message" — self-destructs for everyone · max 300s`,
	"/away":    `/away "message"  or  /away — clear away status`,
	"/room":    "/room <name> — switch rooms mid-session (online only)",
	"/invite":  "/invite — print the join command for your current room",
	"/nick":    "/nick <newname> — change your display name",
	"/me":      "/me <action> — action message  * YOU action",
	"/connect": "/connect <ip>  or  /connect <ip:port> — LAN mode only",
	"/verify":  "/verify <name> — compare key fingerprints to detect MITM",
	"/topic":   `/topic "text"  or  /topic clear`,
	"/clear":   "/clear — clear the screen",
	"/nuke":    "/nuke — disconnect silently, no announcement, clear screen",
	"/quit":    "/quit — exit gracefully, announces departure",
}

// doTabComplete cycles through commands matching the current input prefix.
// Must be called with c.mu held.
func (c *Chat) doTabComplete() {
	buf := string(c.inputBuf)
	if !strings.HasPrefix(buf, "/") {
		return
	}
	// Build match list if we're starting a new completion
	if len(c.tabMatches) == 0 {
		c.tabIdx = 0
		for _, cmd := range allCommands {
			if strings.HasPrefix(cmd, buf) && cmd != buf {
				c.tabMatches = append(c.tabMatches, cmd)
			}
		}
		if len(c.tabMatches) == 0 {
			return
		}
	} else {
		c.tabIdx = (c.tabIdx + 1) % len(c.tabMatches)
	}
	c.inputBuf = []rune(c.tabMatches[c.tabIdx] + " ")
	c.cursorPos = len(c.inputBuf)
	c.redrawInput()
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
		fmt.Printf("%s[DM → %s] %s%s\r\n", brightGreen, target, highlightCode(line, brightGreen), reset)
		c.appendDMHistory(target, c.name, line)
		c.mu.Unlock()
		c.mgr.SendTo(target, network.Envelope{Type: network.MsgDM, To: target, Body: line})
	} else {
		c.mu.Lock()
		c.printMsg(c.name, line, true, false)
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
		if len(parts) >= 2 {
			c.printCommandHelp(parts[1])
		} else {
			c.printHelp()
		}
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
			if c.dmTarget != "" {
				// Already in a session — /dm with no args exits it
				c.sysf("leaving DM with %s · back to group chat", c.dmTarget)
				c.dmTarget = ""
			} else {
				c.sysf("usage: /dm <name>  or  /dm <name> <message>")
			}
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
			c.appendDMHistory(target, c.name, msg)
			c.mu.Unlock()
			c.mgr.SendTo(target, network.Envelope{Type: network.MsgDM, To: target, Body: msg})
		} else {
			// Session mode: /dm <name> — enter DM session
			c.mu.Lock()
			c.dmTarget = target
			c.sysf("DM session open with %s · /dm or /back to return to group", target)
			c.showDMHistory(target)
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
		newName := parts[1]
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

	case "/reveal":
		// /reveal [name] — show a pending onetime privately, then auto-clear
		c.mu.Lock()
		if len(c.pendingOnetime) == 0 {
			c.sysf("no pending onetimes")
			c.mu.Unlock()
			return
		}
		var from, body string
		if len(parts) >= 2 {
			from = parts[1]
			body = c.pendingOnetime[from]
			if body == "" {
				c.sysf("no pending onetime from %s", from)
				c.mu.Unlock()
				return
			}
		} else {
			// Show the first pending one
			for k, v := range c.pendingOnetime {
				from, body = k, v
				break
			}
		}
		delete(c.pendingOnetime, from)
		// Clear the screen, show the secret prominently, auto-clear after 15s
		fmt.Print(clearScreen)
		fmt.Printf("%s\r\n", brightGreen)
		fmt.Printf("  ── onetime from %s ──────────────────────────────%s\r\n", strings.ToUpper(from), reset)
		fmt.Printf("\r\n")
		fmt.Printf("%s  %s%s\r\n", brightGreen, body, reset)
		fmt.Printf("\r\n")
		fmt.Printf("%s  ── screen clears in 15 seconds ────────────────%s\r\n", dim+green, reset)
		c.printPrompt()
		go func() {
			time.Sleep(15 * time.Second)
			c.mu.Lock()
			fmt.Print(clearScreen)
			c.sysf("onetime cleared")
			c.printPrompt()
			c.mu.Unlock()
		}()
		c.mu.Unlock()

	case "/burn":
		if len(parts) < 2 {
			c.mu.Lock()
			c.sysf(`usage: /burn <seconds> "message"`)
			c.mu.Unlock()
			return
		}
		secs, err := strconv.Atoi(parts[1])
		if err != nil || secs < 1 || secs > 300 {
			c.mu.Lock()
			c.sysf("seconds must be between 1 and 300")
			c.mu.Unlock()
			return
		}
		q1 := strings.Index(line, `"`)
		q2 := strings.LastIndex(line, `"`)
		if q1 == -1 || q1 == q2 {
			c.mu.Lock()
			c.sysf(`message must be in quotes: /burn %d "your message"`, secs)
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
		c.sysf("● burn message sent · expires in %ds", secs)
		c.mu.Unlock()
		c.mgr.Send(network.Envelope{Type: network.MsgBurn, Body: msg, TTL: secs})

	case "/away":
		c.mu.Lock()
		if len(parts) < 2 {
			if c.awayMsg == "" {
				c.sysf("not currently away")
			} else {
				c.awayMsg = ""
				c.sysf("away status cleared")
			}
			c.mu.Unlock()
			return
		}
		q1 := strings.Index(line, `"`)
		q2 := strings.LastIndex(line, `"`)
		var msg string
		if q1 != -1 && q1 != q2 {
			msg = line[q1+1 : q2]
		} else {
			msg = strings.Join(parts[1:], " ")
		}
		c.awayMsg = msg
		c.sysf("away: %s", msg)
		c.mu.Unlock()

	case "/room":
		type switcher interface {
			SwitchRoom(name string)
		}
		sw, ok := c.mgr.(switcher)
		if !ok {
			c.mu.Lock()
			c.sysf("/room is only available in online mode")
			c.mu.Unlock()
			return
		}
		if len(parts) < 2 {
			c.mu.Lock()
			c.sysf("usage: /room <name>")
			c.mu.Unlock()
			return
		}
		newRoom := parts[1]
		c.mu.Lock()
		c.dmTarget = ""
		c.sysf("switching to room: %s...", newRoom)
		c.mu.Unlock()
		sw.SwitchRoom(newRoom)

	case "/invite":
		type roomer interface {
			RoomName() string
		}
		r, ok := c.mgr.(roomer)
		if !ok {
			c.mu.Lock()
			c.sysf("/invite is only available in online mode")
			c.mu.Unlock()
			return
		}
		c.mu.Lock()
		fmt.Printf("%s  invite: porfavor --room %s%s\r\n", brightGreen, r.RoomName(), reset)
		c.mu.Unlock()

	case "/topic":
		if len(parts) < 2 {
			// Show current topic
			c.mu.Lock()
			if c.currentTopic == "" {
				c.sysf("no topic set · use /topic \"your topic\"")
			} else {
				c.sysf("topic: %s", c.currentTopic)
			}
			c.mu.Unlock()
			return
		}
		if strings.ToLower(parts[1]) == "clear" {
			c.mu.Lock()
			c.currentTopic = ""
			c.sysf("topic cleared")
			c.mu.Unlock()
			c.mgr.Send(network.Envelope{Type: network.MsgTopic, Body: ""})
			return
		}
		q1 := strings.Index(line, `"`)
		q2 := strings.LastIndex(line, `"`)
		if q1 == -1 || q1 == q2 {
			c.mu.Lock()
			c.sysf(`usage: /topic "your topic"  or  /topic clear`)
			c.mu.Unlock()
			return
		}
		topic := line[q1+1 : q2]
		c.mu.Lock()
		c.currentTopic = topic
		c.sysf("topic set: %s", topic)
		c.mu.Unlock()
		c.mgr.Send(network.Envelope{Type: network.MsgTopic, Body: topic})

	case "/verify":
		type verifier interface {
			DMKeyFingerprint(peer string) string
		}
		v, ok := c.mgr.(verifier)
		if !ok {
			c.mu.Lock()
			c.sysf("/verify is only available in online mode")
			c.mu.Unlock()
			return
		}
		if len(parts) < 2 {
			c.mu.Lock()
			c.sysf("usage: /verify <name>  — compare fingerprint out-of-band to detect MITM")
			c.mu.Unlock()
			return
		}
		peer := parts[1]
		fp := v.DMKeyFingerprint(peer)
		c.mu.Lock()
		if fp == "" {
			c.sysf("no DM key for %s yet — open a DM session first: /dm %s hi", peer, peer)
		} else {
			fmt.Printf("%s  key fingerprint with %s:\r\n", green, peer)
			fmt.Printf("%s  %s%s\r\n", brightGreen, fp, reset)
			fmt.Printf("%s  ask %s to run /verify %s and compare — if they match, no MITM%s\r\n", dim+green, peer, c.name, reset)
		}
		c.mu.Unlock()

	case "/nuke":
		c.restore()
		c.mgr.Shutdown()
		fmt.Print(clearScreen)
		os.Exit(0)

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

func (c *Chat) printMsg(name, body string, isSelf, mentioned bool) {
	ts := time.Now().Format("15:04:05")

	baseColor := green
	if isSelf || mentioned {
		baseColor = brightGreen
	}

	// "HH:MM:SS [NAME]  " — prefix width for indent alignment
	prefixLen := 10 + 3 + utf8.RuneCountInString(name) + 2
	indent := strings.Repeat(" ", prefixLen)

	bodyWidth := 0
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > prefixLen+10 {
		bodyWidth = w - prefixLen
	}

	lines := wrapText(body, bodyWidth)
	for i, line := range lines {
		hl := highlightCode(line, baseColor)
		if i == 0 {
			switch {
			case isSelf:
				fmt.Printf("%s%s [%s]  %s%s\r\n", brightGreen, ts, name, hl, reset)
			case mentioned:
				fmt.Printf("%s%s [%s]  ◆ %s%s\r\n", brightGreen, ts, name, hl, reset)
				fmt.Print("\a")
			default:
				fmt.Printf("%s%s [%s]  %s%s\r\n", green, ts, name, hl, reset)
			}
		} else {
			fmt.Printf("%s%s%s%s\r\n", baseColor, indent, hl, reset)
		}
	}
}

// sysf prints a [sys] line. Must be called with c.mu held (or before goroutines start).
func (c *Chat) sysf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Printf("%s[sys] %s%s\r\n", dim+green, msg, reset)
}

func (c *Chat) promptLabel() string {
	label := c.name
	if c.dmTarget != "" {
		label = c.name + " → " + c.dmTarget
	}
	if c.awayMsg != "" {
		label += " · away"
	}
	return label
}

func (c *Chat) printPrompt() {
	ts := time.Now().Format("15:04:05")
	before := string(c.inputBuf[:c.cursorPos])
	after := string(c.inputBuf[c.cursorPos:])
	color := green
	if c.dmTarget != "" {
		color = brightGreen
	}
	fmt.Printf("%s%s [%s]  %s█%s%s", color, ts, c.promptLabel(), before, after, reset)
}

func (c *Chat) redrawInput() {
	ts := time.Now().Format("15:04:05")
	before := string(c.inputBuf[:c.cursorPos])
	after := string(c.inputBuf[c.cursorPos:])
	color := green
	if c.dmTarget != "" {
		color = brightGreen
	}
	fmt.Printf("%s%s%s [%s]  %s█%s%s", clearLine, color, ts, c.promptLabel(), before, after, reset)
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

// appendDMHistory records a DM message for a peer. Must be called with c.mu held.
func (c *Chat) appendDMHistory(peer, from, body string) {
	msgs := c.dmHistory[peer]
	msgs = append(msgs, dmMessage{from: from, body: body, ts: time.Now()})
	if len(msgs) > dmHistoryMax {
		msgs = msgs[len(msgs)-dmHistoryMax:]
	}
	c.dmHistory[peer] = msgs
}

// showDMHistory prints the last N messages for a peer. Must be called with c.mu held.
func (c *Chat) showDMHistory(peer string) {
	msgs := c.dmHistory[peer]
	if len(msgs) == 0 {
		return
	}
	fmt.Printf("%s  ── last %d message(s) with %s ──%s\r\n", dim+green, len(msgs), peer, reset)
	for _, m := range msgs {
		ts := m.ts.Format("15:04:05")
		if m.from == c.name {
			fmt.Printf("%s  %s [%s → %s]  %s%s\r\n", dim+green, ts, m.from, peer, m.body, reset)
		} else {
			fmt.Printf("%s  %s [%s]  %s%s\r\n", dim+green, ts, m.from, m.body, reset)
		}
	}
	fmt.Printf("%s  ──────────────────────────────────%s\r\n", dim+green, reset)
}

// showCommandHint prints usage for the command currently being typed.
// Press ? with an empty buffer → show all commands.
// Press ? while typing /something → show matching commands.
// Must be called with c.mu held.
func (c *Chat) showCommandHint(buf string) {
	fmt.Print(clearLine)

	// Empty buffer — show everything as a quick reference
	if strings.TrimSpace(buf) == "" {
		fmt.Printf("%s  commands:%s\r\n", dim+green, reset)
		for _, cmd := range allCommands {
			if hint, ok := commandHints[cmd]; ok {
				fmt.Printf("%s  %-10s  %s%s\r\n", dim+green, cmd, hint[len(cmd):], reset)
			}
		}
		return
	}

	fields := strings.Fields(buf)
	cmdTyped := strings.ToLower(fields[0])

	// Exact command match — show its usage
	if hint, ok := commandHints[cmdTyped]; ok {
		fmt.Printf("%s  %s%s\r\n", dim+green, hint, reset)
		return
	}

	// Prefix match — show all candidates
	var printed int
	for _, cmd := range allCommands {
		if strings.HasPrefix(cmd, cmdTyped) {
			if hint, ok := commandHints[cmd]; ok {
				fmt.Printf("%s  %s%s\r\n", dim+green, hint, reset)
				printed++
			}
		}
	}
	if printed == 0 {
		fmt.Printf("%s  no matching commands · /help for full list%s\r\n", dim+green, reset)
	}
}

// lockRoomName returns the value the user must type to unlock the session.
// Online mode: the room name. LAN mode: the local display name.
func (c *Chat) lockRoomName() string {
	type roomer interface{ RoomName() string }
	if r, ok := c.mgr.(roomer); ok {
		return r.RoomName()
	}
	return c.name
}

// idleLockLoop locks the session after 5 minutes of no keyboard activity.
// Runs as a background goroutine for the lifetime of the session.
func (c *Chat) idleLockLoop() {
	const idleTimeout = 5 * time.Minute
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		c.mu.Lock()
		idle := time.Since(c.lastInput)
		alreadyLocked := c.locked
		c.mu.Unlock()

		if !alreadyLocked && idle >= idleTimeout {
			c.doLock()
		}
	}
}

// doLock clears the screen and enters the locked state.
// Must NOT be called with c.mu held (it acquires the lock itself).
func (c *Chat) doLock() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.locked = true
	c.lockBuf = c.lockBuf[:0]
	c.clearInput()
	fmt.Print(clearScreen)
	fmt.Printf("%s[sys] session locked · idle for 5 minutes%s\r\n", green, reset)
	fmt.Printf("%s[sys] type the room name to unlock · press Enter%s\r\n\r\n", dim+green, reset)
	fmt.Printf("%s  unlock: %s", green, reset)
}

// printHelp prints the full categorised help. Must be called with c.mu held.
func (c *Chat) printHelp() {
	g, d, r := green, dim+green, reset
	fmt.Printf("%s\r\n", g)
	fmt.Printf("  ── Messaging ───────────────────────────────────%s\r\n", r)
	fmt.Printf("%s  /me <action>              %s* YOU action  (e.g. /me waves)%s\r\n", g, d, r)
	fmt.Printf("%s  /nick <name>              %schange your display name%s\r\n", g, d, r)
	fmt.Printf("%s\r\n", g)
	fmt.Printf("  ── Private ─────────────────────────────────────%s\r\n", r)
	fmt.Printf("%s  /dm <name>                %sopen a DM session (type normally to chat)%s\r\n", g, d, r)
	fmt.Printf("%s  /dm <name> <msg>          %ssend a one-off DM%s\r\n", g, d, r)
	fmt.Printf("%s  /back                     %sreturn to group chat from a DM session%s\r\n", g, d, r)
	fmt.Printf("%s  /onetime <name> \"msg\"     %sburn-after-reading · held until they connect%s\r\n", g, d, r)
	fmt.Printf("%s  /reveal [name]            %sshow a pending onetime privately · clears in 15s%s\r\n", g, d, r)
	fmt.Printf("%s  /burn <secs> \"msg\"        %sself-destructs for everyone after N seconds%s\r\n", g, d, r)
	fmt.Printf("%s  /away \"msg\"               %sauto-reply to DMs · prompt shows [away]%s\r\n", g, d, r)
	fmt.Printf("%s  /away                     %sclear away status%s\r\n", g, d, r)
	fmt.Printf("%s\r\n", g)
	fmt.Printf("  ── Room ────────────────────────────────────────%s\r\n", r)
	fmt.Printf("%s  /peers                    %slist who is online%s\r\n", g, d, r)
	fmt.Printf("%s  /room <name>              %sswitch rooms mid-session (online only)%s\r\n", g, d, r)
	fmt.Printf("%s  /invite                   %sprint the join command for this room%s\r\n", g, d, r)
	fmt.Printf("%s  /connect <ip>             %smanual IP connect (LAN mode only)%s\r\n", g, d, r)
	fmt.Printf("%s\r\n", g)
	fmt.Printf("  ── Navigation ──────────────────────────────────%s\r\n", r)
	fmt.Printf("%s  ↑ / ↓                     %sscroll through message history%s\r\n", g, d, r)
	fmt.Printf("%s  Tab                       %sautocomplete commands%s\r\n", g, d, r)
	fmt.Printf("%s  ?                         %susage hint for current command (while typing /)%s\r\n", g, d, r)
	fmt.Printf("%s\r\n", g)
	fmt.Printf("  ── Utility ─────────────────────────────────────%s\r\n", r)
	fmt.Printf("%s  /clear                    %sclear the screen%s\r\n", g, d, r)
	fmt.Printf("%s  /nuke                     %sdisconnect and vanish immediately%s\r\n", g, d, r)
	fmt.Printf("%s  /quit                     %sexit gracefully%s\r\n", g, d, r)
	fmt.Printf("%s\r\n", g)
	fmt.Printf("  %stip: /help <command> for details    e.g. /help onetime%s\r\n", d, r)
	fmt.Printf("\r\n")
}

// printCommandHelp prints detailed help for a single command. Must be called with c.mu held.
func (c *Chat) printCommandHelp(cmd string) {
	g, d, r := green, dim+green, reset
	cmd = strings.TrimPrefix(strings.ToLower(cmd), "/")

	type entry struct{ usage, desc, example string }
	help := map[string]entry{
		"dm": {
			"/dm <name>  or  /dm <name> <message>",
			"Open a private encrypted conversation.\n" +
				"  /dm JAY          — enters a DM session, prompt becomes [YOU → JAY]\n" +
				"  /dm JAY hey      — sends a one-off DM without entering session mode\n" +
				"  Use /back to return to group chat.",
			"/dm JAY\n  /dm JAY are you around?",
		},
		"onetime": {
			`/onetime <name> "message"`,
			"Burn-after-reading message.\n" +
				"  Delivered exactly once, then deleted from the relay forever.\n" +
				"  If the recipient is offline, the message waits (encrypted) until they connect.\n" +
				"  The relay never sees the plaintext.\n" +
				"  The message arrives masked — type /reveal when you're alone to read it.",
			`/onetime MARK "meet at the usual place"`,
		},
		"reveal": {
			"/reveal  or  /reveal <name>",
			"Show a pending onetime message privately.\n" +
				"  Incoming onetimes are masked on arrival — the content is never shown\n" +
				"  automatically, because someone may be watching your screen.\n" +
				"  /reveal clears the screen, shows the secret prominently, and wipes\n" +
				"  the display automatically after 15 seconds.\n" +
				"  If multiple onetimes are waiting, /reveal <name> picks a specific sender.",
			"/reveal\n  /reveal ALEX",
		},
		"burn": {
			`/burn <seconds> "message"`,
			"Self-destructing room message.\n" +
				"  Everyone in the room sees it with a countdown label.\n" +
				"  After N seconds a burn notice replaces it. Range: 1–300 seconds.",
			`/burn 30 "this message expires in 30 seconds"`,
		},
		"away": {
			`/away "message"  or  /away`,
			"Set an away status.\n" +
				"  Anyone who DMs you gets an automatic reply with your message.\n" +
				"  Your prompt shows [away] while active.\n" +
				"  /away with no argument clears the status.",
			`/away "on a call, back in 20"\n  /away`,
		},
		"room": {
			"/room <name>",
			"Switch rooms without restarting.\n" +
				"  Sends a leave to the current room, derives a fresh encryption key,\n" +
				"  and reconnects to the new room. Online mode only.\n" +
				"  Your DM session is cleared on switch.",
			"/room secretproject",
		},
		"invite": {
			"/invite",
			"Print the command someone else needs to join your current room.\n" +
				"  Copy and share it however you like — Signal, IRL, email.\n" +
				"  Online mode only.",
			"/invite  →  porfavor --room fridaynight",
		},
		"nuke": {
			"/nuke",
			"Panic exit.\n" +
				"  Closes the connection without sending a leave notice.\n" +
				"  Clears the screen and exits immediately. No local trace.\n" +
				"  Peers see the relay close the socket — no named departure is announced.",
			"/nuke",
		},
		"nick": {
			"/nick <newname>",
			"Change your display name.\n" +
				"  Broadcasts the change to everyone in the room.\n" +
				"  Name is automatically uppercased.",
			"/nick SHADOW",
		},
		"me": {
			"/me <action>",
			"Send an action message in third person.\n" +
				"  Shows as:  * NAME action\n" +
				"  Classic IRC-style emote.",
			"/me slaps MARK with a large trout",
		},
		"peers": {
			"/peers",
			"List everyone currently online in your room.",
			"/peers",
		},
		"back": {
			"/back",
			"Return to group chat from a DM session.\n" +
				"  Does not affect away status — use /away to clear that.",
			"/back",
		},
		"connect": {
			"/connect <ip>  or  /connect <ip:port>",
			"Manually connect to a peer by IP address.\n" +
				"  LAN mode only. Use when mDNS discovery is blocked.\n" +
				"  Default port is 47200.",
			"/connect 192.168.1.42",
		},
		"clear": {"/clear", "Clear the terminal screen.", "/clear"},
		"quit":  {"/quit", "Exit gracefully. Announces your departure to peers, then disconnects.", "/quit"},
	}

	e, ok := help[cmd]
	if !ok {
		c.sysf("no help for '%s' — try /help for the full list", cmd)
		return
	}
	fmt.Printf("%s\r\n", g)
	fmt.Printf("  %s%s%s\r\n", g, e.usage, r)
	fmt.Printf("\r\n")
	for _, line := range strings.Split(e.desc, "\n") {
		fmt.Printf("  %s%s%s\r\n", d, line, r)
	}
	fmt.Printf("\r\n")
	fmt.Printf("  %sexample:%s\r\n", d, r)
	for _, line := range strings.Split(e.example, "\n") {
		fmt.Printf("  %s  %s%s\r\n", g, line, r)
	}
	fmt.Printf("\r\n")
}
