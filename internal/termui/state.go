package termui

import (
	"strconv"
	"strings"
)

type conversationState struct {
	conversations []string
	activeIndex   int
	messages      map[string][]string
	unread        map[string]int
}

func newConversationState(defaultConversation string) *conversationState {
	defaultID := strings.TrimSpace(defaultConversation)
	if defaultID == "" {
		defaultID = "main"
	}
	return &conversationState{
		conversations: []string{defaultID},
		activeIndex:   0,
		messages:      map[string][]string{defaultID: {}},
		unread:        map[string]int{},
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
	id = strings.TrimSpace(id)
	if id == "" {
		id = "main"
	}
	for i, existing := range s.conversations {
		if existing == id {
			if _, ok := s.messages[id]; !ok {
				s.messages[id] = nil
			}
			return i
		}
	}
	s.conversations = append(s.conversations, id)
	s.messages[id] = nil
	return len(s.conversations) - 1
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
	s.messages[s.conversations[idx]] = append(s.messages[s.conversations[idx]], "you: "+content)
}

func (s *conversationState) appendAssistantMessage(conversationID, content string) {
	idx := s.ensureConversation(conversationID)
	id := s.conversations[idx]
	s.messages[id] = append(s.messages[id], "assistant: "+content)
	if idx != s.activeIndex {
		s.unread[id]++
	}
}

func parseInputLine(activeConversation, raw string) (conversationID, content string, ok bool) {
	line := strings.TrimSpace(raw)
	if line == "" {
		return "", "", false
	}
	if cid, msg, hasColon := strings.Cut(line, ":"); hasColon && strings.TrimSpace(cid) != "" && strings.TrimSpace(msg) != "" {
		return strings.TrimSpace(cid), strings.TrimSpace(msg), true
	}
	return strings.TrimSpace(activeConversation), line, true
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
