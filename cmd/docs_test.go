package cmd

import (
	"strings"
	"testing"
)

// Both docs outputs list the event types from one list (eventTypes).
func TestDocsListEveryEventType(t *testing.T) {
	text := runCLI(t, t.TempDir(), "http://127.0.0.1:0", nil, "docs")
	if text.exitCode != 0 || strings.Contains(text.stdout, "{{") {
		t.Fatalf("docs: exit %d\n%s", text.exitCode, text.stdout)
	}

	run := runCLI(t, t.TempDir(), "http://127.0.0.1:0", nil, "docs", "--json")
	out := run.json(t)
	if out["documentation_url"] != "https://docs.botwallet.co/reference/cli/" {
		t.Errorf("documentation_url = %v", out["documentation_url"])
	}
	events, _ := out["events"].(map[string]interface{})
	listed, _ := events["event_types"].([]interface{})
	if len(listed) != len(eventTypes) {
		t.Errorf("docs --json lists %d event types, want %d", len(listed), len(eventTypes))
	}

	for i, e := range eventTypes {
		if !strings.Contains(text.stdout, "      "+e.name+" ") {
			t.Errorf("docs does not list event type %s", e.name)
		}
		if i < len(listed) && !strings.HasPrefix(listed[i].(string), e.name+" — ") {
			t.Errorf("docs --json event_types[%d] = %v, want %s", i, listed[i], e.name)
		}
	}
}
