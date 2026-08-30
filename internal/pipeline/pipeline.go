// Package pipeline verbindet Postfach, Modell und Kalender zu einem Durchlauf.
package pipeline

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/pheskde/termiq/internal/calendar"
	"github.com/pheskde/termiq/internal/config"
	"github.com/pheskde/termiq/internal/ledger"
	"github.com/pheskde/termiq/internal/llm"
	"github.com/pheskde/termiq/internal/mailbox"
)

type Options struct {
	DryRun     bool
	LedgerPath string
	Logf       func(format string, args ...any)
}

type Summary struct {
	Examined     int
	SkippedSeen  int
	SkippedOld   int
	SkippedPromo int
	SkippedLowCf int
	EventsMade   int
	Errors       []string
}

func Run(ctx context.Context, cfg *config.Config, opt Options) (Summary, error) {
	var sum Summary
	logf := opt.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if err := cfg.Validate(); err != nil {
		return sum, err
	}

	loc, err := time.LoadLocation(cfg.Behavior.Timezone)
	if err != nil {
		loc = time.Local
		logf("Zeitzone %q unbekannt, nutze lokale Zeit", cfg.Behavior.Timezone)
	}

	led, err := ledger.Open(opt.LedgerPath)
	if err != nil {
		return sum, fmt.Errorf("Journal nicht lesbar: %w", err)
	}

	mb, err := mailbox.Dial(cfg.IMAP.Host, cfg.IMAP.Port, cfg.IMAP.User, cfg.IMAP.Password)
	if err != nil {
		return sum, err
	}
	defer mb.Close()

	msgs, err := mb.Fetch(cfg.IMAP.Folder, cfg.Behavior.LookbackDays,
		cfg.Behavior.MaxMailsPerRun, cfg.Behavior.UnseenOnly)
	if err != nil {
		return sum, err
	}
	logf("%d Mail(s) im Zugriff", len(msgs))

	var cal *calendar.Client
	if !opt.DryRun {
		cal, err = calendar.Connect(cfg.CalDAV.URL, cfg.CalDAV.User, cfg.CalDAV.Password, cfg.CalDAV.SkipVerify)
		if err != nil {
			return sum, err
		}
	}

	model := llm.New(cfg.LLM.BaseURL, cfg.LLM.APIKey, cfg.LLM.Model)
	now := time.Now().In(loc)

	for _, m := range msgs {
		sum.Examined++
		label := trim(m.Subject, 60)

		if led.Seen(m.MessageID) {
			sum.SkippedSeen++
			continue
		}
		if cfg.Behavior.MaxAgeDays > 0 && !m.Date.IsZero() {
			if now.Sub(m.Date) > time.Duration(cfg.Behavior.MaxAgeDays)*24*time.Hour {
				sum.SkippedOld++
				led.Mark(m.MessageID, m.Subject, "zu_alt", 0)
				logf("übersprungen (zu alt): %s", label)
				continue
			}
		}
		if cfg.Behavior.SkipPromotional && m.Promotional {
			sum.SkippedPromo++
			led.Mark(m.MessageID, m.Subject, "werbung_header", 0)
			logf("übersprungen (Massenmail): %s", label)
			continue
		}

		text := buildPrompt(m)
		res, err := model.Extract(ctx, text, now, cfg.Behavior.Timezone)
		if err != nil {
			sum.Errors = append(sum.Errors, fmt.Sprintf("%s: %v", label, err))
			logf("FEHLER bei %q: %v", label, err)
			continue
		}
		if cfg.Behavior.SkipPromotional && res.Promotional {
			sum.SkippedPromo++
			led.Mark(m.MessageID, m.Subject, "werbung_llm", 0)
			logf("übersprungen (Werbung laut Modell): %s", label)
			continue
		}

		made := 0
		for _, ev := range res.Events {
			if ev.Confidence > 0 && ev.Confidence < cfg.Behavior.MinConfidence {
				sum.SkippedLowCf++
				logf("verworfen (Confidence %.2f): %s", ev.Confidence, trim(ev.Title, 50))
				continue
			}
			start, ok := parseTime(ev.Start, loc)
			if !ok {
				logf("verworfen (Startdatum unlesbar %q): %s", ev.Start, trim(ev.Title, 50))
				continue
			}
			end, ok := parseTime(ev.End, loc)
			if !ok {
				if ev.AllDay {
					end = start.AddDate(0, 0, 1)
				} else {
					end = start.Add(time.Hour)
				}
			}
			if !end.After(start) {
				end = start.Add(time.Hour)
			}

			title := ev.Title
			if strings.TrimSpace(title) == "" {
				title = m.Subject
			}
			desc := ev.Description
			if strings.TrimSpace(desc) == "" {
				desc = m.Subject
			}

			if opt.DryRun {
				logf("WÜRDE ANLEGEN: %s | %s – %s | %s", trim(title, 60),
					start.In(loc).Format("02.01.2006 15:04"), end.In(loc).Format("15:04"), ev.Location)
				made++
				continue
			}
			if _, err := cal.CreateEvent(ctx, cfg.CalDAV.CalendarPath, calendar.Event{
				Title: title, Start: start, End: end, AllDay: ev.AllDay,
				Location: ev.Location, Description: desc,
			}); err != nil {
				sum.Errors = append(sum.Errors, fmt.Sprintf("%s: %v", trim(title, 40), err))
				logf("FEHLER beim Anlegen von %q: %v", trim(title, 50), err)
				continue
			}
			logf("angelegt: %s | %s", trim(title, 60), start.In(loc).Format("02.01.2006 15:04"))
			made++
		}
		sum.EventsMade += made

		if opt.DryRun {
			continue
		}

		outcome := "kein_termin"
		if made > 0 {
			outcome = "termin_angelegt"
		}
		led.Mark(m.MessageID, m.Subject, outcome, made)

		if made > 0 && cfg.IMAP.ArchiveFolder != "" {
			if err := mb.MoveTo(m.UID, cfg.IMAP.ArchiveFolder); err != nil {
				logf("Verschieben nach %q fehlgeschlagen: %v", cfg.IMAP.ArchiveFolder, err)
			}
		} else if cfg.IMAP.MarkSeen {
			_ = mb.MarkSeen(m.UID)
		}
	}

	if !opt.DryRun {
		if err := led.Save(); err != nil {
			logf("Journal konnte nicht geschrieben werden: %v", err)
		}
	}
	return sum, nil
}

func buildPrompt(m mailbox.Message) string {
	var b strings.Builder
	b.WriteString("Betreff: " + m.Subject + "\n")
	if m.From != "" {
		b.WriteString("Von: " + m.From + "\n")
	}
	if !m.Date.IsZero() {
		b.WriteString("Gesendet: " + m.Date.Format(time.RFC1123) + "\n")
	}
	if len(m.PDFNames) > 0 {
		b.WriteString("PDF-Anhänge (Inhalt nicht ausgewertet): " + strings.Join(m.PDFNames, ", ") + "\n")
	}
	b.WriteString("\n")
	b.WriteString(m.Body)
	return trim(b.String(), 24000)
}

// parseTime akzeptiert die üblichen ISO-Varianten. Zeiten ohne Zonenangabe
// gelten in der konfigurierten Zeitzone, nicht in UTC.
func parseTime(s string, loc *time.Location) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	withZone := []string{time.RFC3339, "2006-01-02T15:04:05Z0700", "2006-01-02 15:04:05Z07:00"}
	for _, layout := range withZone {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	naive := []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05",
		"2006-01-02 15:04", "2006-01-02"}
	for _, layout := range naive {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func trim(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
