package termui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/klizhentas/goclaw/internal/listener"
	"github.com/rivo/tview"
)

const CommandRemoveConversation = "remove_conversation"
const CommandTasksList = "tasks_list"
const CommandTasksRemove = "tasks_remove"
const CommandTasksRemoveAll = "tasks_remove_all"

type CommandAction struct {
	Kind           string
	ConversationID string
	TaskID         string
	All            bool
}

type UI struct {
	app    *tview.Application
	layout *tview.Flex

	conversationList *tview.List
	mainPages        *tview.Pages
	messageView      *tview.TextView
	debugTable       *tview.Table
	input            *tview.InputField
	status           *tview.TextView

	mu            sync.Mutex
	state         *conversationState
	currentOutCID string
	currentOutBuf strings.Builder
	rendering     atomic.Bool
	debugEnabled  bool
	debugTabIndex int
	debugSnapshot *DebugSnapshot
	lastKey       string
	statusText    string
	flashIndex    int
	flashUntil    time.Time
	debugProvider func(context.Context, string) (*DebugSnapshot, error)
}

func New(defaultConversation, userLabel, assistantLabel string, debugProvider func(context.Context, string) (*DebugSnapshot, error)) *UI {
	u := &UI{
		app:           tview.NewApplication(),
		state:         newConversationState(defaultConversation, userLabel, assistantLabel),
		status:        tview.NewTextView(),
		flashIndex:    -1,
		debugTabIndex: 0,
		debugProvider: debugProvider,
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
		if u.debugEnabled {
			u.setDebugTabLocked(index)
			u.renderLocked("debug section: " + debugSectionName(u.debugTabIndex))
			return
		}
		u.state.switchToIndex(index)
		u.renderLocked("switched conversation")
	})

	u.messageView = tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetWordWrap(true)
	u.messageView.SetBorder(true).SetTitle(" Messages ")

	u.debugTable = tview.NewTable().
		SetBorders(false).
		SetSelectable(false, false)
	u.debugTable.SetBorder(true).SetTitle(" Debug ")

	u.mainPages = tview.NewPages().
		AddPage("messages", u.messageView, true, true).
		AddPage("debug", u.debugTable, true, false)

	u.input = tview.NewInputField()
	u.input.SetBorder(true).SetTitle(" Input (message or /new [conversation_id]) ")

	u.status.SetBorder(true).SetTitle(" Status / Debug ")

	right := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(u.mainPages, 0, 1, false).
		AddItem(u.input, 3, 0, true).
		AddItem(u.status, 8, 0, false)

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

func (u *UI) Run(
	ctx context.Context,
	onMessage func(context.Context, listener.InboundMessage) error,
	onCommand func(context.Context, CommandAction) (string, error),
) error {
	slog.Info("termui run start", "stage", "ingress")
	u.input.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEnter:
			slog.Info("termui input enter", "stage", "ingress")
			u.submitInputWithCallback(ctx, onMessage, onCommand)
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

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				u.mu.Lock()
				enabled := u.debugEnabled
				u.mu.Unlock()
				if enabled {
					u.refreshDebug(ctx)
				}
			}
		}
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
	u.setLastKey(keyString(event))
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
			prev := u.state.activeIndex
			if u.state.switchToIndex(idx) {
				u.triggerFlashLocked(idx)
				if prev == idx {
					u.renderLocked(fmt.Sprintf("pressed Alt-%d (already active)", idx+1))
				} else {
					slog.Info("termui switch alt-number", "stage", "ingress", "index", idx)
					u.renderLocked(fmt.Sprintf("pressed Alt-%d", idx+1))
				}
			} else {
				u.renderLocked(fmt.Sprintf("pressed Alt-%d (no conversation)", idx+1))
			}
			return nil
		}
	}

	if event.Key() == tcell.KeyCtrlG {
		u.mu.Lock()
		u.debugEnabled = !u.debugEnabled
		enabled := u.debugEnabled
		u.renderLocked(fmt.Sprintf("debug %s", onOff(enabled)))
		u.mu.Unlock()
		if enabled {
			go u.refreshDebug(context.Background())
		}
		return nil
	}

	switch event.Key() {
	case tcell.KeyCtrlN, tcell.KeyTab:
		u.mu.Lock()
		defer u.mu.Unlock()
		if u.debugEnabled {
			u.cycleDebugTabLocked(1)
			u.renderLocked("debug section: " + debugSectionName(u.debugTabIndex))
			return nil
		}
		u.state.cycleNext()
		slog.Info("termui cycle next", "stage", "ingress", "active", u.state.activeConversation())
		u.renderLocked("switched conversation")
		return nil
	case tcell.KeyCtrlP, tcell.KeyBacktab:
		u.mu.Lock()
		defer u.mu.Unlock()
		if u.debugEnabled {
			u.cycleDebugTabLocked(-1)
			u.renderLocked("debug section: " + debugSectionName(u.debugTabIndex))
			return nil
		}
		u.state.cyclePrev()
		slog.Info("termui cycle prev", "stage", "ingress", "active", u.state.activeConversation())
		u.renderLocked("switched conversation")
		return nil
	default:
		return event
	}
}

func (u *UI) refreshDebug(parent context.Context) {
	if u.debugProvider == nil {
		return
	}

	u.mu.Lock()
	active := u.state.activeConversation()
	u.mu.Unlock()

	ctx, cancel := context.WithTimeout(parent, 1500*time.Millisecond)
	defer cancel()
	snapshot, err := u.debugProvider(ctx, active)
	if err != nil {
		snapshot = &DebugSnapshot{
			GeneratedAt:       time.Now().UTC(),
			ScopeConversation: active,
		}
		slog.Error("termui debug refresh failed", "stage", "egress", "error", err)
	}

	u.app.QueueUpdateDraw(func() {
		u.mu.Lock()
		defer u.mu.Unlock()
		if !u.debugEnabled {
			return
		}
		u.debugSnapshot = snapshot
		u.renderLocked("debug updated")
	})
}

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

func (u *UI) submitInput() {
	u.submitInputWithCallback(
		context.Background(),
		func(_ context.Context, _ listener.InboundMessage) error { return nil },
		func(_ context.Context, _ CommandAction) (string, error) {
			return "", errors.New("command handling unavailable")
		},
	)
}

func (u *UI) submitInputWithCallback(
	ctx context.Context,
	onMessage func(context.Context, listener.InboundMessage) error,
	onCommand func(context.Context, CommandAction) (string, error),
) {
	raw := strings.TrimSpace(u.input.GetText())
	if raw == "" {
		slog.Debug("termui submit ignored empty", "stage", "ingress")
		return
	}
	if u.debugEnabled {
		u.setStatus("debug mode active; press Ctrl+g to return to conversations")
		return
	}

	cmd := parseCommand(raw)
	if cmd.kind != commandNone {
		u.handleCommand(ctx, cmd, onCommand)
		return
	}

	u.mu.Lock()
	conversationID := u.state.activeConversation()
	content := raw
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
	u.statusText = statusText

	active := u.state.activeConversation()
	if u.debugEnabled {
		u.renderDebugSidebarLocked()
		u.mainPages.SwitchToPage("debug")
		u.renderDebugTableLocked()
	} else {
		u.conversationList.SetTitle(" Conversations ")
		u.conversationList.Clear()
		for i, id := range u.state.conversations {
			label := id
			if i == u.state.activeIndex {
				label = "▶ " + label
			}
			if i == u.flashIndex && time.Now().Before(u.flashUntil) {
				label = "[black:yellow:b]" + label + "[-:-:-]"
			}
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

		u.mainPages.SwitchToPage("messages")
		u.messageView.SetTitle(" Messages [" + active + "] ")
		u.messageView.SetText(strings.Join(u.state.messages[active], "\n"))
		u.messageView.ScrollToEnd()
	}

	u.status.SetText(
		"Active: " + active +
			" | New: /new [id] | Switch: /switch <id> | Rename: /rename <id> | Remove: /remove [id] | Tasks: /tasks ..." +
			" | Keys: Alt+1..9 switch, Ctrl+n/p cycle, Tab/Shift+Tab cycle, Ctrl+g debug, Enter send, Esc clear, Ctrl+c quit" +
			" | Last key: " + defaultIfEmpty(u.lastKey, "-") +
			" | " + statusText +
			u.debugSuffixLocked(),
	)
}

func (u *UI) renderDebugSidebarLocked() {
	u.conversationList.SetTitle(" Debug ")
	u.conversationList.Clear()
	for i := 0; i < debugSectionCount; i++ {
		u.conversationList.AddItem(debugSectionName(i), "", 0, nil)
	}
	u.conversationList.SetCurrentItem(clampDebugTab(u.debugTabIndex))
}

func (u *UI) renderDebugTableLocked() {
	u.debugTable.Clear()
	s := u.debugSnapshot
	if s == nil {
		u.debugTable.SetCell(0, 0, tview.NewTableCell("Loading debug snapshot..."))
		return
	}

	row := 0
	write := func(k, v string, valueColor tcell.Color) {
		u.debugTable.SetCell(row, 0, tview.NewTableCell(k).SetAttributes(tcell.AttrBold))
		cell := tview.NewTableCell(v)
		if valueColor != tcell.ColorDefault {
			cell.SetTextColor(valueColor)
		}
		u.debugTable.SetCell(row, 1, cell)
		row++
	}

	switch clampDebugTab(u.debugTabIndex) {
	case debugTabSummary:
		write("Generated", s.GeneratedAt.Format(time.RFC3339), tcell.ColorDefault)
		write("Scope", defaultIfEmpty(strings.TrimSpace(s.ScopeConversation), "all"), tcell.ColorDefault)
		write("Tasks", fmt.Sprintf("total=%d due=%d claimable=%d leased=%d", s.TasksTotal, s.TasksDue, s.TasksClaimable, s.TasksLeased), tcell.ColorDefault)
		write("Runs", fmt.Sprintf("queued=%d running=%d", s.TaskRunsQueued, s.TaskRunsRunning), tcell.ColorDefault)
		workerColor := tcell.ColorDefault
		if len(s.Workers) == 0 {
			workerColor = tcell.ColorYellow
		}
		write("Workers", defaultIfEmpty(strings.Join(s.Workers, ","), "none"), workerColor)
		if len(s.Workers) == 1 && strings.TrimSpace(s.Workers[0]) == "worker-1" {
			write("Warning", "only default worker-1 observed; set unique WORKER_ID per worker process", tcell.ColorYellow)
		}
		if len(u.state.conversations) > len(s.Workers) {
			write(
				"Warning",
				fmt.Sprintf("conversations=%d exceed workers=%d", len(u.state.conversations), len(s.Workers)),
				tcell.ColorYellow,
			)
		}
		write("Schedulers", defaultIfEmpty(strings.Join(s.Schedulers, ","), "none"), tcell.ColorDefault)
	case debugTabQueue:
		write("Generated", s.GeneratedAt.Format(time.RFC3339), tcell.ColorDefault)
		write("Queue(in)", fmt.Sprintf("pending=%d processing=%d error=%d", s.InboundPending, s.InboundProcessing, s.InboundError), tcell.ColorDefault)
		write("Queue(out)", fmt.Sprintf("pending=%d processing=%d error=%d", s.OutboundPending, s.OutboundProcessing, s.OutboundError), tcell.ColorDefault)
	case debugTabWorkers:
		write("Generated", s.GeneratedAt.Format(time.RFC3339), tcell.ColorDefault)
		workerColor := tcell.ColorDefault
		workerValue := defaultIfEmpty(strings.Join(s.Workers, ","), "none")
		if len(s.Workers) == 0 {
			workerColor = tcell.ColorYellow
			write("Warning", "no workers observed in recent window", tcell.ColorYellow)
		}
		write("Workers", workerValue, workerColor)
		if len(s.Workers) == 1 && strings.TrimSpace(s.Workers[0]) == "worker-1" {
			write("Warning", "only default worker-1 observed; set unique WORKER_ID per worker process", tcell.ColorYellow)
		}
		if len(u.state.conversations) > len(s.Workers) {
			write(
				"Warning",
				fmt.Sprintf("conversations=%d exceed workers=%d", len(u.state.conversations), len(s.Workers)),
				tcell.ColorYellow,
			)
		}
		write("Schedulers", defaultIfEmpty(strings.Join(s.Schedulers, ","), "none"), tcell.ColorDefault)
	case debugTabRuns:
		u.debugTable.SetCell(row, 0, tview.NewTableCell("Recent Task Runs").SetAttributes(tcell.AttrBold).SetExpansion(1))
		row++
		u.debugTable.SetCell(row, 0, tview.NewTableCell("Task"))
		u.debugTable.SetCell(row, 1, tview.NewTableCell("Conv"))
		u.debugTable.SetCell(row, 2, tview.NewTableCell("Status"))
		u.debugTable.SetCell(row, 3, tview.NewTableCell("Next"))
		u.debugTable.SetCell(row, 4, tview.NewTableCell("Last"))
		u.debugTable.SetCell(row, 5, tview.NewTableCell("Failures"))
		row++

		if len(s.RecentTasks) == 0 {
			u.debugTable.SetCell(row, 0, tview.NewTableCell("none"))
			return
		}
		for i := 0; i < len(s.RecentTasks) && i < 8; i++ {
			t := s.RecentTasks[i]
			last := t.LastRunStatus
			if t.LastRunStartedAt != nil {
				last = last + "@" + t.LastRunStartedAt.Format("15:04:05")
			}
			u.debugTable.SetCell(row, 0, tview.NewTableCell(t.ID))
			u.debugTable.SetCell(row, 1, tview.NewTableCell(t.ConversationID))
			u.debugTable.SetCell(row, 2, tview.NewTableCell(t.Status))
			u.debugTable.SetCell(row, 3, tview.NewTableCell(t.NextRunAt.Format("15:04:05")))
			u.debugTable.SetCell(row, 4, tview.NewTableCell(defaultIfEmpty(last, "-")))
			u.debugTable.SetCell(row, 5, tview.NewTableCell(strconv.Itoa(t.FailureCount)))
			row++
		}
	}
}

func (u *UI) handleCommand(ctx context.Context, cmd parsedCommand, onCommand func(context.Context, CommandAction) (string, error)) {
	u.mu.Lock()
	defer u.mu.Unlock()

	switch cmd.kind {
	case commandHelp:
		u.input.SetText("")
		u.renderLocked("commands: /new [conversation], /switch <conversation>, /rename <conversation>, /remove [conversation], /tasks <ls|ls-all|ls-conversation|rm|rm-all>, /help, /quit, /exit")
	case commandQuit:
		u.input.SetText("")
		u.renderLocked("exiting...")
		go u.app.Stop()
	case commandNew:
		conversationID := strings.TrimSpace(cmd.arg)
		if conversationID == "" {
			conversationID = u.state.nextDefaultConversationID()
		}
		idx, created := u.state.ensureConversationWithCreated(conversationID)
		u.state.switchToIndex(idx)
		u.input.SetText("")
		if created {
			u.renderLocked("created conversation " + conversationID)
		} else {
			u.renderLocked("conversation " + conversationID + " already exists")
		}
	case commandSwitch:
		if !u.state.hasConversation(cmd.arg) {
			u.renderLocked("conversation " + cmd.arg + " not found (create with /new)")
			return
		}
		idx := u.state.ensureConversation(cmd.arg)
		u.state.switchToIndex(idx)
		u.input.SetText("")
		u.renderLocked("switched to conversation " + cmd.arg)
	case commandRename:
		oldID, err := u.state.renameActiveConversation(cmd.arg)
		u.input.SetText("")
		if err != nil {
			u.renderLocked("rename failed: " + err.Error())
			return
		}
		if oldID == cmd.arg {
			u.renderLocked("conversation already named " + cmd.arg)
			return
		}
		u.renderLocked("renamed " + oldID + " to " + cmd.arg)
	case commandRemove:
		target := strings.TrimSpace(cmd.arg)
		if target == "" {
			target = u.state.activeConversation()
		}
		if !u.state.hasConversation(target) {
			u.input.SetText("")
			u.renderLocked("conversation " + target + " not found")
			return
		}
		u.input.SetText("")
		u.renderLocked("removing conversation " + target + "...")
		go func(conversationID string) {
			if onCommand == nil {
				u.setStatus("remove failed: backend command handler unavailable")
				return
			}
			result, err := onCommand(ctx, CommandAction{
				Kind:           CommandRemoveConversation,
				ConversationID: conversationID,
			})
			if err != nil {
				u.setStatus("remove failed: " + err.Error())
				return
			}
			u.app.QueueUpdateDraw(func() {
				u.mu.Lock()
				defer u.mu.Unlock()
				removed, active := u.state.removeConversation(conversationID)
				if !removed {
					u.renderLocked("conversation " + conversationID + " not found")
					return
				}
				status := "removed conversation " + conversationID + "; active=" + active
				if strings.TrimSpace(result) != "" {
					status = result
				}
				u.renderLocked(status)
			})
		}(target)
	case commandTasks:
		u.input.SetText("")
		action, usageErr := parseTasksAction(strings.TrimSpace(cmd.arg))
		if usageErr != "" {
			u.renderLocked(usageErr)
			return
		}
		u.renderLocked("running /tasks ...")
		go func(a CommandAction) {
			if onCommand == nil {
				u.setStatus("tasks failed: backend command handler unavailable")
				return
			}
			result, err := onCommand(ctx, a)
			if err != nil {
				u.setStatus("tasks failed: " + err.Error())
				return
			}
			u.app.QueueUpdateDraw(func() {
				u.mu.Lock()
				defer u.mu.Unlock()
				if strings.TrimSpace(result) != "" {
					u.state.appendAssistantMessage(u.state.activeConversation(), result)
				}
				u.renderLocked("tasks command complete")
			})
		}(action)
	default:
		u.renderLocked("invalid command (try /help)")
	}
}

func parseTasksAction(raw string) (CommandAction, string) {
	if strings.TrimSpace(raw) == "" {
		return CommandAction{}, "usage: /tasks ls [conversation] | /tasks ls-all | /tasks ls-conversation <id> | /tasks rm <task_id> | /tasks rm-all --all|<conversation>"
	}
	parts := strings.Fields(raw)
	sub := strings.ToLower(strings.TrimSpace(parts[0]))
	switch sub {
	case "ls":
		action := CommandAction{Kind: CommandTasksList}
		if len(parts) >= 2 {
			if parts[1] == "--conversation" && len(parts) >= 3 {
				action.ConversationID = strings.TrimSpace(parts[2])
			} else {
				action.ConversationID = strings.TrimSpace(parts[1])
			}
		}
		return action, ""
	case "ls-all":
		return CommandAction{Kind: CommandTasksList}, ""
	case "ls-conversation":
		if len(parts) < 2 {
			return CommandAction{}, "usage: /tasks ls-conversation <conversation_id>"
		}
		if parts[1] == "--conversation" {
			if len(parts) < 3 {
				return CommandAction{}, "usage: /tasks ls-conversation --conversation <conversation_id>"
			}
			return CommandAction{Kind: CommandTasksList, ConversationID: strings.TrimSpace(parts[2])}, ""
		}
		return CommandAction{Kind: CommandTasksList, ConversationID: strings.TrimSpace(parts[1])}, ""
	case "rm":
		if len(parts) < 2 {
			return CommandAction{}, "usage: /tasks rm <task_id>"
		}
		taskID := strings.TrimSpace(parts[1])
		if parts[1] == "--id" {
			if len(parts) < 3 {
				return CommandAction{}, "usage: /tasks rm --id <task_id>"
			}
			taskID = strings.TrimSpace(parts[2])
		}
		if taskID == "" {
			return CommandAction{}, "usage: /tasks rm <task_id>"
		}
		return CommandAction{Kind: CommandTasksRemove, TaskID: taskID}, ""
	case "rm-all":
		if len(parts) < 2 {
			return CommandAction{}, "usage: /tasks rm-all --all|<conversation_id>|--conversation <conversation_id>"
		}
		if parts[1] == "--all" {
			return CommandAction{Kind: CommandTasksRemoveAll, All: true}, ""
		}
		if parts[1] == "--conversation" {
			if len(parts) < 3 {
				return CommandAction{}, "usage: /tasks rm-all --conversation <conversation_id>"
			}
			return CommandAction{Kind: CommandTasksRemoveAll, ConversationID: strings.TrimSpace(parts[2])}, ""
		}
		return CommandAction{Kind: CommandTasksRemoveAll, ConversationID: strings.TrimSpace(parts[1])}, ""
	default:
		return CommandAction{}, "unknown /tasks subcommand"
	}
}

func (u *UI) triggerFlashLocked(index int) {
	u.flashIndex = index
	u.flashUntil = time.Now().Add(180 * time.Millisecond)
	expire := u.flashUntil
	go func() {
		time.Sleep(200 * time.Millisecond)
		u.app.QueueUpdateDraw(func() {
			u.mu.Lock()
			defer u.mu.Unlock()
			if u.flashIndex == index && u.flashUntil.Equal(expire) && time.Now().After(u.flashUntil) {
				u.renderLocked(u.statusText)
			}
		})
	}()
}

func (u *UI) setLastKey(v string) {
	u.mu.Lock()
	u.lastKey = strings.TrimSpace(v)
	u.mu.Unlock()
}

func keyString(event *tcell.EventKey) string {
	if event == nil {
		return ""
	}
	switch event.Key() {
	case tcell.KeyCtrlC:
		return "Ctrl+c"
	case tcell.KeyCtrlN:
		return "Ctrl+n"
	case tcell.KeyCtrlP:
		return "Ctrl+p"
	case tcell.KeyCtrlG:
		return "Ctrl+g"
	case tcell.KeyTab:
		return "Tab"
	case tcell.KeyBacktab:
		return "Shift+Tab"
	case tcell.KeyEnter:
		return "Enter"
	case tcell.KeyEsc:
		return "Esc"
	case tcell.KeyRune:
		r := event.Rune()
		if event.Modifiers()&tcell.ModAlt != 0 {
			return "Alt+" + string(r)
		}
		return string(r)
	default:
		return event.Name()
	}
}

func defaultIfEmpty(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func (u *UI) debugSuffixLocked() string {
	if !u.debugEnabled {
		return ""
	}
	return "\nDEBUG: dashboard mode (" + debugSectionName(u.debugTabIndex) + ")"
}

const (
	debugTabSummary = iota
	debugTabQueue
	debugTabWorkers
	debugTabRuns
	debugSectionCount
)

func debugSectionName(idx int) string {
	switch clampDebugTab(idx) {
	case debugTabSummary:
		return "Summary"
	case debugTabQueue:
		return "Queue"
	case debugTabWorkers:
		return "Workers/Schedulers"
	case debugTabRuns:
		return "Last Runs"
	default:
		return "Summary"
	}
}

func clampDebugTab(idx int) int {
	if idx < 0 || idx >= debugSectionCount {
		return debugTabSummary
	}
	return idx
}

func (u *UI) setDebugTabLocked(index int) {
	u.debugTabIndex = clampDebugTab(index)
}

func (u *UI) cycleDebugTabLocked(delta int) {
	next := u.debugTabIndex + delta
	if next < 0 {
		next = debugSectionCount - 1
	}
	if next >= debugSectionCount {
		next = 0
	}
	u.debugTabIndex = next
}
