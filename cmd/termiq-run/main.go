// termiq-run holt Mails, lässt Termine extrahieren und trägt sie in den
// Kalender ein. Ohne Rückfragen — gedacht für die Aufgabenplanung.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pheskde/termiq/internal/config"
	"github.com/pheskde/termiq/internal/pipeline"
)

var version = "dev"

func main() {
	var (
		dryRun  = flag.Bool("dry-run", false, "nichts eintragen, nur zeigen was passieren würde")
		cfgPath = flag.String("config", "", "Pfad zur termiq.json (Standard: neben der EXE)")
		quiet   = flag.Bool("quiet", false, "nur Fehler und Ergebnis ausgeben")
		showVer = flag.Bool("version", false, "Version ausgeben")
	)
	flag.Parse()

	if *showVer {
		fmt.Println("termiq-run", version)
		return
	}

	path := *cfgPath
	if path == "" {
		path = config.Path()
	}
	cfg, err := config.LoadFrom(path)
	if err != nil {
		fatal(err)
	}

	logf := func(format string, args ...any) {
		if !*quiet {
			fmt.Printf("%s  %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
		}
	}

	if *dryRun {
		logf("Probelauf — es wird nichts eingetragen und nichts verschoben")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	sum, err := pipeline.Run(ctx, cfg, pipeline.Options{
		DryRun:     *dryRun,
		LedgerPath: filepath.Join(filepath.Dir(path), "termiq-journal.json"),
		Logf:       logf,
	})
	if err != nil {
		fatal(err)
	}

	fmt.Printf("\nGeprüft: %d   Termine: %d\n", sum.Examined, sum.EventsMade)
	if sum.SkippedSeen+sum.SkippedOld+sum.SkippedPromo+sum.SkippedLowCf > 0 {
		fmt.Printf("Übersprungen — bereits bearbeitet: %d, zu alt: %d, Werbung: %d, unsichere Erkennung: %d\n",
			sum.SkippedSeen, sum.SkippedOld, sum.SkippedPromo, sum.SkippedLowCf)
	}
	if len(sum.Errors) > 0 {
		fmt.Printf("\n%d Fehler:\n", len(sum.Errors))
		for _, e := range sum.Errors {
			fmt.Println("  -", e)
		}
		os.Exit(2)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "Fehler:", err)
	os.Exit(1)
}
