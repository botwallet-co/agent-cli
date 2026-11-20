package output

import "fmt"

// =============================================================================
// Agent-Facing JSON Fields
// =============================================================================
// What the JSON output adds to the API result for agents: next steps,
// ready_to_confirm, agent_hint, action_required and send_to_email.
// SKILL.md and 'botwallet docs' tell agents to follow these fields, so
// their names and meaning are part of the CLI's contract. Each function
// returns a new map; the API result is left as it was.

// registerNextStep is what the agent should tell its human right after
// registering. Keep it calm and factual: claim codes expire 48 hours after
// registration, and the owner controls spending once they claim.
const registerNextStep = "Share the claim_url and claim_code with your human now and tell them the code expires in 48 hours. " +
	"Explain in plain words: this is a spending account they will own; after claiming they can set how much you may spend without asking, add money, and withdraw any time. " +
	"Don't make payments until they've claimed it and set an allowance."

// registerOnClaimed is what the agent may do once the human has claimed the
// wallet. A sample payment link is only an offer, never a request for money.
const registerOnClaimed = "When your human confirms they've claimed the wallet, thank them. " +
	"You can offer to show what you can do, for example by creating a small sample payment link with 'botwallet paylink create' that they can try, " +
	"but never pressure them to pay you."

// approvalAgentHint is the agent_hint for anything waiting on the owner's
// approval. Approving never executes anything by itself: the agent has to
// come back and run confirm_command (which co-signs) once the owner approves.
// what names the action, e.g. "payment" or "withdrawal".
func approvalAgentHint(what string, data map[string]interface{}) string {
	checkCmd := "botwallet approval status <approval_id>"
	if approvalID := getString(data, "approval_id"); approvalID != "" {
		checkCmd = "botwallet approval status " + approvalID
	}
	return fmt.Sprintf("Send the approval_url to your human owner and tell them that approving does not send the %s by itself: "+
		"once they approve, you must come back and run confirm_command to complete it. "+
		"Save approval_id to persistent memory and check '%s' periodically until it is approved. "+
		"Approvals expire after 24 hours.", what, checkCmd)
}

// withAgentFields returns a copy of data to add agent fields to.
func withAgentFields(data map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(data)+2)
	for k, v := range data {
		out[k] = v
	}
	return out
}

// registerForAgent is the JSON output of a new wallet: only what matters
// now, with the next step for the agent's human.
func registerForAgent(data map[string]interface{}) map[string]interface{} {
	clean := map[string]interface{}{
		"success":     true,
		"wallet_name": data["username"],
		"wallet_id":   data["wallet_id"],
		"status":      "unclaimed",
		"claim_url":   data["claim_url"],
		"claim_code":  data["claim_code"],
		"pledged_to":  data["pledged_to"],
		"next_step": map[string]interface{}{
			"action":     registerNextStep,
			"claim_url":  data["claim_url"],
			"claim_code": data["claim_code"],
		},
		"on_claimed": registerOnClaimed,
	}

	if claimExpiresAt := getString(data, "claim_expires_at"); claimExpiresAt != "" {
		clean["claim_expires_at"] = claimExpiresAt
	}

	if previousDefault := getString(data, "previous_default"); previousDefault != "" {
		clean["note"] = fmt.Sprintf("This wallet is now your default. Previous default was '%s'. Use 'botwallet wallet list' to see all wallets.", previousDefault)
	}
	return clean
}

// infoForAgent adds action_required to the wallet info of an unclaimed
// wallet: most operations are blocked until a human claims it.
func infoForAgent(data map[string]interface{}) map[string]interface{} {
	out := withAgentFields(data)
	if status, ok := data["status"].(string); ok && status == "unclaimed" {
		out["action_required"] = map[string]interface{}{
			"status":             "WALLET_BLOCKED",
			"reason":             "Wallet is unclaimed - most operations are blocked until a human claims it",
			"what_to_tell_human": fmt.Sprintf("Please claim my wallet on Botwallet at %s using code %s", data["claim_url"], data["claim_code"]),
			"claim_url":          data["claim_url"],
			"claim_code":         data["claim_code"],
			"expires_at":         data["claim_expires_at"],
		}
	}
	return out
}

// initiatedForAgent adds ready_to_confirm, and agent_hint while the owner
// has to approve, to a payment intent (pay, x402 fetch). what names it in
// the hint, e.g. "payment".
func initiatedForAgent(what string, data map[string]interface{}) map[string]interface{} {
	out := withAgentFields(data)
	switch getString(data, "status") {
	case "pre_approved":
		out["ready_to_confirm"] = true
	case "awaiting_approval":
		out["ready_to_confirm"] = false
		out["agent_hint"] = approvalAgentHint(what, data)
	case "rejected", "blocked":
		out["ready_to_confirm"] = false
	}
	return out
}

// withdrawalForAgent adds ready_to_confirm (always false: the owner has to
// approve every withdrawal) and the approval agent_hint.
func withdrawalForAgent(data map[string]interface{}) map[string]interface{} {
	out := withAgentFields(data)
	out["ready_to_confirm"] = false
	if getString(data, "approval_id") != "" {
		out["agent_hint"] = approvalAgentHint("withdrawal", data)
	}
	return out
}

// paymentRequestForAgent adds send_to_email, the command that sends a new
// paylink as an invoice.
func paymentRequestForAgent(data map[string]interface{}) map[string]interface{} {
	out := withAgentFields(data)
	if requestID := getString(data, "request_id"); requestID != "" {
		out["send_to_email"] = fmt.Sprintf("To email as invoice: botwallet paylink send %s --to recipient@example.com", requestID)
	}
	return out
}

// renamedForAgent adds a note that the username did not change.
func renamedForAgent(data map[string]interface{}) map[string]interface{} {
	out := withAgentFields(data)
	out["note"] = fmt.Sprintf("Display name updated. Your username @%s is unchanged.", data["username"])
	return out
}
