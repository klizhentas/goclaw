package listener

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
)

type InboundMessage struct {
	ConversationID string
	Content        string
}

type CLIListener struct {
	in  io.Reader
	out io.Writer
}

func NewCLIListener(in io.Reader, out io.Writer) *CLIListener {
	return &CLIListener{in: in, out: out}
}

func (l *CLIListener) Run(ctx context.Context, onMessage func(context.Context, InboundMessage) error) error {
	scanner := bufio.NewScanner(l.in)
	fmt.Fprintln(l.out, "enter messages as <conversation_id>: <text>; type 'exit' to quit")

	lineCh := make(chan string)
	errCh := make(chan error, 1)
	go func() {
		defer close(lineCh)
		for scanner.Scan() {
			lineCh <- scanner.Text()
		}
		if err := scanner.Err(); err != nil {
			errCh <- err
		}
		close(errCh)
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case line, ok := <-lineCh:
			if !ok {
				if err, hasErr := <-errCh; hasErr && err != nil {
					return err
				}
				return nil
			}
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if strings.EqualFold(line, "exit") {
				return nil
			}
			conversationID, content, ok := strings.Cut(line, ":")
			if !ok || strings.TrimSpace(conversationID) == "" || strings.TrimSpace(content) == "" {
				fmt.Fprintln(l.out, "invalid input; expected <conversation_id>: <text>")
				continue
			}
			if err := onMessage(ctx, InboundMessage{ConversationID: strings.TrimSpace(conversationID), Content: strings.TrimSpace(content)}); err != nil {
				return err
			}
		}
	}
}
