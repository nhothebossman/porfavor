package logo

import (
	"fmt"
	"time"
)

const (
	green = "\033[32m"
	reset = "\033[0m"
)

const logo = `██████╗  ██████╗ ██████╗     ███████╗ █████╗ ██╗   ██╗ ██████╗ ██████╗
██╔══██╗██╔═══██╗██╔══██╗    ██╔════╝██╔══██╗██║   ██║██╔═══██╗██╔══██╗
██████╔╝██║   ██║██████╔╝    █████╗  ███████║██║   ██║██║   ██║██████╔╝
██╔═══╝ ██║   ██║██╔══██╗    ██╔══╝  ██╔══██║╚██╗ ██╔╝██║   ██║██╔══██╗
██║     ╚██████╔╝██║  ██║    ██║     ██║  ██║ ╚████╔╝ ╚██████╔╝██║  ██║
╚═╝      ╚═════╝ ╚═╝  ╚═╝    ╚═╝     ╚═╝  ╚═╝  ╚═══╝   ╚═════╝ ╚═╝  ╚═╝`

const footer = `════════════════════════════════════════════
 encrypted · ephemeral · no accounts
 type /help for commands
════════════════════════════════════════════`

func PrintLogo() {
	fmt.Print(green)
	fmt.Println(logo)
	time.Sleep(60 * time.Millisecond)
	fmt.Println(footer)
	fmt.Print(reset)
	fmt.Println()
}
