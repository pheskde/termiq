// Package config lädt und speichert die Einstellungen. Die Datei liegt
// standardmäßig neben der EXE, damit TermiQ von einem USB-Stick oder aus
// einem beliebigen Ordner laufen kann, ohne etwas zu installieren.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const FileName = "termiq.json"

type IMAP struct {
	Host          string `json:"host"`
	Port          int    `json:"port"`
	User          string `json:"user"`
	Password      string `json:"password"`
	Folder        string `json:"folder"`
	ArchiveFolder string `json:"archive_folder"` // leer = Mail bleibt liegen
	MarkSeen      bool   `json:"mark_seen"`
}

type CalDAV struct {
	URL          string `json:"url"`
	User         string `json:"user"`
	Password     string `json:"password"`
	CalendarPath string `json:"calendar_path"`
	CalendarName string `json:"calendar_name"`
	SkipVerify   bool   `json:"skip_verify"`
}

type LLM struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	Model   string `json:"model"`
}

type Behavior struct {
	Timezone        string  `json:"timezone"`
	MaxMailsPerRun  int     `json:"max_mails_per_run"`
	MaxAgeDays      int     `json:"max_age_days"`
	MinConfidence   float64 `json:"min_confidence"`
	SkipPromotional bool    `json:"skip_promotional"`
	LookbackDays    int     `json:"lookback_days"`
	UnseenOnly      bool    `json:"unseen_only"`
}

type Config struct {
	IMAP     IMAP     `json:"imap"`
	CalDAV   CalDAV   `json:"caldav"`
	LLM      LLM      `json:"llm"`
	Behavior Behavior `json:"behavior"`
}

func Default() *Config {
	return &Config{
		IMAP: IMAP{Port: 993, Folder: "INBOX"},
		LLM:  LLM{BaseURL: "https://api.openai.com/v1", Model: "gpt-4o-mini"},
		Behavior: Behavior{
			Timezone:        "Europe/Berlin",
			MaxMailsPerRun:  10,
			MaxAgeDays:      14,
			MinConfidence:   0.6,
			SkipPromotional: true,
			LookbackDays:    7,
			UnseenOnly:      true,
		},
	}
}

// Path liefert den Pfad der Konfigurationsdatei: TERMIQ_CONFIG, sonst neben der EXE.
func Path() string {
	if p := os.Getenv("TERMIQ_CONFIG"); p != "" {
		return p
	}
	exe, err := os.Executable()
	if err != nil {
		return FileName
	}
	return filepath.Join(filepath.Dir(exe), FileName)
}

func Load() (*Config, error) {
	return LoadFrom(Path())
}

func LoadFrom(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("keine Konfiguration unter %s — bitte zuerst termiq-config starten", path)
		}
		return nil, err
	}
	cfg := Default()
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("%s ist beschädigt: %w", path, err)
	}
	return cfg, nil
}

func Save(cfg *Config) error {
	return SaveTo(Path(), cfg)
}

func SaveTo(path string, cfg *Config) error {
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	// 0600: die Datei enthält Klartext-Zugangsdaten.
	return os.WriteFile(path, raw, 0o600)
}

// Validate prüft, ob genug für einen Lauf konfiguriert ist.
func (c *Config) Validate() error {
	var missing []string
	if c.IMAP.Host == "" {
		missing = append(missing, "IMAP-Server")
	}
	if c.IMAP.User == "" || c.IMAP.Password == "" {
		missing = append(missing, "IMAP-Zugangsdaten")
	}
	if c.CalDAV.URL == "" {
		missing = append(missing, "CalDAV-Adresse")
	}
	if c.CalDAV.CalendarPath == "" {
		missing = append(missing, "Zielkalender")
	}
	if c.LLM.APIKey == "" {
		missing = append(missing, "API-Key")
	}
	if len(missing) > 0 {
		return fmt.Errorf("unvollständige Konfiguration: %v", missing)
	}
	return nil
}
