package cmd

import (
	"reflect"
	"sync"
	"testing"
)

// eventsAPI answers events with a deposit and an approval event and
// records the mark_read requests.
type eventsAPI struct {
	mu     sync.Mutex
	marked []map[string]interface{}
}

func (e *eventsAPI) handle(action string, req map[string]interface{}) (int, interface{}) {
	e.mu.Lock()
	defer e.mu.Unlock()
	switch action {
	case "events":
		return apiOK(map[string]interface{}{"events": []interface{}{
			map[string]interface{}{"id": "ev-deposit", "type": "deposit_received"},
			map[string]interface{}{"id": "ev-approval", "type": "approval_resolved"},
		}})
	case "mark_read":
		e.marked = append(e.marked, req)
		return apiOK(map[string]interface{}{"marked_read": 1})
	}
	return apiFail(400, "VALIDATION_ERROR", "unexpected action "+action)
}

func (e *eventsAPI) markRequests() []map[string]interface{} {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]map[string]interface{}(nil), e.marked...)
}

func TestEventsMarkRead(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantIDs []interface{} // nil: marks all
	}{
		{"ids", []string{"--mark-read", "--ids", "ev-1, ev-2"}, []interface{}{"ev-1", "ev-2"}},
		{"all", []string{"--mark-read"}, nil},
		// The server lists every type when it does not know the one asked
		// for, so only listed events of the requested type are marked.
		{"filtered", []string{"--mark-read", "--type", "deposit_received"}, []interface{}{"ev-deposit"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			addTestWallet(t, home, "main")
			server := &eventsAPI{}
			api := newFakeAPI(t, server.handle)

			run := runCLI(t, home, api.URL, nil, append([]string{"events"}, c.args...)...)
			if run.exitCode != 0 {
				t.Fatalf("exit %d\n%s", run.exitCode, run.stdout)
			}
			marked := server.markRequests()
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
		server := &eventsAPI{}
		api := newFakeAPI(t, server.handle)

		run := runCLI(t, home, api.URL, nil, args...)
		if code := run.json(t)["error"]; code != "VALIDATION_ERROR" {
			t.Errorf("%v: error = %v, want VALIDATION_ERROR", args, code)
		}
		if len(server.markRequests()) != 0 {
			t.Errorf("%v: events were marked read", args)
		}
	}
}
