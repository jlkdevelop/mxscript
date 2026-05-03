// notify.go — outbound notification primitives. Each function is a
// thin one-shot HTTP call to a well-known service so route handlers
// don't have to ship copy-pasted curl recipes.
//
// Today we cover the three channels real SaaS apps wire up first:
//
//   notify.slack(webhook_url, msg)            // incoming-webhook post
//   notify.discord(webhook_url, msg)          // Discord webhook
//   notify.email(to, subject, body, opts?)    // Resend (recommended) or any RESEND_API_KEY-compatible API
//
// Each returns a small response object: { ok: bool, status: number,
// error: string|null }. Callers stay declarative — `if (!r.ok) ...`
// reads better than wrapping every call in try/catch.
package interpreter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"time"
)

// notifyURLPathEscape wraps net/url.QueryEscape so the test file
// keeps a stable name for monkey-patching if needed.
func notifyURLPathEscape(s string) string { return neturl.QueryEscape(s) }

// notifyResult builds the standard response object every notify.*
// function returns: { ok, status, error }. Centralised so the shape
// stays identical across providers and tests can assert on it.
func notifyResult(status int, errMsg string) Value {
	out := NewOrderedMap()
	out.Set("ok", BoolValue(status >= 200 && status < 300 && errMsg == ""))
	out.Set("status", NumberValue(float64(status)))
	if errMsg != "" {
		out.Set("error", StringValue(errMsg))
	} else {
		out.Set("error", NullValue())
	}
	return ObjectValue(out)
}

// postJSON does the boilerplate that every notify.* helper shares —
// serialise body, POST it, return the resulting (status, error).
// Callers pass headers if they need authentication.
func postJSON(url string, headers map[string]string, body any) (int, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequest("POST", url, bytes.NewReader(raw))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, fmt.Errorf("%s", string(raw))
	}
	return resp.StatusCode, nil
}

// notify.slack(webhook_url, message) — Slack incoming webhook.
//
// `message` may be a plain string (sent as `text`) or an object whose
// keys map directly to Slack's webhook payload (text, blocks,
// attachments, username, icon_emoji, …).
//
//	notify.slack(env("SLACK_WEBHOOK"), "deploy succeeded")
//	notify.slack(env("SLACK_WEBHOOK"), {
//	  text: "deploy",
//	  blocks: [...]
//	})
func builtinNotifySlack(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 || args[0].Kind != KindString {
		return Value{}, fmt.Errorf("notify.slack(webhook_url, message)")
	}
	url := args[0].String
	body := slackOrDiscordBody(args[1])
	status, err := postJSON(url, nil, body)
	if err != nil {
		return notifyResult(status, err.Error()), nil
	}
	return notifyResult(status, ""), nil
}

// notify.discord(webhook_url, message) — Discord webhook.
//
// String messages send `content`; object messages map directly to
// Discord's payload (content, embeds, username, avatar_url, …).
//
//	notify.discord(env("DISCORD_WEBHOOK"), "build broke 💥")
//	notify.discord(env("DISCORD_WEBHOOK"), {
//	  content: "deploy",
//	  embeds: [{ title: "release v1.2.3", color: 65280 }]
//	})
func builtinNotifyDiscord(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 || args[0].Kind != KindString {
		return Value{}, fmt.Errorf("notify.discord(webhook_url, message)")
	}
	url := args[0].String
	body := discordBody(args[1])
	status, err := postJSON(url, nil, body)
	if err != nil {
		return notifyResult(status, err.Error()), nil
	}
	return notifyResult(status, ""), nil
}

// notify.email(to, subject, body, opts?) — transactional email
// through Resend (https://resend.com). Reads RESEND_API_KEY from the
// environment.
//
// `opts` may set: from (default RESEND_FROM env or "noreply@example.com"),
// reply_to, html (vs plain), cc, bcc.
//
//	notify.email("user@example.com", "Welcome",
//	  "Hi! Click the link to verify ${magic}", { html: true })
func builtinNotifyEmail(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 3 {
		return Value{}, fmt.Errorf("notify.email(to, subject, body, opts?)")
	}
	to, _ := stringArg(args, 0)
	subject, _ := stringArg(args, 1)
	body, _ := stringArg(args, 2)
	from := os.Getenv("RESEND_FROM")
	if from == "" {
		from = "noreply@example.com"
	}
	html := false
	var replyTo, cc, bcc string
	if len(args) > 3 && args[3].Kind == KindObject {
		opts := args[3].Object
		if v, ok := opts.Get("from"); ok && v.Kind == KindString {
			from = v.String
		}
		if v, ok := opts.Get("html"); ok && v.Kind == KindBool {
			html = v.Bool
		}
		if v, ok := opts.Get("reply_to"); ok && v.Kind == KindString {
			replyTo = v.String
		}
		if v, ok := opts.Get("cc"); ok && v.Kind == KindString {
			cc = v.String
		}
		if v, ok := opts.Get("bcc"); ok && v.Kind == KindString {
			bcc = v.String
		}
	}
	apiKey := os.Getenv("RESEND_API_KEY")
	if apiKey == "" {
		return notifyResult(0, "notify.email requires RESEND_API_KEY"), nil
	}
	payload := map[string]any{
		"from":    from,
		"to":      to,
		"subject": subject,
	}
	if html {
		payload["html"] = body
	} else {
		payload["text"] = body
	}
	if replyTo != "" {
		payload["reply_to"] = replyTo
	}
	if cc != "" {
		payload["cc"] = cc
	}
	if bcc != "" {
		payload["bcc"] = bcc
	}
	status, err := postJSON("https://api.resend.com/emails",
		map[string]string{"Authorization": "Bearer " + apiKey},
		payload,
	)
	if err != nil {
		return notifyResult(status, err.Error()), nil
	}
	return notifyResult(status, ""), nil
}

// notify.sms(to, body, opts?) — send an SMS via Twilio. Reads
// TWILIO_ACCOUNT_SID + TWILIO_AUTH_TOKEN + TWILIO_FROM_NUMBER from
// the environment.
//
//	notify.sms("+15555550100", "Your code is " + code)
//	notify.sms(user.phone, "Order shipped!", { from: env("TWILIO_FROM_NUMBER") })
func builtinNotifySMS(_ *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 {
		return Value{}, fmt.Errorf("notify.sms(to, body, opts?)")
	}
	to, _ := stringArg(args, 0)
	body, _ := stringArg(args, 1)

	sid := os.Getenv("TWILIO_ACCOUNT_SID")
	token := os.Getenv("TWILIO_AUTH_TOKEN")
	from := os.Getenv("TWILIO_FROM_NUMBER")
	if len(args) > 2 && args[2].Kind == KindObject {
		if v, ok := args[2].Object.Get("from"); ok && v.Kind == KindString {
			from = v.String
		}
	}
	if sid == "" || token == "" {
		return notifyResult(0, "notify.sms requires TWILIO_ACCOUNT_SID + TWILIO_AUTH_TOKEN"), nil
	}
	if from == "" {
		return notifyResult(0, "notify.sms requires TWILIO_FROM_NUMBER (or opts.from)"), nil
	}

	form := []byte("To=" + urlEscape(to) + "&From=" + urlEscape(from) + "&Body=" + urlEscape(body))
	url := "https://api.twilio.com/2010-04-01/Accounts/" + sid + "/Messages.json"
	req, err := http.NewRequest("POST", url, bytes.NewReader(form))
	if err != nil {
		return Value{}, err
	}
	req.SetBasicAuth(sid, token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return notifyResult(0, err.Error()), nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return notifyResult(resp.StatusCode, string(raw)), nil
	}
	return notifyResult(resp.StatusCode, ""), nil
}

// urlEscape is a tiny percent-encoder for SMS form fields. We only
// need to handle the chars Twilio's docs flag — net/url's PathEscape
// does the heavy lifting.
func urlEscape(s string) string {
	return notifyURLPathEscape(s)
}

// slackOrDiscordBody picks the right payload shape: strings become
// `{ text: "..." }`, objects pass through. The two services use the
// same convention for the simple-string case.
func slackOrDiscordBody(v Value) any {
	if v.Kind == KindString {
		return map[string]string{"text": v.String}
	}
	if v.Kind == KindObject {
		return valueToPlainGo(v)
	}
	return map[string]string{"text": v.Display()}
}

func discordBody(v Value) any {
	if v.Kind == KindString {
		return map[string]string{"content": v.String}
	}
	if v.Kind == KindObject {
		return valueToPlainGo(v)
	}
	return map[string]string{"content": v.Display()}
}
