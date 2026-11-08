package main

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestFailureDetailsPrivacyBoundary(t *testing.T) {
	const private = "private body token email message"
	for _, tt := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"network", &failure{class: "submit_network"}, 0, "submit_network"},
		{"proof", fmt.Errorf("ignored wrapper %s: %w", private, &failure{class: "submit_http", status: 403, code: "FORM_PROOF_INVALID"}), 403, "submit_http:FORM_PROOF_INVALID"},
		{"unavailable", &failure{class: "submit_http", status: 503, code: "FORM_PROOF_UNAVAILABLE"}, 503, "submit_http:FORM_PROOF_UNAVAILABLE"},
		{"unknown code", &failure{class: "submit_http", status: 400, code: private}, 400, "submit_http"},
		{"unknown class", &failure{class: private, status: 999, code: private}, 0, "unknown"},
		{"raw error", errors.New(private), 0, "unknown"},
		{"nil error", nil, 0, "unknown"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := failureDetails("motherboard_repair_quote", tt.err)
			want := map[string]any{"component": "contact_form", "form_id": "motherboard_repair_quote", "stage": "submit", "status": tt.status, "code": tt.code}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("unexpected metadata: %#v", got)
			}
			if strings.Contains(fmt.Sprint(got), private) {
				t.Fatal("private data escaped")
			}
		})
	}
	if got := failureDetails(private, errors.New(private)); got["form_id"] != "unknown" {
		t.Fatal("unbounded form ID escaped")
	}
}
