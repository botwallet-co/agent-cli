package cmd

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/botwallet-co/agent-cli/output"
)

var (
	eventsType     string
	eventsLimit    int
	eventsSince    string
	eventsAll      bool // include read events
	eventsMarkRead bool
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

IMPORTANT: After acting on events, use 'botwallet events --mark-read' to acknowledge them
so you don't see them again.`,
	Example: `  botwallet events                                    # Unread events (default)
  botwallet events --type approval_resolved            # Only approval updates
  botwallet events --all --limit 25                    # All recent events
  botwallet events --mark-read                         # Mark all as read
  botwallet events --type deposit_received,payment_completed`,
	Run: func(cmd *cobra.Command, args []string) {
		if !requireAPIKey() {
			return
		}

		client := getClient()

		if eventsMarkRead {
			result, err := client.MarkRead(nil, true)
			if err != nil {
				handleAPIError(err)
				return
			}
			output.FormatMarkRead(result)
			return
		}

		var types []string
		if eventsType != "" {
			types = strings.Split(eventsType, ",")
			for i := range types {
				types[i] = strings.TrimSpace(types[i])
			}
		}

		result, err := client.Events(types, eventsLimit, !eventsAll, eventsSince)
		if err != nil {
			handleAPIError(err)
			return
		}

		output.FormatEvents(result)
	},
}

func init() {
	eventsCmd.Flags().StringVar(&eventsType, "type", "", "Filter by event type (comma-separated: approval_resolved, deposit_received, payment_completed, fund_requested, etc.)")
	eventsCmd.Flags().IntVar(&eventsLimit, "limit", 10, "Maximum events to return (max 25)")
	eventsCmd.Flags().StringVar(&eventsSince, "since", "", "Only events after this ISO timestamp")
	eventsCmd.Flags().BoolVar(&eventsAll, "all", false, "Include already-read events")
	eventsCmd.Flags().BoolVar(&eventsMarkRead, "mark-read", false, "Mark all unread events as read")
}
