package output

// =============================================================================
// Utility Formatters
// =============================================================================

// FormatPing formats the API's answer to ping.
func FormatPing(data map[string]interface{}) {
	if !humanOutput {
		JSON(data)
		return
	}

	SuccessMsg("API is reachable!")
	KeyValue("Version", data["version"])
	KeyValue("Timestamp", data["timestamp"])
}
