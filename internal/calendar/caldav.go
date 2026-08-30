// Package calendar schreibt Termine per CalDAV — dieselbe Mechanik wie im
// OmniQ-Backend, nur ohne Datenbank drumherum.
package calendar

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"
)

type Client struct {
	c *caldav.Client
}

type Calendar struct {
	Name string
	Path string
}

type Event struct {
	UID         string
	Title       string
	Start       time.Time
	End         time.Time
	AllDay      bool
	Location    string
	Description string
}

func Connect(rawURL, user, password string, skipVerify bool) (*Client, error) {
	httpc := &http.Client{Timeout: 30 * time.Second}
	if skipVerify {
		httpc.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // bewusste Option für Self-Signed-Server
		}
	}
	authed := webdav.HTTPClientWithBasicAuth(httpc, user, password)
	c, err := caldav.NewClient(authed, rawURL)
	if err != nil {
		return nil, fmt.Errorf("CalDAV-Adresse ungültig: %w", err)
	}
	return &Client{c: c}, nil
}

// Calendars ermittelt alle Kalender des angemeldeten Benutzers.
func (c *Client) Calendars(ctx context.Context) ([]Calendar, error) {
	principal, err := c.c.FindCurrentUserPrincipal(ctx)
	if err != nil {
		return nil, fmt.Errorf("Anmeldung fehlgeschlagen: %w", err)
	}
	homeSet, err := c.c.FindCalendarHomeSet(ctx, principal)
	if err != nil {
		return nil, fmt.Errorf("Kalender-Basis nicht gefunden: %w", err)
	}
	cals, err := c.c.FindCalendars(ctx, homeSet)
	if err != nil {
		return nil, fmt.Errorf("Kalender nicht lesbar: %w", err)
	}
	out := make([]Calendar, 0, len(cals))
	for _, cal := range cals {
		name := cal.Name
		if name == "" {
			name = strings.Trim(cal.Path, "/")
		}
		out = append(out, Calendar{Name: name, Path: cal.Path})
	}
	return out, nil
}

// CreateEvent legt einen Termin an und liefert die UID zurück.
func (c *Client) CreateEvent(ctx context.Context, calendarPath string, ev Event) (string, error) {
	if ev.UID == "" {
		ev.UID = newUID()
	}
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropProductID, "-//TermiQ//Mail nach Kalender//DE")
	cal.Props.SetText(ical.PropVersion, "2.0")

	e := ical.NewEvent()
	e.Props.SetText(ical.PropUID, ev.UID)
	e.Props.SetDateTime(ical.PropDateTimeStamp, time.Now().UTC())
	e.Props.SetText(ical.PropSummary, ev.Title)
	if ev.AllDay {
		e.Props.SetDate(ical.PropDateTimeStart, ev.Start)
		e.Props.SetDate(ical.PropDateTimeEnd, ev.End)
	} else {
		e.Props.SetDateTime(ical.PropDateTimeStart, ev.Start.UTC())
		e.Props.SetDateTime(ical.PropDateTimeEnd, ev.End.UTC())
	}
	if ev.Location != "" {
		e.Props.SetText(ical.PropLocation, ev.Location)
	}
	if ev.Description != "" {
		e.Props.SetText(ical.PropDescription, ev.Description)
	}
	cal.Children = append(cal.Children, e.Component)

	path := strings.TrimRight(calendarPath, "/") + "/" + ev.UID + ".ics"
	if _, err := c.c.PutCalendarObject(ctx, path, cal); err != nil {
		return "", fmt.Errorf("Termin konnte nicht gespeichert werden: %w", err)
	}
	return ev.UID, nil
}

func newUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "termiq-" + hex.EncodeToString(b)
}
