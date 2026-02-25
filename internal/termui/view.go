package termui

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gdamore/tcell/v2"
	"github.com/klizhentas/goclaw/internal/listener"
	"github.com/rivo/tview"
)

type UI struct {
	app    *tview.Application
	layout *tview.Flex

	conversationList *tview.List
	messageView      *tview.TextView
	input            *tview.InputField
	status           *tview.TextView

	mu            sync.Mutex
	state         *conversationState
	currentOutCID string
	currentOutBuf strings.Builder
	rendering     atomic.Bool
}

func New(defaultConversation string) *UI {
	u := &UI{
		app:    tview.NewApplication(),
		state:  newConversationState(defaultConversation),
		status: tview.NewTextView(),
	}

	u.conversationList = tview.NewList().
		ShowSecondaryText(false)
	u.conversationList.SetBorder(true).SetTitle(" Conversations ")
	u.conversationList.SetChangedFunc(func(index int, _, _ string, _ rune) {
		if u.rendering.Load() {
			return
		}
		slog.Info("termui conversation changed", "stage", "egress", "index", index)
		u.mu.Lock()
		defer u.mu.Unlock()
		u.state.switchToIndex(index)
		u.renderLocked("switched conversation")
	})

	u.messageView = tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetWordWrap(true)
	u.messageView.SetBorder(true).SetTitle(" Messages ")

	u.input = tview.NewInputField()
	u.input.SetBorder(true).SetTitle(" Input ")

	u.status.SetBorder(true).SetTitle(" Status ")

	right := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(u.messageView, 0, 1, false).
		AddItem(u.input, 3, 0, true).
		AddItem(u.status, 3, 0, false)

	u.layout = tview.NewFlex().
		AddItem(u.conversationList, 28, 0, false).
		AddItem(right, 0, 1, true)

	u.input.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEnter:
			u.submitInput()
		case tcell.KeyEsc:
			u.input.SetText("")
			u.setStatus("input cleared")
		}
	})

	u.app.SetInputCapture(u.captureKeys)

	u.mu.Lock()
	u.renderLocked("ready")
	u.mu.Unlock()
	slog.Info("termui initialized", "stage", "egress", "default_conversation", u.state.activeConversation())

	return u
}

func (u *UI) Run(ctx context.Context, onMessage func(context.Context, listener.InboundMessage) error) error {
	slog.Info("termui run start", "stage", "ingress")
	u.input.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEnter:
			slog.Info("termui input enter", "stage", "ingress")
			u.submitInputWithCallback(ctx, onMessage)
		case tcell.KeyEsc:
			slog.Info("termui input esc", "stage", "ingress")
			u.input.SetText("")
			u.setStatus("input cleared")
		}
	})

	go func() {
		<-ctx.Done()
		slog.Info("termui context canceled", "stage", "ingress", "error", ctx.Err())
		u.app.Stop()
	}()

	if err := u.app.SetRoot(u.layout, true).SetFocus(u.input).Run(); err != nil {
		slog.Error("termui run error", "stage", "ingress", "error", err)
		return err
	}
	slog.Info("termui run stop", "stage", "ingress")
	return nil
}

func (u *UI) Start(conversationID string) {
	slog.Debug("termui outbound start", "stage", "egress", "conversation_id", conversationID)
	u.mu.Lock()
	defer u.mu.Unlock()
	u.currentOutCID = strings.TrimSpace(conversationID)
	u.currentOutBuf.Reset()
	if u.currentOutCID == "" {
		u.currentOutCID = u.state.activeConversation()
	}
	u.state.ensureConversation(u.currentOutCID)
}

func (u *UI) Token(token string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.currentOutBuf.WriteString(token)
}

func (u *UI) End() {
	u.mu.Lock()
	conversationID := u.currentOutCID
	content := u.currentOutBuf.String()
	u.currentOutCID = ""
	u.currentOutBuf.Reset()
	if strings.TrimSpace(content) != "" {
		u.state.appendAssistantMessage(conversationID, content)
	}
	u.mu.Unlock()
	slog.Info("termui outbound end", "stage", "egress", "conversation_id", conversationID, "content_len", len(content))

	u.app.QueueUpdateDraw(func() {
		u.mu.Lock()
		defer u.mu.Unlock()
		u.renderLocked("assistant response received")
	})
}

func (u *UI) captureKeys(event *tcell.EventKey) *tcell.EventKey {
	slog.Debug("termui key", "stage", "ingress", "key", event.Key(), "rune", event.Rune(), "mods", event.Modifiers())
	if event.Key() == tcell.KeyCtrlC {
		slog.Info("termui ctrl+c", "stage", "ingress")
		u.app.Stop()
		return nil
	}

	if event.Modifiers()&tcell.ModAlt != 0 {
		if idx, ok := indexFromRune(event.Rune()); ok {
			u.mu.Lock()
			defer u.mu.Unlock()
			if u.state.switchToIndex(idx) {
				slog.Info("termui switch alt-number", "stage", "ingress", "index", idx)
				u.renderLocked("switched conversation")
			}
			return nil
		}
	}

	switch event.Key() {
	case tcell.KeyCtrlN, tcell.KeyTab:
		u.mu.Lock()
		defer u.mu.Unlock()
		u.state.cycleNext()
		slog.Info("termui cycle next", "stage", "ingress", "active", u.state.activeConversation())
		u.renderLocked("switched conversation")
		return nil
	case tcell.KeyCtrlP, tcell.KeyBacktab:
		u.mu.Lock()
		defer u.mu.Unlock()
		u.state.cyclePrev()
		slog.Info("termui cycle prev", "stage", "ingress", "active", u.state.activeConversation())
		u.renderLocked("switched conversation")
		return nil
	default:
		return event
	}
}

func (u *UI) submitInput() {
	u.submitInputWithCallback(context.Background(), func(_ context.Context, _ listener.InboundMessage) error { return nil })
}

func (u *UI) submitInputWithCallback(ctx context.Context, onMessage func(context.Context, listener.InboundMessage) error) {
	raw := strings.TrimSpace(u.input.GetText())
	if raw == "" {
		slog.Debug("termui submit ignored empty", "stage", "ingress")
		return
	}

	u.mu.Lock()
	active := u.state.activeConversation()
	conversationID, content, ok := parseInputLine(active, raw)
	if !ok {
		u.mu.Unlock()
		slog.Warn("termui submit invalid input", "stage", "ingress", "raw", raw)
		u.setStatus("invalid input")
		return
	}
	idx := u.state.ensureConversation(conversationID)
	u.state.switchToIndex(idx)
	u.state.appendUserMessage(conversationID, content)
	u.input.SetText("")
	u.renderLocked("sending")
	u.mu.Unlock()
	slog.Info("termui submit", "stage", "ingress", "conversation_id", conversationID, "content_len", len(content))

	go func() {
		if err := onMessage(ctx, listener.InboundMessage{
			ConversationID: conversationID,
			Content:        content,
		}); err != nil {
			slog.Error("termui submit failed", "stage", "persist_in", "conversation_id", conversationID, "error", err)
			u.setStatus("send failed: " + err.Error())
			return
		}
		slog.Info("termui submit queued", "stage", "persist_in", "conversation_id", conversationID)
		u.setStatus("sent")
	}()
}

func (u *UI) setStatus(message string) {
	u.app.QueueUpdateDraw(func() {
		u.mu.Lock()
		defer u.mu.Unlock()
		u.renderLocked(message)
	})
}

func (u *UI) renderLocked(statusText string) {
	u.rendering.Store(true)
	defer u.rendering.Store(false)

	u.conversationList.Clear()
	for i, id := range u.state.conversations {
		label := id
		if n := u.state.unread[id]; n > 0 {
			label += " (" + strconv.Itoa(n) + ")"
		}
		shortcut := rune(0)
		if i < 9 {
			shortcut = rune('1' + i)
		}
		u.conversationList.AddItem(label, "", shortcut, nil)
	}
	if len(u.state.conversations) > 0 {
		u.conversationList.SetCurrentItem(u.state.activeIndex)
	}

	active := u.state.activeConversation()
	u.messageView.SetTitle(" Messages [" + active + "] ")
	u.messageView.SetText(strings.Join(u.state.messages[active], "\n"))
	u.messageView.ScrollToEnd()

	u.status.SetText(
		"Active: " + active +
			" | Keys: Alt+1..9 switch, Ctrl+n/p cycle, Tab/Shift+Tab cycle, Enter send, Esc clear, Ctrl+c quit" +
			" | " + statusText,
	)
}
