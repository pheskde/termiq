// Package ledger merkt sich verarbeitete Mails anhand der Message-ID.
// Ohne dieses Gedächtnis legt jeder Lauf dieselben Termine erneut an.
package ledger

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Entry struct {
	ProcessedAt time.Time `json:"processed_at"`
	Subject     string    `json:"subject"`
	Events      int       `json:"events"`
	Outcome     string    `json:"outcome"`
}

type Ledger struct {
	path    string
	mu      sync.Mutex
	Entries map[string]Entry `json:"entries"`
}

func Open(path string) (*Ledger, error) {
	l := &Ledger{path: path, Entries: map[string]Entry{}}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return l, nil
		}
		return nil, err
	}
	if len(raw) == 0 {
		return l, nil
	}
	if err := json.Unmarshal(raw, l); err != nil {
		// Beschädigtes Journal: lieber neu anfangen als den Lauf blockieren.
		l.Entries = map[string]Entry{}
	}
	if l.Entries == nil {
		l.Entries = map[string]Entry{}
	}
	return l, nil
}

func (l *Ledger) Seen(messageID string) bool {
	if messageID == "" {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.Entries[messageID]
	return ok
}

func (l *Ledger) Mark(messageID, subject, outcome string, events int) {
	if messageID == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Entries[messageID] = Entry{
		ProcessedAt: time.Now(),
		Subject:     subject,
		Events:      events,
		Outcome:     outcome,
	}
}

func (l *Ledger) Save() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	raw, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(l.path); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	return os.WriteFile(l.path, raw, 0o600)
}
