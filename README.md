# TermiQ

Liest ein Postfach, lässt ein Sprachmodell die enthaltenen Termine erkennen und
trägt sie in einen CalDAV-Kalender ein. Zwei Programme, keine Installation,
keine Datenbank, kein Server — alles läuft auf dem Rechner, auf dem es liegt.

```
termiq-config.exe    Einrichtung im Browser: Postfach, Kalender, Modell, Probelauf
termiq-run.exe       ein Durchlauf, ohne Rückfragen — für die Aufgabenplanung
```

## Einrichten

1. `termiq-config.exe` starten. Es öffnet sich der Browser mit der Oberfläche;
   das Programm lauscht ausschließlich auf `127.0.0.1`.
2. **Postfach** eintragen und *Verbindung testen* — danach lassen sich Quell-
   und Archivordner aus der Liste wählen.
3. **Kalender**: CalDAV-Adresse und Zugangsdaten eintragen, *Kalender laden*,
   Zielkalender auswählen.
4. **Sprachmodell**: Adresse, Modellname und API-Key. Jeder OpenAI-kompatible
   Endpunkt funktioniert, auch ein eigener Server im Haus.
5. **Probelauf** starten. Er zeigt, welche Termine entstehen würden, trägt aber
   nichts ein und verschiebt nichts.
6. **Speichern**. Die Einstellungen landen in `termiq.json` neben der EXE.

## Betrieb

```
termiq-run.exe                 einmal durchlaufen
termiq-run.exe -dry-run        nur zeigen, nichts eintragen
termiq-run.exe -quiet          nur Fehler und Ergebnis ausgeben
termiq-run.exe -config C:\Pfad\termiq.json
```

Für den regelmäßigen Lauf in der Windows-Aufgabenplanung eine Aufgabe anlegen,
die `termiq-run.exe` alle 15 Minuten startet. Arbeitsverzeichnis auf den Ordner
der EXE setzen, damit Konfiguration und Journal gefunden werden.

Rückgabewerte: `0` alles gut, `1` Konfigurations- oder Verbindungsfehler,
`2` Durchlauf beendet, aber mit Fehlern bei einzelnen Mails.

## Dateien neben der EXE

| Datei | Inhalt |
|---|---|
| `termiq.json` | Einstellungen samt Zugangsdaten |
| `termiq-journal.json` | verarbeitete Message-IDs |

Das Journal ist der Grund, warum kein Termin doppelt entsteht: Jede Mail wird
über ihre `Message-ID` genau einmal verarbeitet. Wird das Journal gelöscht,
werden bereits eingetragene Termine beim nächsten Lauf erneut angelegt.

## Wie entschieden wird

Eine Mail wird übersprungen, wenn sie bereits im Journal steht, älter als
`max_age_days` ist, oder als Massenmail erkannt wurde. Letzteres passiert
zweistufig: erst über die Kopfzeilen `List-Unsubscribe` und `Precedence`, dann
über die Einschätzung des Modells. Ohne diese Bremse erzeugt jede
„Angebot endet Sonntag"-Mail einen Kalendereintrag.

Termine unterhalb von `min_confidence` werden verworfen, ebenso solche mit
unlesbarem Startdatum. Fehlt eine Endzeit, wird eine Stunde angenommen
(bei ganztägigen Terminen ein Tag).

Der Prompt verbietet dem Modell ausdrücklich, Daten zu erfinden, und trennt
Buchungsdatum von Termindatum. Hin- und Rückreise ergeben zwei Einträge,
mehrtägige Aufenthalte einen von Anreise bis Abreise.

## Einstellungen

Alles ist über die Oberfläche erreichbar; `termiq.json` lässt sich aber auch
direkt bearbeiten.

| Feld | Bedeutung | Standard |
|---|---|---|
| `imap.folder` | Quellordner | `INBOX` |
| `imap.archive_folder` | Ziel nach erfolgreichem Eintrag, leer = liegen lassen | leer |
| `imap.mark_seen` | bearbeitete Mails als gelesen markieren | `false` |
| `caldav.skip_verify` | Zertifikat nicht prüfen, nur für selbstsignierte Server | `false` |
| `behavior.lookback_days` | wie weit zurück gesucht wird | `7` |
| `behavior.unseen_only` | nur ungelesene Mails ansehen | `true` |
| `behavior.max_mails_per_run` | Obergrenze pro Lauf | `10` |
| `behavior.max_age_days` | aus älteren Mails entstehen keine Termine | `14` |
| `behavior.min_confidence` | Schwelle für die Übernahme | `0.6` |
| `behavior.skip_promotional` | Werbung und Newsletter überspringen | `true` |

## Bauen

```
./build.sh              # dist/windows-amd64/*.exe
go test ./...
```

Reines Go, kein cgo — die Windows-Binaries lassen sich von macOS oder Linux aus
erzeugen.

## Was TermiQ nicht tut

- **PDF-Anhänge werden nicht gelesen.** Dateinamen gehen als Hinweis in den
  Prompt, der Inhalt nicht. Flugtickets und Buchungsbestätigungen, bei denen
  das Datum ausschließlich im PDF steht, werden dadurch nicht erkannt.
- **Keine Aktualisierung bestehender Termine.** Eine Umbuchung erzeugt einen
  zweiten Eintrag statt den ersten zu ändern.
- **Keine Wiederholungstermine.**
- **Die Zugangsdaten stehen im Klartext** in `termiq.json`. Die Datei wird mit
  restriktiven Rechten geschrieben, ist aber nicht verschlüsselt. Auf einem
  Mehrbenutzer-Rechner gehört sie in ein geschütztes Profilverzeichnis.
