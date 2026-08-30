// Package llm ruft einen OpenAI-kompatiblen /chat/completions-Endpoint auf und
// zieht Termine aus einem Mailtext.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// SystemPrompt — die Regeln stammen aus dem Produktivbetrieb. Vor allem das
// Verbot, Daten zu erfinden, ist entscheidend: ohne das baut das Modell aus
// Werbemails Fantasietermine.
const SystemPrompt = `Du bist ein Assistent, der aus E-Mails ALLE Termine für einen Kalender extrahiert
(Flüge, Bahnfahrten, Konzert- und Veranstaltungstickets, Hotelreservierungen, Buchungen, Arzt- und Friseurtermine).

Antworte AUSSCHLIESSLICH mit JSON in genau diesem Schema:
{"promotional": bool,
 "events": [{"title": string, "start": string (ISO 8601), "end": string (ISO 8601),
             "all_day": bool, "location": string|null, "description": string|null,
             "confidence": number (0..1)}]}

promotional: true, wenn die Mail im Kern Werbung oder ein Newsletter ist.

Regeln zum Datum (SEHR WICHTIG):
- Verwende NUR Daten und Uhrzeiten, die wörtlich im Text (inklusive Betreff) stehen. ERFINDE NIEMALS ein Datum.
- Verwechsle das Buchungs- oder Versanddatum NICHT mit dem eigentlichen Termindatum. Achte auf den Monat.
- Steht das Datum nur im Betreff, nutze genau dieses Datum.
- Findest du für einen Teil-Termin (etwa den Rückflug) kein Datum, lass dieses Event WEG.
  Lieber ein korrektes Event als zwei mit geratenem Datum.

Regeln zur Struktur:
- Hin- UND Rückreise ergeben ZWEI separate Events, jeweils mit eigenem Datum und eigener Uhrzeit.
- Mehrtägige Aufenthalte (Hotel, Miete) ergeben EIN Event von Check-in (start) bis Check-out (end).
- Mehrere Segmente oder Termine in einer Mail ergeben je ein eigenes Event.
- Nur Datum ohne Uhrzeit: all_day = true.
- Fehlt eine Dauer: end = start + 1 Stunde.
- title kurz und sprechend, zum Beispiel "Flug FRA→LHR" oder "Zahnarzt Dr. Meier".

Wenn KEIN klarer Termin mit Datum erkennbar ist: events = [].`

type Event struct {
	Title       string  `json:"title"`
	Start       string  `json:"start"`
	End         string  `json:"end"`
	AllDay      bool    `json:"all_day"`
	Location    string  `json:"location"`
	Description string  `json:"description"`
	Confidence  float64 `json:"confidence"`
}

type Result struct {
	Promotional bool    `json:"promotional"`
	Events      []Event `json:"events"`
}

type Client struct {
	BaseURL string
	APIKey  string
	Model   string
	HTTP    *http.Client
}

func New(baseURL, apiKey, model string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		Model:   model,
		HTTP:    &http.Client{Timeout: 120 * time.Second},
	}
}

type chatRequest struct {
	Model          string        `json:"model"`
	Messages       []chatMessage `json:"messages"`
	Temperature    float64       `json:"temperature"`
	ResponseFormat *respFormat   `json:"response_format,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type respFormat struct {
	Type string `json:"type"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Extract schickt den Mailtext ans Modell und liefert die erkannten Termine.
func (c *Client) Extract(ctx context.Context, mailText string, now time.Time, tz string) (*Result, error) {
	user := fmt.Sprintf("Aktuelles Datum: %s (Zeitzone %s).\n\n%s",
		now.Format("2006-01-02 15:04 Montag"), tz, mailText)

	body, err := json.Marshal(chatRequest{
		Model:       c.Model,
		Temperature: 0,
		Messages: []chatMessage{
			{Role: "system", Content: SystemPrompt},
			{Role: "user", Content: user},
		},
		ResponseFormat: &respFormat{Type: "json_object"},
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("LLM nicht erreichbar: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("LLM antwortete mit %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}

	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return nil, fmt.Errorf("Antwort unlesbar: %w", err)
	}
	if cr.Error != nil {
		return nil, fmt.Errorf("LLM-Fehler: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return nil, fmt.Errorf("LLM lieferte keine Antwort")
	}
	return ParseResult(cr.Choices[0].Message.Content)
}

var reFence = regexp.MustCompile("(?s)```(?:json)?(.*?)```")

// ParseResult ist tolerant: manche Modelle verpacken das JSON in Code-Fences
// oder stellen einen Satz voran.
func ParseResult(content string) (*Result, error) {
	s := strings.TrimSpace(content)
	if m := reFence.FindStringSubmatch(s); m != nil {
		s = strings.TrimSpace(m[1])
	}
	if i := strings.Index(s, "{"); i > 0 {
		s = s[i:]
	}
	if j := strings.LastIndex(s, "}"); j >= 0 && j < len(s)-1 {
		s = s[:j+1]
	}
	var r Result
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		return nil, fmt.Errorf("kein gültiges JSON vom Modell: %w", err)
	}
	return &r, nil
}

// Ping prüft Erreichbarkeit und Modellnamen mit einem Minimal-Request.
func (c *Client) Ping(ctx context.Context) error {
	body, _ := json.Marshal(chatRequest{
		Model:       c.Model,
		Temperature: 0,
		Messages:    []chatMessage{{Role: "user", Content: "ping"}},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("nicht erreichbar: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
