package main

import (
	"os"

	"golang.org/x/sys/windows"
)

func init() {
	// Enable ANSI escape code processing on Windows stdout/stderr
	for _, f := range []*os.File{os.Stdout, os.Stderr} {
		handle := windows.Handle(f.Fd())
		var mode uint32
		if err := windows.GetConsoleMode(handle, &mode); err == nil {
			_ = windows.SetConsoleMode(handle, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
		}
	}
}
