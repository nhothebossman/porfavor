package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"porfavor/chat"
	"porfavor/logo"
	"porfavor/network"
)

// Injected at build time via -ldflags "-X main.version=vX.Y.Z"
var version = "dev"

func main() {
	var (
		lanMode     = flag.Bool("lan", false, "use LAN mode (mDNS) instead of online relay")
		serverURL   = flag.String("server", network.DefaultRelayURL, "relay server WebSocket URL")
		roomName    = flag.String("room", "default", "room name / password (shared with peers)")
		nameFlag    = flag.String("name", "", "override saved name for this session")
		ver         = flag.Bool("version", false, "print version and exit")
		sendMode    = flag.Bool("send", false, "read from stdin and send as a message, then exit")
		expiresFlag = flag.String("expires", "", "room lifetime, e.g. 30m, 2h, 1h30m — room is deleted after this duration (online only)")
	)
	flag.Parse()

	// Parse --expires into a unix timestamp.
	var expiresAt int64
	if *expiresFlag != "" {
		if *lanMode {
			fmt.Fprintln(os.Stderr, "error: --expires is only available in online mode (remove --lan)")
			os.Exit(1)
		}
		dur, err := time.ParseDuration(*expiresFlag)
		if err != nil || dur <= 0 {
			fmt.Fprintf(os.Stderr, "error: invalid --expires value %q — use e.g. 30m, 2h, 1h30m\n", *expiresFlag)
			os.Exit(1)
		}
		expiresAt = time.Now().Add(dur).Unix()
	}

	if *ver {
		fmt.Println("porfavor " + version)
		return
	}

	// Pipe mode: read stdin → send → exit. No logo, no interactive UI.
	if *sendMode {
		scanner := bufio.NewScanner(os.Stdin)
		var lines []string
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		msg := strings.TrimSpace(strings.Join(lines, "\n"))
		if msg == "" {
			return
		}
		name := *nameFlag
		if name == "" {
			name = loadNameSilent()
		}
		mgr := network.NewOnlineManager(name, *serverURL, *roomName, 0) // pipe mode: no expiry
		mgr.Start()
		time.Sleep(700 * time.Millisecond) // wait for relay connection
		mgr.Send(network.Envelope{Type: network.MsgChat, Body: msg})
		time.Sleep(250 * time.Millisecond) // wait for delivery
		mgr.Shutdown()
		return
	}

	logo.PrintLogo()

	name := *nameFlag
	if name != "" {
		fmt.Printf("\033[32m  Running as %s.\033[0m\n\n", name)
	} else {
		name = loadOrPromptName()
	}

	var mgr network.Backend
	if *lanMode {
		fmt.Printf("\033[2m\033[32m  [mode] LAN · mDNS discovery\033[0m\n\n")
		mgr = network.NewManager(name)
	} else {
		fmt.Printf("\033[2m\033[32m  [mode] online · %s\033[0m\n\n", *serverURL)
		mgr = network.NewOnlineManager(name, *serverURL, *roomName, expiresAt)
	}

	mgr.Start()
	chat.New(mgr, name).Run()
}

func loadOrPromptName() string {
	configPath := configFile()

	data, err := os.ReadFile(configPath)
	if err == nil {
		name := strings.TrimSpace(string(data))
		if name != "" {
			fmt.Printf("\033[32m  Welcome back, %s.\033[0m\n\n", name)
			return name
		}
	}

	fmt.Print("\033[32m  What's your name? → \033[0m")
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	name := strings.TrimSpace(input)

	if name == "" {
		name = "anon"
	}

	saveName(configPath, name)
	fmt.Println()
	return name
}

func configFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	dir := filepath.Join(home, ".porfavor")
	_ = os.MkdirAll(dir, 0700)
	return filepath.Join(dir, "config")
}

func saveName(path, name string) {
	_ = os.WriteFile(path, []byte(name), 0600)
}

// loadNameSilent reads the saved name without prompting.
// Falls back to hostname, then "pipe".
func loadNameSilent() string {
	data, err := os.ReadFile(configFile())
	if err == nil {
		if name := strings.TrimSpace(string(data)); name != "" {
			return name
		}
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "pipe"
}
