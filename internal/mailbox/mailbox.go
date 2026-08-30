// Package mailbox kapselt den IMAP-Zugriff: Ordner auflisten, Kandidaten holen,
// MIME auspacken und Mails wegsortieren.
package mailbox

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/mail"

	_ "github.com/emersion/go-message/charset" // ISO-8859-1 & Co. beim Dekodieren
)

// Message ist eine Mail, reduziert auf das, was für die Auswertung zählt.
type Message struct {
	UID         imap.UID
	MessageID   string
	Subject     string
	From        string
	Date        time.Time
	Body        string
	Promotional bool // aus List-Unsubscribe / Precedence abgeleitet
	PDFNames    []string
}

type Client struct {
	c *imapclient.Client
}

func Dial(host string, port int, user, password string) (*Client, error) {
	addr := fmt.Sprintf("%s:%d", host, port)
	c, err := imapclient.DialTLS(addr, nil)
	if err != nil {
		return nil, fmt.Errorf("Verbindung zu %s fehlgeschlagen: %w", addr, err)
	}
	if err := c.Login(user, password).Wait(); err != nil {
		c.Close()
		return nil, fmt.Errorf("Anmeldung fehlgeschlagen: %w", err)
	}
	return &Client{c: c}, nil
}

func (m *Client) Close() {
	if m.c != nil {
		_ = m.c.Logout().Wait()
		_ = m.c.Close()
	}
}

// Folders listet alle Ordner des Postfachs.
func (m *Client) Folders() ([]string, error) {
	entries, err := m.c.List("", "*", nil).Collect()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Mailbox)
	}
	return out, nil
}

// Fetch liefert bis zu limit Mails aus folder. unseenOnly beschränkt auf
// ungelesene, lookbackDays auf die jüngere Vergangenheit.
func (m *Client) Fetch(folder string, lookbackDays, limit int, unseenOnly bool) ([]Message, error) {
	if _, err := m.c.Select(folder, nil).Wait(); err != nil {
		return nil, fmt.Errorf("Ordner %q nicht lesbar: %w", folder, err)
	}

	criteria := &imap.SearchCriteria{}
	if lookbackDays > 0 {
		criteria.Since = time.Now().AddDate(0, 0, -lookbackDays)
	}
	if unseenOnly {
		criteria.NotFlag = []imap.Flag{imap.FlagSeen}
	}

	data, err := m.c.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("Suche fehlgeschlagen: %w", err)
	}
	uids := data.AllUIDs()
	if len(uids) == 0 {
		return nil, nil
	}
	// Neueste zuerst, dann kappen — ein volles Postfach soll den Lauf nicht sprengen.
	for i, j := 0, len(uids)-1; i < j; i, j = i+1, j-1 {
		uids[i], uids[j] = uids[j], uids[i]
	}
	if limit > 0 && len(uids) > limit {
		uids = uids[:limit]
	}

	var set imap.UIDSet
	set.AddNum(uids...)

	opts := &imap.FetchOptions{
		UID:         true,
		Envelope:    true,
		BodySection: []*imap.FetchItemBodySection{{}},
	}
	buffers, err := m.c.Fetch(set, opts).Collect()
	if err != nil {
		return nil, fmt.Errorf("Abruf fehlgeschlagen: %w", err)
	}

	out := make([]Message, 0, len(buffers))
	for _, b := range buffers {
		var raw []byte
		for _, sec := range b.BodySection {
			if len(sec.Bytes) > 0 {
				raw = sec.Bytes
				break
			}
		}
		if len(raw) == 0 {
			continue
		}
		msg, err := parse(raw)
		if err != nil {
			// Eine kaputte Mail darf den ganzen Lauf nicht stoppen.
			continue
		}
		msg.UID = b.UID
		if msg.MessageID == "" && b.Envelope != nil {
			msg.MessageID = b.Envelope.MessageID
		}
		if msg.Subject == "" && b.Envelope != nil {
			msg.Subject = b.Envelope.Subject
		}
		if msg.Date.IsZero() && b.Envelope != nil {
			msg.Date = b.Envelope.Date
		}
		out = append(out, msg)
	}
	return out, nil
}

// MoveTo verschiebt eine Mail; legt den Zielordner bei Bedarf an.
func (m *Client) MoveTo(uid imap.UID, dest string) error {
	if dest == "" {
		return nil
	}
	if err := m.c.Create(dest, nil).Wait(); err != nil {
		// existiert bereits — kein Fehlerfall
		_ = err
	}
	var set imap.UIDSet
	set.AddNum(uid)
	_, err := m.c.Move(set, dest).Wait()
	return err
}

// MarkSeen setzt das \Seen-Flag.
func (m *Client) MarkSeen(uid imap.UID) error {
	var set imap.UIDSet
	set.AddNum(uid)
	store := &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagSeen}}
	cmd := m.c.Store(set, store, nil)
	if cmd == nil {
		return nil
	}
	_, err := cmd.Collect()
	return err
}

// parse packt die MIME-Struktur aus und liefert Kopfdaten plus Textkörper.
func parse(raw []byte) (Message, error) {
	var out Message

	mr, err := mail.CreateReader(strings.NewReader(string(raw)))
	if err != nil {
		return out, err
	}
	h := mr.Header
	out.Subject, _ = h.Subject()
	out.MessageID, _ = h.MessageID()
	if out.MessageID != "" && !strings.HasPrefix(out.MessageID, "<") {
		out.MessageID = "<" + out.MessageID + ">"
	}
	if addrs, err := h.AddressList("From"); err == nil && len(addrs) > 0 {
		out.From = addrs[0].String()
	}
	if d, err := h.Date(); err == nil {
		out.Date = d
	}
	// Massenmail-Erkennung ohne LLM — billig und zuverlässig.
	if v := h.Get("List-Unsubscribe"); v != "" {
		out.Promotional = true
	}
	if v := strings.ToLower(h.Get("Precedence")); v == "bulk" || v == "list" {
		out.Promotional = true
	}

	var plain, html []string
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		} else if err != nil {
			break
		}
		switch ph := p.Header.(type) {
		case *mail.InlineHeader:
			ct, _, _ := ph.ContentType()
			body, _ := io.ReadAll(p.Body)
			if ct == "text/html" {
				html = append(html, string(body))
			} else {
				plain = append(plain, string(body))
			}
		case *mail.AttachmentHeader:
			name, _ := ph.Filename()
			if strings.HasSuffix(strings.ToLower(name), ".pdf") {
				out.PDFNames = append(out.PDFNames, name)
			}
		}
	}

	body := strings.TrimSpace(strings.Join(plain, "\n"))
	if body == "" && len(html) > 0 {
		body = HTMLToText(strings.Join(html, "\n"))
	}
	out.Body = body
	return out, nil
}
