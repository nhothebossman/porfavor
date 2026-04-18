package main

import (
	"bufio"
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
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-version") {
		fmt.Println("porfavor " + version)
		return
	}

	logo.PrintLogo()

	// Allow overriding name without touching saved config
	var name string
	for i, arg := range os.Args[1:] {
		if arg == "--name" || arg == "-name" {
			if i+2 < len(os.Args) {
				name = strings.ToUpper(os.Args[i+2])
			}
			break
		}
	}
	if name == "" {
		name = loadOrPromptName()
	} else {
		fmt.Printf("\033[32m  Running as %s.\033[0m\n\n", name)
	}

	mgr := network.NewManager(name)
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

	// First run — prompt for name
	fmt.Print("\033[32m  What's your name? → \033[0m")
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	name := strings.TrimSpace(input)

	if name == "" {
		name = "anon"
	}

	// Uppercase the name for display
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
