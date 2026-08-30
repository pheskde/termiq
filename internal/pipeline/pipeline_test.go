package pipeline

import (
	"testing"
	"time"
)

func TestParseTime(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		in      string
		wantOK  bool
		wantUTC string // erwartete Zeit in UTC, leer = egal
	}{
		// Mit Zonenangabe: wird übernommen.
		{"2026-09-11T08:30:00+02:00", true, "2026-09-11T06:30:00Z"},
		{"2026-09-11T08:30:00Z", true, "2026-09-11T08:30:00Z"},
		// Ohne Zone: gilt als Ortszeit, NICHT als UTC. Im September ist
		// Berlin UTC+2, 08:30 lokal sind also 06:30 UTC.
		{"2026-09-11T08:30:00", true, "2026-09-11T06:30:00Z"},
		{"2026-09-11T08:30", true, "2026-09-11T06:30:00Z"},
		{"2026-09-11 08:30", true, "2026-09-11T06:30:00Z"},
		// Reines Datum: Mitternacht Ortszeit.
		{"2026-09-11", true, "2026-09-10T22:00:00Z"},
		// Im Winter greift UTC+1.
		{"2026-01-15T08:30:00", true, "2026-01-15T07:30:00Z"},
		// Unbrauchbar.
		{"", false, ""},
		{"nächsten Dienstag", false, ""},
		{"11.09.2026", false, ""},
	}

	for _, c := range cases {
		got, ok := parseTime(c.in, berlin)
		if ok != c.wantOK {
			t.Errorf("parseTime(%q) ok = %v, erwartet %v", c.in, ok, c.wantOK)
			continue
		}
		if !ok || c.wantUTC == "" {
			continue
		}
		if s := got.UTC().Format(time.RFC3339); s != c.wantUTC {
			t.Errorf("parseTime(%q) = %s, erwartet %s", c.in, s, c.wantUTC)
		}
	}
}

func TestTrimKeepsRunesIntact(t *testing.T) {
	// Byte-weises Kürzen würde Umlaute zerschneiden.
	if got := trim("Flug München→Wien", 8); got != "Flug Mün…" {
		t.Errorf("trim = %q", got)
	}
	if got := trim("kurz", 10); got != "kurz" {
		t.Errorf("trim = %q", got)
	}
}
