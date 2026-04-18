package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"porfavor/chat"
	"porfavor/logo"
	"porfavor/network"
)

// Injected at build time via -ldflags "-X main.version=vX.Y.Z"
var version = "dev"

func main() {
	var (
		lanMode   = flag.Bool("lan", false, "use LAN mode (mDNS) instead of online relay")
		serverURL = flag.String("server", network.DefaultRelayURL, "relay server WebSocket URL")
		roomName  = flag.String("room", "default", "room name / password (shared with peers)")
		nameFlag  = flag.String("name", "", "override saved name for this session")
		ver       = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *ver {
		fmt.Println("porfavor " + version)
		return
	}

	logo.PrintLogo()

	name := *nameFlag
	if name != "" {
		name = strings.ToUpper(name)
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
		mgr = network.NewOnlineManager(name, *serverURL, *roomName)
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

	name = strings.ToUpper(name)
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
