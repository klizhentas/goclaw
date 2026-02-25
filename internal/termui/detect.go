package termui

import (
	"os"
	"strings"

	xterm "golang.org/x/term"
)

type SupportInfo struct {
	Supported bool
	Term      string
	StdinTTY  bool
	StdoutTTY bool
	Reason    string
}

func DetectSupport() SupportInfo {
	term := strings.ToLower(strings.TrimSpace(os.Getenv("TERM")))
	stdinTTY := xterm.IsTerminal(int(os.Stdin.Fd()))
	stdoutTTY := xterm.IsTerminal(int(os.Stdout.Fd()))

	info := SupportInfo{
		Supported: false,
		Term:      term,
		StdinTTY:  stdinTTY,
		StdoutTTY: stdoutTTY,
	}

	if term == "" {
		info.Reason = "TERM is empty"
		return info
	}
	if term == "dumb" {
		info.Reason = "TERM=dumb"
		return info
	}
	if !stdinTTY {
		info.Reason = "stdin is not a TTY"
		return info
	}
	if !stdoutTTY {
		info.Reason = "stdout is not a TTY"
		return info
	}

	info.Supported = true
	info.Reason = "ok"
	return info
}

func Supported() bool {
	return DetectSupport().Supported
}
