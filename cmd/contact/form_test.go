// Copyright © 2026 ColinKnapp.com. All rights reserved.

package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestSanitizersPreserveUsefulContactData(t *testing.T) {
	if got := sanitizeText("  Jane\x00   Doe  "); got != "Jane Doe" {
		t.Fatalf("sanitizeText() = %q", got)
	}
	if got := sanitizeMultiline("Board <burn>\nline two\x00"); got != "Board burn\nline two" {
		t.Fatalf("sanitizeMultiline() = %q", got)
	}
	if got := sanitizePhone("+1 (226) 702-0555"); got != "+12267020555" {
		t.Fatalf("sanitizePhone() = %q", got)
	}
	if got := sanitizeEmail("bad <mail@example.ca>"); got != "" {
		t.Fatalf("sanitizeEmail() accepted %q", got)
	}
}

func TestURLAndLanguageSanitizers(t *testing.T) {
	if got := sanitizeURL(" https://example.com/a?x=1&y=2 "); got != "https://example.com/a?x=1&y=2" {
		t.Fatalf("sanitizeURL() = %q", got)
	}
	for _, invalid := range []string{"javascript:alert(1)", "https://example.com/<bad>"} {
		if got := sanitizeURL(invalid); got != "" {
			t.Fatalf("sanitizeURL(%q) = %q", invalid, got)
		}
	}
	if got := normalizeLanguageTag("fr_ca"); got != "fr-CA" {
		t.Fatalf("normalizeLanguageTag() = %q", got)
	}
	if got := normalizeLanguageTag("Québec"); got != "fr-CA" {
		t.Fatalf("normalizeLanguageTag(Québec) = %q", got)
	}
}

func TestProofBindingMatchesJavaScriptContract(t *testing.T) {
	data := payload{Raw: map[string]any{
		"source": "feedback & repair", "full_name": "A < B", "email": "a@example.ca",
		"phone_number": "+12267020555", "company": "Café", "notes": "line\u2028two",
	}}
	want := "[\"mrc-form-proof-v1\",\"feedback & repair\",\"A < B\",\"a@example.ca\",\"+12267020555\",\"Café\",\"line\u2028two\",\"\"]"
	if got := string(proofBinding(data)); got != want {
		t.Fatalf("proofBinding() = %q, want %q", got, want)
	}
}

func TestProofBindingMatchesJavaScriptCoercionAndAliasContract(t *testing.T) {
	tests := []struct {
		raw  map[string]any
		want string
	}{
		{map[string]any{"form_id": float64(123), "message": "numeric"}, `["mrc-form-proof-v1","123","","","","","numeric",""]`},
		{map[string]any{"form_id": false, "source": "fallback", "message": true}, `["mrc-form-proof-v1","fallback","","","","","true",""]`},
		{map[string]any{"form_id": "", "source": "alias", "message": nil, "notes": "aliased notes"}, `["mrc-form-proof-v1","alias","","","","","aliased notes",""]`},
		{map[string]any{"email": false, "company": float64(0)}, `["mrc-form-proof-v1","","","false","","0","",""]`},
	}
	for _, test := range tests {
		if got := string(proofBinding(payload{Raw: test.raw})); got != test.want {
			t.Errorf("proofBinding(%#v) = %q, want %q", test.raw, got, test.want)
		}
	}
}

func TestDecodedPayloadPreservesFeedbackFields(t *testing.T) {
	encoded := []byte(`{"form_id":"motherboard_repair_feedback","message":"Useful","rating":5,"feedback_type":"improvement","review_action":"details","source_url":"https://example.ca/","utm_campaign":"summer","extra_fields":{"page":"feedback"}}`)
	data, err := decodePayloadJSON(encoded)
	if err != nil {
		t.Fatal(err)
	}
	got := payloadMap(data)
	want := map[string]any{}
	if err := json.Unmarshal(encoded, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("payloadMap() = %#v, want %#v", got, want)
	}
}

func TestProofSolverAndBinding(t *testing.T) {
	data := payload{FormID: "motherboard_repair_quote", Name: "Test", Phone: "+12267020555", Message: "Test request"}
	digest := payloadDigest(data)
	counter, err := solveProof("challenge", digest, 13, 10_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if counter < 0 {
		t.Fatal("counter must be non-negative")
	}
}

func TestLeadingZeroBits(t *testing.T) {
	for _, test := range []struct {
		value []byte
		bits  int
		want  bool
	}{
		{[]byte{0x00, 0x0f}, 12, true},
		{[]byte{0x00, 0x10}, 12, false},
		{[]byte{0x7f}, 1, true},
		{[]byte{0x80}, 1, false},
	} {
		if got := hasLeadingZeroBits(test.value, test.bits); got != test.want {
			t.Fatalf("hasLeadingZeroBits(%x, %d) = %v", test.value, test.bits, got)
		}
	}
}
