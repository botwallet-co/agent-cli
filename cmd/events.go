package cmd

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/botwallet-co/agent-cli/api"
	"github.com/botwallet-co/agent-cli/output"
)

var (
	eventsType     string
	eventsLimit    int
	eventsSince    string
	eventsAll      bool // include read events
	eventsMarkRead bool
	eventsIDs      string // with --mark-read: only these events
)

var eventsCmd = &cobra.Command{
	Use:     "events",
	Aliases: []string{"notifications"},
	Short:   "Check wallet notifications and events",
	Long: `Check for notifications about your wallet.

Events are generated automatically when things happen:
- Human approves or rejects a pending payment/withdrawal
- Funds are deposited to your wallet
- A payment you made completes on-chain
- Fund request is funded or dismissed
- Guard rails are updated by your owner

By default, shows only unread events (max 10). Use --all to include read events.

IMPORTANT: After acting on events, mark the ones you handled as read so you
don't see them again: 'botwallet events --mark-read --ids <id>,<id>' with the
ids from the listing. With --type, --since or --limit, --mark-read marks the
unread events those filters list. Plain --mark-read marks every unread event,
including any that arrived after you last listed them.`,
	Example: `  botwallet events                                    # Unread events (default)
  botwallet events --type approval_resolved            # Only approval updates
  botwallet events --all --limit 25                    # All recent events
  botwallet events --mark-read --ids <id>,<id>         # Mark the events you handled
  botwallet events --mark-read --type deposit_received # Mark unread deposits
  botwallet events --mark-read                         # Mark every unread event
  botwallet events --type deposit_received,payment_completed`,
	Run: func(cmd *cobra.Command, args []string) {
		if eventsIDs != "" && !eventsMarkRead {
			output.ValidationError("--ids is used with --mark-read",
				"Example: botwallet events --mark-read --ids <id>,<id>")
			return
		}

		if !requireAPIKey() {
			return
		}

		client := getClient()
		types := splitList(eventsType)

		if eventsMarkRead {
			markEventsRead(cmd, client, types)
			return
		}

		result, err := client.Events(types, eventsLimit, !eventsAll, eventsSince)
		if err != nil {
			handleAPIError(err)
			return
		}

		output.FormatEvents(result)
	},
}

// markEventsRead marks the events --ids names, or the unread events the
// list filters select, or (with neither) every unread event.
func markEventsRead(cmd *cobra.Command, client *api.Client, types []string) {
	filtered := cmd.Flags().Changed("type") || cmd.Flags().Changed("since") || cmd.Flags().Changed("limit")

	ids := splitList(eventsIDs)
	switch {
	case cmd.Flags().Changed("ids") && len(ids) == 0:
		output.ValidationError("--ids is empty", "Pass the event ids to mark, e.g. --ids <id>,<id>")
		return
	case len(ids) > 0 && filtered:
		output.ValidationError("Use either --ids or --type/--since/--limit with --mark-read",
			"To mark the events you handled, pass only --mark-read --ids <id>,<id>")
		return
	case len(ids) == 0 && filtered:
		// Mark what 'events' with the same filters lists, nothing else.
		listed, err := client.Events(types, eventsLimit, true, eventsSince)
		if err != nil {
			handleAPIError(err)
			return
		}
		ids = listedEventIDs(listed, types)
		if len(ids) == 0 {
			output.FormatMarkRead(map[string]interface{}{"marked_read": 0})
			return
		}
	}

	result, err := client.MarkRead(ids, len(ids) == 0)
	if err != nil {
		handleAPIError(err)
		return
	}
	output.FormatMarkRead(result)
}

// listedEventIDs returns the ids of the events in an events response. The
// server drops type names it does not know and then lists every type, so
// with types given only events of those types count.
func listedEventIDs(listed map[string]interface{}, types []string) []string {
	var ids []string
	events, _ := listed["events"].([]interface{})
	for _, e := range events {
		evt, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		id, _ := evt["id"].(string)
		eventType, _ := evt["type"].(string)
		if id == "" || (len(types) > 0 && !containsString(types, eventType)) {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

// splitList splits a comma-separated flag value, dropping empty items.
func splitList(s string) []string {
	var items []string
	for _, item := range strings.Split(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}

func containsString(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

func init() {
	eventsCmd.Flags().StringVar(&eventsType, "type", "", "Filter by event type (comma-separated: approval_resolved, deposit_received, payment_completed, fund_requested, etc.)")
	eventsCmd.Flags().IntVar(&eventsLimit, "limit", 10, "Maximum events to return (max 25)")
	eventsCmd.Flags().StringVar(&eventsSince, "since", "", "Only events after this ISO timestamp")
	eventsCmd.Flags().BoolVar(&eventsAll, "all", false, "Include already-read events")
	eventsCmd.Flags().BoolVar(&eventsMarkRead, "mark-read", false, "Mark events as read: those in --ids, else those the filters list, else every unread event")
	eventsCmd.Flags().StringVar(&eventsIDs, "ids", "", "With --mark-read: comma-separated ids of the events to mark as read")
}
