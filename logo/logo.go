package logo

import (
	"fmt"
	"math/rand"
	"time"
)

const (
	green = "\033[32m"
	reset = "\033[0m"
)

const style1 = `██████╗  ██████╗ ██████╗     ███████╗ █████╗ ██╗   ██╗ ██████╗ ██████╗
██╔══██╗██╔═══██╗██╔══██╗    ██╔════╝██╔══██╗██║   ██║██╔═══██╗██╔══██╗
██████╔╝██║   ██║██████╔╝    █████╗  ███████║██║   ██║██║   ██║██████╔╝
██╔═══╝ ██║   ██║██╔══██╗    ██╔══╝  ██╔══██║╚██╗ ██╔╝██║   ██║██╔══██╗
██║     ╚██████╔╝██║  ██║    ██║     ██║  ██║ ╚████╔╝ ╚██████╔╝██║  ██║
╚═╝      ╚═════╝ ╚═╝  ╚═╝    ╚═╝     ╚═╝  ╚═╝  ╚═══╝   ╚═════╝ ╚═╝  ╚═╝`

const style7 = ` ___  ___  ____     ____  __   _  _  ___  ____
(  ,\(  _)(  _ \   (  __)/  \ / )( \(   \(  _ \
 ) _/ ) _)  )   /   ) _)(  O )\ \/ / ) ) )) __/
(_)  (___)(__)\_)  (__)  \__/  \__/ (___/(__)  `

const footer = `════════════════════════════════════════════
 p2p · lan only · encrypted · no trace
 type /help for commands
════════════════════════════════════════════`

func PrintLogo() {
	logos := []string{style1, style7}
	chosen := logos[rand.Intn(len(logos))]

	fmt.Print(green)
	fmt.Println(chosen)
	time.Sleep(60 * time.Millisecond)
	fmt.Println(footer)
	fmt.Print(reset)
	fmt.Println()
}
