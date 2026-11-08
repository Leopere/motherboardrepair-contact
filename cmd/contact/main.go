//go:build js && wasm

// Copyright © 2026 ColinKnapp.com. All rights reserved.

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"syscall/js"
	"time"
)

const (
	proofURL       = "https://forms.motherboardrepair.ca/api/form-proof"
	submitURL      = "https://forms.motherboardrepair.ca/api/submit"
	requestTimeout = 20 * time.Second
	maxCounter     = 10_000_000
)

var (
	challengeLock sync.Mutex
	cachedProof   proofChallenge
	heldFuncs     []js.Func
)

type proofChallenge struct {
	Challenge  string `json:"challenge"`
	Difficulty int    `json:"difficulty"`
	ReadyAt    int64  `json:"ready_at"`
	ExpiresAt  int64  `json:"expires_at"`
}

type responseData struct {
	OK      bool   `json:"ok"`
	Success bool   `json:"success"`
	Code    string `json:"code"`
	Error   string `json:"error"`
	Message string `json:"message"`
}

type uiMessages struct {
	SubmittingButton string
	Submitting       string
	Success          string
	RelayError       string
	Consent          string
	Invalid          string
}

var localizedMessages = map[string]uiMessages{
	"en": {"Submitting...", "Submitting your request...", "Thank you! Your request has been submitted. We'll contact you shortly.", "Sorry, there was an issue submitting your request. Please try again or leave a voicemail at 226-702-0555.", "Please confirm you agree to the Privacy Policy, Terms of Service, and How It Works before submitting.", "Please check the required fields and try again."},
	"fr": {"Envoi...", "Soumission de votre demande...", "Merci! Votre demande a été soumise. Nous vous contacterons sous peu.", "Désolé, un problème est survenu lors de l'envoi de votre demande. Veuillez réessayer ou laisser un message vocal au 226-702-0555.", "Veuillez accepter la politique de confidentialité, les conditions d’utilisation et la section Fonctionnement avant de soumettre.", "Veuillez vérifier les champs obligatoires et réessayer."},
	"es": {"Enviando...", "Enviando su solicitud...", "¡Gracias! Su solicitud ha sido enviada. Nos comunicaremos contigo en breve.", "Lo sentimos, hubo un problema al enviar su solicitud. Vuelva a intentarlo o deje un mensaje de voz en 226-702-0555.", "Confirme que acepta la Política de privacidad, los Términos de servicio y Cómo funciona antes de enviar.", "Revise los campos obligatorios y vuelva a intentarlo."},
	"vi": {"Đang gửi...", "Đang gửi yêu cầu của bạn...", "Cảm ơn bạn! Yêu cầu của bạn đã được gửi. Chúng tôi sẽ sớm liên hệ với bạn.", "Rất tiếc, đã xảy ra sự cố khi gửi yêu cầu. Vui lòng thử lại hoặc để lại thư thoại theo số 226-702-0555.", "Vui lòng xác nhận rằng bạn đồng ý với Chính sách quyền riêng tư, Điều khoản dịch vụ và Cách thức hoạt động trước khi gửi.", "Vui lòng kiểm tra các trường bắt buộc và thử lại."},
	"ar": {"جارٍ الإرسال...", "جارٍ إرسال طلبك...", "شكرا لك! تم إرسال طلبك. سنتصل بك قريبًا.", "عذرًا، حدثت مشكلة أثناء إرسال طلبك. حاول مرة أخرى أو اترك رسالة صوتية على 226-702-0555.", "يرجى تأكيد موافقتك على سياسة الخصوصية وشروط الخدمة وكيفية العمل قبل الإرسال.", "يرجى التحقق من الحقول المطلوبة والمحاولة مرة أخرى."},
	"ja": {"送信中...", "リクエストを送信しています...", "ありがとうございます！リクエストは送信されました。すぐにご連絡いたします。", "リクエストの送信中に問題が発生しました。もう一度試すか、226-702-0555 にボイスメールを残してください。", "送信する前に、プライバシーポリシー、利用規約、仕組みに同意してください。", "必須項目を確認して、もう一度お試しください。"},
}

func main() {
	api := map[string]any{
		"init": initFunction(),
		"prepareProtectedSubmission": promiseFunction(func(_ []js.Value) (any, error) {
			challenge, err := getChallenge(false)
			return challenge, err
		}),
		"submitProtectedPayload": promiseFunction(func(args []js.Value) (any, error) {
			if len(args) == 0 {
				return nil, errors.New("missing form payload")
			}
			data, err := decodePayload(args[0])
			if err != nil {
				return nil, err
			}
			return submit(data)
		}),
	}
	js.Global().Set("MRCContactWASM", js.ValueOf(api))
	select {}
}

func initFunction() js.Func {
	fn := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 || args[0].IsNull() || args[0].IsUndefined() {
			return nil
		}
		initializeForm(args[0])
		return nil
	})
	heldFuncs = append(heldFuncs, fn)
	return fn
}

func promiseFunction(work func([]js.Value) (any, error)) js.Func {
	fn := js.FuncOf(func(_ js.Value, args []js.Value) any {
		copied := append([]js.Value(nil), args...)
		executor := js.FuncOf(func(_ js.Value, callbacks []js.Value) any {
			resolve, reject := callbacks[0], callbacks[1]
			go func() {
				value, err := work(copied)
				if err != nil {
					reject.Invoke(js.Global().Get("Error").New(err.Error()))
					return
				}
				resolve.Invoke(toJS(value))
			}()
			return nil
		})
		promise := js.Global().Get("Promise").New(executor)
		executor.Release()
		return promise
	})
	heldFuncs = append(heldFuncs, fn)
	return fn
}

func initializeForm(form js.Value) {
	dataset := form.Get("dataset")
	if dataset.Get("mrcWasmInitialized").String() == "true" {
		return
	}
	dataset.Set("mrcWasmInitialized", "true")
	setupInputFilters(form)
	getChallengeAsync()

	submitHandler := js.FuncOf(func(_ js.Value, args []js.Value) any {
		event := args[0]
		event.Call("preventDefault")
		go handleSubmit(form)
		return nil
	})
	heldFuncs = append(heldFuncs, submitHandler)
	form.Call("addEventListener", "submit", submitHandler)
}

func setupInputFilters(form js.Value) {
	filters := map[string]func(string) string{
		"#message": sanitizeMultiline,
	}
	for selector, filter := range filters {
		input := form.Call("querySelector", selector)
		if input.IsNull() {
			continue
		}
		currentInput, currentFilter := input, filter
		handler := js.FuncOf(func(_ js.Value, _ []js.Value) any {
			currentInput.Set("value", currentFilter(currentInput.Get("value").String()))
			return nil
		})
		heldFuncs = append(heldFuncs, handler)
		input.Call("addEventListener", "input", handler)
	}
}

func handleSubmit(form js.Value) {
	button := form.Call("querySelector", "#submit-btn")
	message := form.Call("querySelector", "#form-message")
	if button.IsNull() || message.IsNull() {
		return
	}
	copy := messagesForDocument()
	originalButtonText := button.Get("textContent").String()
	button.Set("disabled", true)
	button.Set("textContent", copy.SubmittingButton)
	restore := func() {
		button.Set("disabled", false)
		button.Set("textContent", originalButtonText)
	}

	values, err := formValues(form)
	if err != nil {
		showMessage(message, "error", copy.Invalid)
		restore()
		return
	}
	if values["website"] != "" {
		showMessage(message, "success", copy.Success)
		restore()
		return
	}
	if !accepted(form) {
		showMessage(message, "error", copy.Consent)
		restore()
		return
	}

	data := buildPayload(values)
	if err := validatePayload(form, data); err != nil {
		showMessage(message, "error", copy.Invalid)
		restore()
		return
	}
	showMessage(message, "info", copy.Submitting)
	if _, err := submit(data); err != nil {
		reportFailure(data.FormID, err)
		showMessage(message, "error", copy.RelayError)
		restore()
		return
	}
	showMessage(message, "success", copy.Success)
	form.Call("reset")
	restore()
}

func messagesForDocument() uiMessages {
	language := strings.ToLower(js.Global().Get("document").Get("documentElement").Get("lang").String())
	if index := strings.Index(language, "-"); index >= 0 {
		language = language[:index]
	}
	if messages, ok := localizedMessages[language]; ok {
		return messages
	}
	return localizedMessages["en"]
}

func formValues(form js.Value) (map[string]string, error) {
	formData := js.Global().Get("FormData").New(form)
	object := js.Global().Get("Object").Call("fromEntries", formData.Call("entries"))
	encoded := js.Global().Get("JSON").Call("stringify", object).String()
	values := map[string]string{}
	if err := json.Unmarshal([]byte(encoded), &values); err != nil {
		return nil, errors.New("unable to read the contact form")
	}
	return values, nil
}

func buildPayload(values map[string]string) payload {
	standard := map[string]bool{
		"name": true, "email": true, "phone": true, "company": true,
		"message": true, "form_id": true, "slack_profile": true,
		"preferred_language": true, "site_language": true, "browser_language": true,
		"browser_languages": true, "english_support_preference": true,
	}
	extra := map[string]string{}
	for key, value := range values {
		if !standard[key] {
			extra[key] = truncate(sanitizeText(value), 200)
		}
	}
	data := payload{
		Name:           truncate(sanitizeText(values["name"]), 100),
		Email:          sanitizeEmail(values["email"]),
		Phone:          sanitizePhone(values["phone"]),
		Company:        truncate(sanitizeText(values["company"]), 150),
		Message:        truncate(sanitizeMultiline(values["message"]), 2000),
		FormID:         truncate(sanitizeText(values["form_id"]), 100),
		SlackProfile:   sanitizeURL(values["slack_profile"]),
		PreferredLang:  truncate(sanitizeText(values["preferred_language"]), 200),
		SiteLang:       normalizeLanguageTag(values["site_language"]),
		BrowserLang:    truncate(sanitizeText(values["browser_language"]), 200),
		BrowserLangs:   truncate(sanitizeText(values["browser_languages"]), 200),
		EnglishSupport: truncate(sanitizeText(values["english_support_preference"]), 200),
		ExtraFields:    extra,
	}
	for key, value := range map[string]string{
		"preferred_language":         normalizeLanguageTag(data.PreferredLang),
		"site_language":              data.SiteLang,
		"browser_language":           data.BrowserLang,
		"browser_languages":          data.BrowserLangs,
		"english_support_preference": data.EnglishSupport,
	} {
		if value != "" {
			data.ExtraFields[key] = value
		}
	}
	if data.SlackProfile != "" {
		data.ExtraFields["slack_profile"] = data.SlackProfile
	}
	if rush := extra["rush_service"]; rush != "" {
		data.Message = fmt.Sprintf("rush_service: %s\n\n%s", rush, strings.TrimSpace(data.Message))
	}
	if device := strings.TrimSpace(extra["device_name"]); device != "" {
		data.Message = fmt.Sprintf("--- device_name ---\ndevice_name: %s\n\n%s", device, strings.TrimSpace(data.Message))
	}
	data.Raw = payloadMap(data)
	return data
}

func validatePayload(form js.Value, data payload) error {
	device := form.Call("querySelector", "#device_name")
	return validatePayloadFields(data, !device.IsNull() && device.Get("required").Bool())
}

func accepted(form js.Value) bool {
	for _, selector := range []string{"#accept_legal", "#accept_terms"} {
		field := form.Call("querySelector", selector)
		if !field.IsNull() && visible(field) {
			return field.Get("checked").Bool()
		}
	}
	all := true
	found := false
	for _, selector := range []string{"#accept_privacy", "#accept_tos", "#accept_how_it_works"} {
		field := form.Call("querySelector", selector)
		if !field.IsNull() && visible(field) {
			found = true
			all = all && field.Get("checked").Bool()
		}
	}
	return found && all
}

func visible(element js.Value) bool {
	return !element.Get("offsetParent").IsNull()
}

func showMessage(element js.Value, kind, text string) {
	element.Set("textContent", text)
	element.Set("className", "form-message "+kind)
	element.Get("style").Set("display", "block")
}

func reportFailure(formID string, err error) {
	reporter := js.Global().Get("reportNotomoIssue")
	if reporter.Type() == js.TypeFunction {
		reporter.Invoke("contact_form_submission_failed", js.ValueOf(failureDetails(formID, err)))
	}
}

func submit(data payload) (any, error) {
	for attempt := 0; attempt < 2; attempt++ {
		challenge, err := getChallenge(attempt > 0)
		if err != nil {
			return nil, err
		}
		if wait := time.Until(time.UnixMilli(challenge.ReadyAt)); wait > 0 {
			time.Sleep(wait)
		}
		counter, err := solveProof(challenge.Challenge, payloadDigest(data), challenge.Difficulty, maxCounter)
		if err != nil {
			return nil, &failure{class: "proof_compute"}
		}
		body := payloadMap(data)
		body["form_proof_token"] = challenge.Challenge
		body["form_proof_counter"] = counter
		status, response, err := fetchJSON(submitURL, "POST", body)
		if err != nil {
			return nil, err
		}
		if status == 403 && response.Code == "FORM_PROOF_INVALID" && attempt == 0 {
			continue
		}
		if status < 200 || status >= 300 || !response.OK || !response.Success {
			class := "submit_rejected"
			if status < 200 || status >= 300 {
				class = "submit_http"
			}
			return nil, &failure{class: class, status: status, code: allowlistedRelayCode(response.Code)}
		}
		return response, nil
	}
	return nil, &failure{class: "submit_rejected"}
}

func getChallenge(force bool) (proofChallenge, error) {
	challengeLock.Lock()
	defer challengeLock.Unlock()
	if !force && cachedProof.Challenge != "" && cachedProof.ExpiresAt > time.Now().Add(time.Minute).UnixMilli() {
		return cachedProof, nil
	}
	status, response, raw, err := fetchRaw(proofURL, "GET", nil)
	if err != nil {
		return proofChallenge{}, err
	}
	if status < 200 || status >= 300 {
		return proofChallenge{}, &failure{class: "proof_http", status: status, code: allowlistedRelayCode(response.Code)}
	}
	var challenge proofChallenge
	if err := json.Unmarshal(raw, &challenge); err != nil || challenge.Challenge == "" || challenge.Difficulty < 0 {
		return proofChallenge{}, &failure{class: "proof_challenge", status: status}
	}
	cachedProof = challenge
	return challenge, nil
}

func getChallengeAsync() {
	go func() { _, _ = getChallenge(false) }()
}

func fetchJSON(url, method string, body any) (int, responseData, error) {
	status, response, _, err := fetchRaw(url, method, body)
	return status, response, err
}

func fetchRaw(url, method string, body any) (int, responseData, []byte, error) {
	phase := "submit"
	if url == proofURL {
		phase = "proof"
	}
	controller := js.Global().Get("AbortController").New()
	requestFailure := func(kind string, status int) error {
		if controller.Get("signal").Get("aborted").Bool() {
			kind = "timeout"
		}
		return &failure{class: phase + "_" + kind, status: status}
	}
	options := map[string]any{
		"method":  method,
		"headers": map[string]any{"Accept": "application/json"},
		"cache":   "no-store",
		"signal":  controller.Get("signal"),
	}
	if body != nil {
		encoded, _ := json.Marshal(body)
		options["headers"] = map[string]any{"Accept": "application/json", "Content-Type": "application/json"}
		options["body"] = string(encoded)
	}
	timer := time.AfterFunc(requestTimeout, func() { controller.Call("abort") })
	defer timer.Stop()
	result, err := await(js.Global().Call("fetch", url, js.ValueOf(options)))
	if err != nil {
		return 0, responseData{}, nil, requestFailure("network", 0)
	}
	status := result.Get("status").Int()
	text, err := await(result.Call("text"))
	if err != nil {
		return status, responseData{}, nil, requestFailure("response_read", status)
	}
	raw := []byte(text.String())
	var response responseData
	if err := json.Unmarshal(raw, &response); err != nil {
		return status, responseData{}, raw, &failure{class: phase + "_response_json", status: status}
	}
	return status, response, raw, nil
}

func await(promise js.Value) (js.Value, error) {
	type result struct {
		value js.Value
		err   error
	}
	channel := make(chan result, 1)
	resolve := js.FuncOf(func(_ js.Value, args []js.Value) any {
		channel <- result{value: args[0]}
		return nil
	})
	reject := js.FuncOf(func(_ js.Value, args []js.Value) any {
		message := "request failed"
		if len(args) > 0 {
			message = args[0].Get("message").String()
		}
		channel <- result{err: errors.New(message)}
		return nil
	})
	promise.Call("then", resolve).Call("catch", reject)
	completed := <-channel
	resolve.Release()
	reject.Release()
	return completed.value, completed.err
}

func decodePayload(value js.Value) (payload, error) {
	encoded := js.Global().Get("JSON").Call("stringify", value).String()
	data, err := decodePayloadJSON([]byte(encoded))
	if err != nil {
		return payload{}, errors.New("invalid form payload")
	}
	data.Binding = proofBindingFromJS(value)
	return data, nil
}

func proofBindingFromJS(value js.Value) []string {
	stringValue := func(item js.Value) string {
		if item.IsUndefined() || item.IsNull() {
			return ""
		}
		return js.Global().Get("String").Invoke(item).String()
	}
	aliasValue := func(primary, alias string) js.Value {
		item := value.Get(primary)
		if item.Truthy() {
			return item
		}
		return value.Get(alias)
	}
	return []string{
		proofBindingVersion,
		stringValue(aliasValue("form_id", "source")),
		stringValue(aliasValue("name", "full_name")),
		stringValue(value.Get("email")),
		stringValue(aliasValue("phone", "phone_number")),
		stringValue(value.Get("company")),
		stringValue(aliasValue("message", "notes")),
		stringValue(value.Get("slack_profile")),
	}
}

func toJS(value any) js.Value {
	encoded, _ := json.Marshal(value)
	return js.Global().Get("JSON").Call("parse", string(encoded))
}
