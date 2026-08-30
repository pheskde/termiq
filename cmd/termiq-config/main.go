// termiq-config öffnet eine kleine Oberfläche im Browser, in der Postfach,
// Kalender und Modell eingerichtet und getestet werden. Der Server lauscht nur
// auf 127.0.0.1 und beendet sich mit dem Fenster bzw. per Strg+C.
package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	iofs "io/fs"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/pheskde/termiq/internal/calendar"
	"github.com/pheskde/termiq/internal/config"
	"github.com/pheskde/termiq/internal/llm"
	"github.com/pheskde/termiq/internal/mailbox"
	"github.com/pheskde/termiq/internal/pipeline"
)

//go:embed ui/*
var uiFS embed.FS

var (
	cfgPath string
	mu      sync.Mutex
)

func main() {
	var (
		addr   = flag.String("addr", "127.0.0.1:0", "Adresse der Oberfläche")
		path   = flag.String("config", "", "Pfad zur termiq.json")
		noOpen = flag.Bool("no-open", false, "Browser nicht automatisch öffnen")
	)
	flag.Parse()

	cfgPath = *path
	if cfgPath == "" {
		cfgPath = config.Path()
	}

	ui, err := iofs.Sub(uiFS, "ui")
	if err != nil {
		fmt.Println("Fehler: Oberfläche fehlt im Programm:", err)
		return
	}

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(ui)))
	mux.HandleFunc("/api/config", handleConfig)
	mux.HandleFunc("/api/test/imap", handleTestIMAP)
	mux.HandleFunc("/api/test/caldav", handleTestCalDAV)
	mux.HandleFunc("/api/test/llm", handleTestLLM)
	mux.HandleFunc("/api/dryrun", handleDryRun)

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Println("Fehler: Oberfläche konnte nicht gestartet werden:", err)
		return
	}
	url := "http://" + ln.Addr().String() + "/"
	fmt.Println("TermiQ Einrichtung läuft auf", url)
	fmt.Println("Konfigurationsdatei:", cfgPath)
	fmt.Println("Zum Beenden dieses Fenster schließen oder Strg+C drücken.")
	if !*noOpen {
		openBrowser(url)
	}
	_ = http.Serve(ln, mux)
}

func handleConfig(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()

	switch r.Method {
	case http.MethodGet:
		cfg, err := config.LoadFrom(cfgPath)
		if err != nil {
			cfg = config.Default()
		}
		writeJSON(w, map[string]any{"config": cfg, "path": cfgPath})
	case http.MethodPost:
		var cfg config.Config
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeErr(w, err)
			return
		}
		if err := config.SaveTo(cfgPath, &cfg); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "path": cfgPath})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func handleTestIMAP(w http.ResponseWriter, r *http.Request) {
	var c config.IMAP
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		writeErr(w, err)
		return
	}
	if c.Port == 0 {
		c.Port = 993
	}
	mb, err := mailbox.Dial(c.Host, c.Port, c.User, c.Password)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer mb.Close()
	folders, err := mb.Folders()
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "folders": folders})
}

func handleTestCalDAV(w http.ResponseWriter, r *http.Request) {
	var c config.CalDAV
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		writeErr(w, err)
		return
	}
	cl, err := calendar.Connect(c.URL, c.User, c.Password, c.SkipVerify)
	if err != nil {
		writeErr(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	cals, err := cl.Calendars(ctx)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "calendars": cals})
}

func handleTestLLM(w http.ResponseWriter, r *http.Request) {
	var c config.LLM
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		writeErr(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	if err := llm.New(c.BaseURL, c.APIKey, c.Model).Ping(ctx); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func handleDryRun(w http.ResponseWriter, r *http.Request) {
	var cfg config.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeErr(w, err)
		return
	}
	var lines []string
	var lmu sync.Mutex
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()

	sum, err := pipeline.Run(ctx, &cfg, pipeline.Options{
		DryRun:     true,
		LedgerPath: filepath.Join(filepath.Dir(cfgPath), "termiq-journal.json"),
		Logf: func(format string, args ...any) {
			lmu.Lock()
			lines = append(lines, fmt.Sprintf(format, args...))
			lmu.Unlock()
		},
	})
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error(), "log": lines})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "log": lines, "summary": sum})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
}

func openBrowser(url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "windows":
		cmd, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		cmd, args = "open", []string{url}
	default:
		cmd, args = "xdg-open", []string{url}
	}
	if err := exec.Command(cmd, args...).Start(); err != nil {
		_ = strings.TrimSpace(url)
	}
}
