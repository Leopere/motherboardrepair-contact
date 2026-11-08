// Copyright © 2026 ColinKnapp.com. All rights reserved.

package main

import "errors"

// failure contains only bounded diagnostics, never response text or payload data.
// The consumer forwards only component/form_id/stage/status/code to Notomo.
// code is a finite class, optionally followed by an allowlisted relay code.
type failure struct {
	class  string
	status int
	code   string
}

func (f *failure) Error() string { return "contact form submission failed" }

func allowlistedRelayCode(code string) string {
	switch code {
	case "FORM_PROOF_INVALID", "FORM_PROOF_UNAVAILABLE":
		return code
	default:
		return ""
	}
}

func failureDetails(formID string, err error) map[string]any {
	class, status, code := "unknown", 0, ""
	var f *failure
	if errors.As(err, &f) {
		switch f.class {
		case "proof_network", "proof_timeout", "proof_response_read", "proof_response_json", "proof_http", "proof_challenge", "proof_compute",
			"submit_network", "submit_timeout", "submit_response_read", "submit_response_json", "submit_http", "submit_rejected":
			class = f.class
		}
		if f.status >= 100 && f.status <= 599 {
			status = f.status
		}
		code = allowlistedRelayCode(f.code)
	}
	if code != "" {
		class += ":" + code
	}
	// Form IDs are source identifiers, not free-form telemetry.
	switch formID {
	case "motherboard_repair_quote", "motherboard_repair_business", "motherboard_repair_careers_inquiry", "ads_landing_quote", "builders_club_quote", "trios_college_quote",
		"motherboard_repair_feedback", "motherboard_repair_positive_feedback", "graphics_card_repair_quote":
	default:
		formID = "unknown"
	}
	return map[string]any{
		"component": "contact_form", "form_id": formID, "stage": "submit",
		"status": status, "code": class,
	}
}
