package cmd

import (
	"reflect"
	"testing"
)

// eventsReply answers events with a deposit and an approval event, and
// mark_read with a count.
func eventsReply(action string, req map[string]interface{}) (int, interface{}) {
	switch action {
	case "events":
		return apiOK(map[string]interface{}{"events": []interface{}{
			map[string]interface{}{"id": "ev-deposit", "type": "deposit_received"},
			map[string]interface{}{"id": "ev-approval", "type": "approval_resolved"},
		}})
	case "mark_read":
		return apiOK(map[string]interface{}{"marked_read": 1})
	}
	return apiFail(400, "VALIDATION_ERROR", "unexpected action "+action)
}

func TestEventsMarkRead(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantIDs []interface{} // nil: marks all
	}{
		{"ids", []string{"--mark-read", "--ids", "ev-1, ev-2"}, []interface{}{"ev-1", "ev-2"}},
		{"all", []string{"--mark-read"}, nil},
		// Older servers dropped type names they did not know and then listed
		// every type (current ones answer VALIDATION_ERROR), so only listed
		// events of the requested type are marked.
		{"filtered", []string{"--mark-read", "--type", "deposit_received"}, []interface{}{"ev-deposit"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			addTestWallet(t, home, "main")
			api := newFakeAPI(t, eventsReply)

			run := runCLI(t, home, api.URL, nil, append([]string{"events"}, c.args...)...)
			if run.exitCode != 0 {
				t.Fatalf("exit %d\n%s", run.exitCode, run.stdout)
			}
			marked := api.requests("mark_read")
			if len(marked) != 1 {
				t.Fatalf("mark_read calls = %d, want 1", len(marked))
			}
			if c.wantIDs == nil {
				if marked[0]["all"] != true {
					t.Errorf("mark_read = %v, want all", marked[0])
				}
				return
			}
			if marked[0]["all"] != nil || !reflect.DeepEqual(marked[0]["event_ids"], c.wantIDs) {
				t.Errorf("mark_read = %v, want event_ids %v", marked[0], c.wantIDs)
			}
		})
	}
}

func TestEventsMarkReadRejectsUnclearFlags(t *testing.T) {
	for _, args := range [][]string{
		{"events", "--ids", "ev-1"},                               // --ids without --mark-read
		{"events", "--mark-read", "--ids", "ev-1", "--type", "x"}, // ids and filters
		{"events", "--mark-read", "--ids", " , "},                 // no ids
	} {
		home := t.TempDir()
		addTestWallet(t, home, "main")
		api := newFakeAPI(t, eventsReply)

		run := runCLI(t, home, api.URL, nil, args...)
		if code := run.json(t)["error"]; code != "VALIDATION_ERROR" {
			t.Errorf("%v: error = %v, want VALIDATION_ERROR", args, code)
		}
		if len(api.requests("mark_read")) != 0 {
			t.Errorf("%v: events were marked read", args)
		}
	}
}
