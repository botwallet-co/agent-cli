package output

// =============================================================================
// Safe Data Extraction Helpers
// =============================================================================
// Production code managing money must NEVER panic on missing/nil fields.
// These helpers safely extract values from API response maps. With several
// keys they return the first that holds a value of the right type (e.g.
// amount_usdc, then the older amount).

// getString safely extracts a string from a map, returning "" if missing/nil.
func getString(data map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if v, ok := data[key].(string); ok {
			return v
		}
	}
	return ""
}

// getFloat safely extracts a float64 from a map, returning 0 if missing/nil.
func getFloat(data map[string]interface{}, keys ...string) float64 {
	for _, key := range keys {
		if v, ok := data[key].(float64); ok {
			return v
		}
	}
	return 0
}

// getNonZeroFloat returns the first of keys that holds a non-zero number,
// or 0. Unlike getFloat it skips a zero amount_usdc in favor of amount.
func getNonZeroFloat(data map[string]interface{}, keys ...string) float64 {
	for _, key := range keys {
		if v, ok := data[key].(float64); ok && v != 0 {
			return v
		}
	}
	return 0
}

// getBool safely extracts a bool from a map, returning false if missing/nil.
func getBool(data map[string]interface{}, keys ...string) bool {
	for _, key := range keys {
		if v, ok := data[key].(bool); ok {
			return v
		}
	}
	return false
}

// getSlice safely extracts a []interface{} from a map, returning empty slice if missing/nil.
func getSlice(data map[string]interface{}, keys ...string) []interface{} {
	for _, key := range keys {
		if v, ok := data[key].([]interface{}); ok {
			return v
		}
	}
	return []interface{}{}
}

// getMap safely extracts a nested map from a map, returning nil if missing.
func getMap(data map[string]interface{}, key string) map[string]interface{} {
	if v, ok := data[key].(map[string]interface{}); ok {
		return v
	}
	return nil
}

// getInt safely extracts an integer (from float64 JSON number), returning 0 if missing.
func getInt(data map[string]interface{}, keys ...string) int {
	for _, key := range keys {
		if v, ok := data[key].(float64); ok {
			return int(v)
		}
	}
	return 0
}
