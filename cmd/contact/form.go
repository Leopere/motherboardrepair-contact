// Copyright © 2026 ColinKnapp.com. All rights reserved.

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

const proofBindingVersion = "mrc-form-proof-v1"

var (
	controlChars = regexp.MustCompile(`[\x00-\x1f\x7f]`)
	phoneDigits  = regexp.MustCompile(`\D`)
	emailUnsafe  = regexp.MustCompile(`[\s<>]`)
	phonePattern = regexp.MustCompile(`^\+?[0-9]{7,15}$`)
)

type payload struct {
	Name           string            `json:"name,omitempty"`
	Email          string            `json:"email,omitempty"`
	Phone          string            `json:"phone,omitempty"`
	Company        string            `json:"company,omitempty"`
	Message        string            `json:"message,omitempty"`
	FormID         string            `json:"form_id,omitempty"`
	SlackProfile   string            `json:"slack_profile,omitempty"`
	PreferredLang  string            `json:"preferred_language,omitempty"`
	SiteLang       string            `json:"site_language,omitempty"`
	BrowserLang    string            `json:"browser_language,omitempty"`
	BrowserLangs   string            `json:"browser_languages,omitempty"`
	EnglishSupport string            `json:"english_support_preference,omitempty"`
	ExtraFields    map[string]string `json:"extra_fields"`
	Raw            map[string]any    `json:"-"`
	PreserveRaw    bool              `json:"-"`
	Binding        []string          `json:"-"`
}

func sanitizeURL(value string) string {
	value = truncate(strings.TrimSpace(controlChars.ReplaceAllString(value, "")), 500)
	if strings.ContainsAny(value, "<>") {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return ""
	}
	return value
}

func normalizeLanguageTag(value string) string {
	value = truncate(sanitizeText(value), 35)
	alias := strings.ToLower(strings.ReplaceAll(value, "_", "-"))
	if alias == "qc" || alias == "ca-qc" || alias == "qc fr" || alias == "fr qc" || alias == "qc-fr" || alias == "quebec" || alias == "québec" {
		return "fr-CA"
	}
	parts := strings.Split(strings.ReplaceAll(value, "_", "-"), "-")
	if len(parts) == 0 || len(parts[0]) < 2 || len(parts[0]) > 3 {
		return ""
	}
	parts[0] = strings.ToLower(parts[0])
	for index := 1; index < len(parts); index++ {
		if len(parts[index]) == 2 {
			parts[index] = strings.ToUpper(parts[index])
		} else {
			parts[index] = strings.ToLower(parts[index])
		}
	}
	return strings.Join(parts, "-")
}

func sanitizeText(value string) string {
	return strings.Join(strings.Fields(controlChars.ReplaceAllString(value, "")), " ")
}

func sanitizeMultiline(value string) string {
	value = strings.Map(func(r rune) rune {
		if (unicode.IsControl(r) && r != '\n') || r == '<' || r == '>' {
			return -1
		}
		return r
	}, value)
	return value
}

func sanitizeEmail(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || emailUnsafe.MatchString(value) {
		return ""
	}
	return truncate(value, 254)
}

func sanitizePhone(value string) string {
	value = strings.TrimSpace(value)
	digits := truncate(phoneDigits.ReplaceAllString(value, ""), 15)
	if strings.HasPrefix(value, "+") {
		return "+" + digits
	}
	return digits
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func proofBinding(data payload) []byte {
	values := []string{
		proofBindingVersion,
		payloadString(data, "form_id", "source"),
		payloadString(data, "name", "full_name"),
		payloadString(data, "email"),
		payloadString(data, "phone", "phone_number"),
		payloadString(data, "company"),
		payloadString(data, "message", "notes"),
		payloadString(data, "slack_profile"),
	}
	if len(data.Binding) == len(values) {
		values = data.Binding
	}
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(values)
	result := bytes.TrimSuffix(encoded.Bytes(), []byte("\n"))
	result = bytes.ReplaceAll(result, []byte(`\u2028`), []byte("\u2028"))
	return bytes.ReplaceAll(result, []byte(`\u2029`), []byte("\u2029"))
}

func payloadString(data payload, keys ...string) string {
	for index, key := range keys {
		value, exists := data.Raw[key]
		if !exists {
			continue
		}
		if len(keys) == 1 || index == len(keys)-1 || jsTruthy(value) {
			return jsString(value)
		}
	}
	switch keys[0] {
	case "form_id":
		return data.FormID
	case "name":
		return data.Name
	case "email":
		return data.Email
	case "phone":
		return data.Phone
	case "company":
		return data.Company
	case "message":
		return data.Message
	case "slack_profile":
		return data.SlackProfile
	}
	return ""
}

func jsTruthy(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		return typed != ""
	case float64:
		return typed != 0
	default:
		return true
	}
}

func jsString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case bool:
		return strconv.FormatBool(typed)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case []any:
		parts := make([]string, len(typed))
		for index, item := range typed {
			parts[index] = jsString(item)
		}
		return strings.Join(parts, ",")
	case map[string]any:
		return "[object Object]"
	default:
		return fmt.Sprint(typed)
	}
}

func decodePayloadJSON(encoded []byte) (payload, error) {
	var raw map[string]any
	if err := json.Unmarshal(encoded, &raw); err != nil {
		return payload{}, err
	}
	data := payload{Raw: raw, PreserveRaw: true, ExtraFields: map[string]string{}}
	data.FormID = firstString(raw, "form_id", "source")
	data.Name = firstString(raw, "name", "full_name")
	data.Email = firstString(raw, "email")
	data.Phone = firstString(raw, "phone", "phone_number")
	data.Company = firstString(raw, "company")
	data.Message = firstString(raw, "message", "notes")
	data.SlackProfile = firstString(raw, "slack_profile")
	if extras, ok := raw["extra_fields"].(map[string]any); ok {
		for key, value := range extras {
			if text, ok := value.(string); ok {
				data.ExtraFields[key] = text
			}
		}
	}
	return data, nil
}

func firstString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := raw[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func payloadMap(data payload) map[string]any {
	result := make(map[string]any, len(data.Raw)+12)
	for key, value := range data.Raw {
		result[key] = value
	}
	if data.PreserveRaw {
		return result
	}
	known := map[string]any{
		"name": data.Name, "email": data.Email, "phone": data.Phone,
		"company": data.Company, "message": data.Message, "form_id": data.FormID,
		"slack_profile": data.SlackProfile, "preferred_language": data.PreferredLang,
		"site_language": data.SiteLang, "browser_language": data.BrowserLang,
		"browser_languages":          data.BrowserLangs,
		"english_support_preference": data.EnglishSupport,
	}
	for key, value := range known {
		if value != "" {
			result[key] = value
		}
	}
	if _, preserved := data.Raw["extra_fields"]; !preserved && data.ExtraFields != nil {
		result["extra_fields"] = data.ExtraFields
	}
	return result
}

func validatePayloadFields(data payload, deviceRequired bool) error {
	if data.Name == "" {
		return errors.New("please enter your name")
	}
	if !phonePattern.MatchString(data.Phone) {
		return errors.New("please enter a valid phone number")
	}
	if deviceRequired && strings.TrimSpace(data.ExtraFields["device_name"]) == "" {
		return errors.New("please enter the device name or model")
	}
	if strings.TrimSpace(data.Message) == "" {
		return errors.New("please describe the issue")
	}
	if strings.EqualFold(strings.TrimSpace(data.ExtraFields["service_type"]), "Mail-In") && strings.TrimSpace(data.ExtraFields["mailing_address"]) == "" {
		return errors.New("please select a valid mailing address")
	}
	if data.FormID == "builders_club_quote" && data.SlackProfile == "" {
		return errors.New("please provide your Builders Club Slack profile link")
	}
	if data.FormID == "trios_college_quote" {
		parts := strings.Split(strings.ToLower(data.Email), "@")
		allowed := map[string]bool{"triosstudent.com": true, "easternstudent.ca": true, "trios.com": true, "eastern.ca": true}
		if len(parts) != 2 || !allowed[parts[1]] {
			return errors.New("please use your TriOS or Eastern College school email")
		}
	}
	return nil
}

func payloadDigest(data payload) string {
	digest := sha256.Sum256(proofBinding(data))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func solveProof(challenge, digest string, difficulty, maximum int) (int, error) {
	for counter := 0; counter <= maximum; counter++ {
		sum := sha256.Sum256([]byte(fmt.Sprintf("%s.%s.%d", challenge, digest, counter)))
		if hasLeadingZeroBits(sum[:], difficulty) {
			return counter, nil
		}
	}
	return 0, fmt.Errorf("unable to complete form verification")
}

func hasLeadingZeroBits(value []byte, bits int) bool {
	if bits < 0 || bits > len(value)*8 {
		return false
	}
	wholeBytes := bits / 8
	for index := 0; index < wholeBytes; index++ {
		if value[index] != 0 {
			return false
		}
	}
	remaining := bits % 8
	return remaining == 0 || value[wholeBytes]&(0xff<<uint(8-remaining)) == 0
}
