package termui

import (
	"errors"
	"strconv"
	"strings"
)

type conversationState struct {
	conversations  []string
	activeIndex    int
	messages       map[string][]string
	unread         map[string]int
	userLabel      string
	assistantLabel string
}

func newConversationState(defaultConversation, userLabel, assistantLabel string) *conversationState {
	defaultID := strings.TrimSpace(defaultConversation)
	if defaultID == "" {
		defaultID = "main"
	}
	userLabel = strings.TrimSpace(userLabel)
	if userLabel == "" {
		userLabel = "you"
	}
	assistantLabel = strings.TrimSpace(assistantLabel)
	if assistantLabel == "" {
		assistantLabel = "goclaw"
	}
	return &conversationState{
		conversations:  []string{defaultID},
		activeIndex:    0,
		messages:       map[string][]string{defaultID: {}},
		unread:         map[string]int{},
		userLabel:      userLabel,
		assistantLabel: assistantLabel,
	}
}

func (s *conversationState) activeConversation() string {
	if len(s.conversations) == 0 {
		return "main"
	}
	if s.activeIndex < 0 || s.activeIndex >= len(s.conversations) {
		s.activeIndex = 0
	}
	return s.conversations[s.activeIndex]
}

func (s *conversationState) ensureConversation(id string) int {
	idx, _ := s.ensureConversationWithCreated(id)
	return idx
}

func (s *conversationState) ensureConversationWithCreated(id string) (index int, created bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		id = "main"
	}
	for i, existing := range s.conversations {
		if existing == id {
			if _, ok := s.messages[id]; !ok {
				s.messages[id] = nil
			}
			return i, false
		}
	}
	s.conversations = append(s.conversations, id)
	s.messages[id] = nil
	return len(s.conversations) - 1, true
}

func (s *conversationState) hasConversation(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	for _, existing := range s.conversations {
		if existing == id {
			return true
		}
	}
	return false
}

func (s *conversationState) switchToIndex(index int) bool {
	if index < 0 || index >= len(s.conversations) {
		return false
	}
	s.activeIndex = index
	s.unread[s.activeConversation()] = 0
	return true
}

func (s *conversationState) cycleNext() {
	if len(s.conversations) == 0 {
		return
	}
	s.activeIndex = (s.activeIndex + 1) % len(s.conversations)
	s.unread[s.activeConversation()] = 0
}

func (s *conversationState) cyclePrev() {
	if len(s.conversations) == 0 {
		return
	}
	s.activeIndex--
	if s.activeIndex < 0 {
		s.activeIndex = len(s.conversations) - 1
	}
	s.unread[s.activeConversation()] = 0
}

func (s *conversationState) appendUserMessage(conversationID, content string) {
	idx := s.ensureConversation(conversationID)
	s.messages[s.conversations[idx]] = append(s.messages[s.conversations[idx]], s.userLabel+": "+content)
}

func (s *conversationState) appendAssistantMessage(conversationID, content string) {
	idx := s.ensureConversation(conversationID)
	id := s.conversations[idx]
	s.messages[id] = append(s.messages[id], s.assistantLabel+": "+content)
	if idx != s.activeIndex {
		s.unread[id]++
	}
}

func (s *conversationState) renameActiveConversation(newID string) (string, error) {
	newID = strings.TrimSpace(newID)
	if newID == "" {
		return "", errors.New("conversation id must be non-empty")
	}
	oldID := s.activeConversation()
	if newID == oldID {
		return oldID, nil
	}
	if s.hasConversation(newID) {
		return oldID, errors.New("conversation already exists")
	}

	s.conversations[s.activeIndex] = newID
	if msgs, ok := s.messages[oldID]; ok {
		s.messages[newID] = msgs
		delete(s.messages, oldID)
	} else {
		s.messages[newID] = nil
	}
	if unread, ok := s.unread[oldID]; ok {
		s.unread[newID] = unread
		delete(s.unread, oldID)
	}
	return oldID, nil
}

func (s *conversationState) nextDefaultConversationID() string {
	for i := 1; ; i++ {
		candidate := "default-" + strconv.Itoa(i)
		if !s.hasConversation(candidate) {
			return candidate
		}
	}
}

type commandKind int

const (
	commandNone commandKind = iota
	commandNew
	commandSwitch
	commandRename
	commandHelp
	commandQuit
	commandInvalid
)

type parsedCommand struct {
	kind commandKind
	arg  string
}

func parseCommand(raw string) parsedCommand {
	line := strings.TrimSpace(raw)
	if line == "" || !strings.HasPrefix(line, "/") {
		return parsedCommand{kind: commandNone}
	}

	parts := strings.Fields(line)
	if len(parts) == 0 {
		return parsedCommand{kind: commandInvalid}
	}

	switch strings.ToLower(parts[0]) {
	case "/new":
		if len(parts) == 1 {
			return parsedCommand{kind: commandNew}
		}
		if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
			return parsedCommand{kind: commandInvalid}
		}
		return parsedCommand{kind: commandNew, arg: strings.TrimSpace(parts[1])}
	case "/switch":
		if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
			return parsedCommand{kind: commandInvalid}
		}
		return parsedCommand{kind: commandSwitch, arg: strings.TrimSpace(parts[1])}
	case "/rename":
		if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
			return parsedCommand{kind: commandInvalid}
		}
		return parsedCommand{kind: commandRename, arg: strings.TrimSpace(parts[1])}
	case "/help":
		return parsedCommand{kind: commandHelp}
	case "/quit", "/exit":
		return parsedCommand{kind: commandQuit}
	default:
		return parsedCommand{kind: commandInvalid}
	}
}

func indexFromRune(r rune) (int, bool) {
	if r < '1' || r > '9' {
		return 0, false
	}
	n, err := strconv.Atoi(string(r))
	if err != nil {
		return 0, false
	}
	return n - 1, true
}
