package outbound

import (
	"fmt"
	"io"
	"sync"
)

type CLIOutbound struct {
	w  io.Writer
	mu sync.Mutex
}

func NewCLIOutbound(w io.Writer) *CLIOutbound {
	return &CLIOutbound{w: w}
}

func (o *CLIOutbound) Start(conversationID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	fmt.Fprintf(o.w, "[%s] assistant: ", conversationID)
}

func (o *CLIOutbound) Token(token string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	fmt.Fprint(o.w, token)
}

func (o *CLIOutbound) End() {
	o.mu.Lock()
	defer o.mu.Unlock()
	fmt.Fprintln(o.w)
}
