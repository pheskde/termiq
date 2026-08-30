package mailbox

import (
	"html"
	"regexp"
	"strings"
)

var (
	reScriptStyle = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)
	reBreak       = regexp.MustCompile(`(?i)<(br|/p|/div|/tr|/h[1-6]|/li)[^>]*>`)
	reTag         = regexp.MustCompile(`(?s)<[^>]+>`)
	reBlankLines  = regexp.MustCompile(`\n{3,}`)
	reSpaces      = regexp.MustCompile(`[ \t]{2,}`)
)

// HTMLToText macht aus einer HTML-Mail einen lesbaren Fließtext. Bewusst
// simpel gehalten: das LLM braucht den Inhalt, nicht die Formatierung.
func HTMLToText(in string) string {
	s := reScriptStyle.ReplaceAllString(in, " ")
	s = reBreak.ReplaceAllString(s, "\n")
	s = reTag.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, " ", " ")

	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(reSpaces.ReplaceAllString(l, " "))
	}
	s = strings.Join(lines, "\n")
	s = reBlankLines.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}
