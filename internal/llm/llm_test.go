package llm

import "testing"

func TestParseResult(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		events int
		promo  bool
		fail   bool
	}{
		{
			name:   "sauberes JSON",
			in:     `{"promotional":false,"events":[{"title":"Zahnarzt","start":"2026-09-01T10:00:00","end":"2026-09-01T10:30:00","all_day":false,"confidence":0.9}]}`,
			events: 1,
		},
		{
			// Manche Modelle verpacken die Antwort trotz json_object in Fences.
			name:   "in Code-Fence",
			in:     "```json\n{\"promotional\":true,\"events\":[]}\n```",
			events: 0,
			promo:  true,
		},
		{
			name:   "mit Vorrede",
			in:     "Hier das Ergebnis:\n{\"promotional\":false,\"events\":[]}",
			events: 0,
		},
		{
			name: "kein JSON",
			in:   "Ich konnte keinen Termin finden.",
			fail: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, err := ParseResult(c.in)
			if c.fail {
				if err == nil {
					t.Fatal("Fehler erwartet, kam keiner")
				}
				return
			}
			if err != nil {
				t.Fatalf("unerwarteter Fehler: %v", err)
			}
			if len(r.Events) != c.events {
				t.Errorf("Events = %d, erwartet %d", len(r.Events), c.events)
			}
			if r.Promotional != c.promo {
				t.Errorf("Promotional = %v, erwartet %v", r.Promotional, c.promo)
			}
		})
	}
}
