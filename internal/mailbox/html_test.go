package mailbox

import "strings"

import "testing"

func TestHTMLToText(t *testing.T) {
	in := `<html><head><style>.x{color:red}</style><script>var a=1;</script></head>
	<body><p>Ihr Flug&nbsp;FRA&rarr;LHR</p><div>am 11.09.2026</div>
	<table><tr><td>Abflug</td><td>08:30</td></tr></table></body></html>`

	got := HTMLToText(in)

	for _, want := range []string{"Ihr Flug FRA→LHR", "am 11.09.2026", "Abflug", "08:30"} {
		if !strings.Contains(got, want) {
			t.Errorf("Text enthält %q nicht:\n%s", want, got)
		}
	}
	// Script- und Style-Inhalte dürfen nicht im Prompt landen.
	for _, bad := range []string{"color:red", "var a=1"} {
		if strings.Contains(got, bad) {
			t.Errorf("Text enthält unerwartet %q:\n%s", bad, got)
		}
	}
	if strings.Contains(got, "<") {
		t.Errorf("Tags nicht entfernt:\n%s", got)
	}
}
