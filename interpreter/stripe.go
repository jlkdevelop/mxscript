// stripe.go — thin one-shot wrappers around the Stripe API. The four
// calls covered here (checkout session, customer, portal, subscription)
// are what 90% of SaaS apps actually need. Anything fancier — disputes,
// refunds, payouts — should drop straight to fetch() with the
// `STRIPE_SECRET_KEY` env var; the surface here intentionally stays
// small.
//
// Combined with v0.56's webhooks.verify_stripe, the full Stripe loop
// (create checkout → user pays → webhook updates DB) is about 30 lines
// of MX. See examples/stripe.mx.
package interpreter

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// stripeBaseURLFn returns the base URL for Stripe API calls. Tests
// override this to point at an httptest server; production resolves
// to the real Stripe endpoint.
var stripeBaseURLFn = func() string { return "https://api.stripe.com/v1" }

// stripeRequest is the shared HTTP plumbing every helper uses. Stripe
// expects form-encoded bodies (yes, even in 2026), basic auth via the
// secret key, and returns JSON. Centralising it here means the four
// callers stay one screen each.
func stripeRequest(method, path string, params url.Values) (map[string]any, error) {
	apiKey := os.Getenv("STRIPE_SECRET_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("stripe.* requires STRIPE_SECRET_KEY")
	}

	body := strings.NewReader(params.Encode())
	req, err := http.NewRequest(method, stripeBaseURLFn()+path, body)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(apiKey, "")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("stripe %s %s: %d %s", method, path, resp.StatusCode, string(raw))
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

// stripe.checkout(price_id, opts?) — creates a Checkout Session and
// returns { url, id }. The user is redirected to `url` to pay; on
// success Stripe sends a `checkout.session.completed` webhook.
//
//	let s = stripe.checkout("price_1ABC...", {
//	  success_url: "https://app.example.com/welcome",
//	  cancel_url:  "https://app.example.com/pricing",
//	  mode:        "subscription",     // default "payment"
//	  customer_email: "alice@app.com",
//	})
//	return redirect(s.url)
func builtinStripeCheckout(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindString {
		return Value{}, fmt.Errorf("stripe.checkout(price_id, opts?)")
	}
	priceID := args[0].String

	mode := "payment"
	successURL := "https://example.com/success"
	cancelURL := "https://example.com/cancel"
	customerEmail := ""
	customerID := ""
	quantity := "1"
	if len(args) > 1 && args[1].Kind == KindObject {
		opts := args[1].Object
		if v, ok := opts.Get("mode"); ok && v.Kind == KindString {
			mode = v.String
		}
		if v, ok := opts.Get("success_url"); ok && v.Kind == KindString {
			successURL = v.String
		}
		if v, ok := opts.Get("cancel_url"); ok && v.Kind == KindString {
			cancelURL = v.String
		}
		if v, ok := opts.Get("customer_email"); ok && v.Kind == KindString {
			customerEmail = v.String
		}
		if v, ok := opts.Get("customer"); ok && v.Kind == KindString {
			customerID = v.String
		}
		if v, ok := opts.Get("quantity"); ok && v.Kind == KindNumber {
			quantity = fmt.Sprintf("%d", int(v.Number))
		}
	}

	form := url.Values{}
	form.Set("mode", mode)
	form.Set("success_url", successURL)
	form.Set("cancel_url", cancelURL)
	form.Set("line_items[0][price]", priceID)
	form.Set("line_items[0][quantity]", quantity)
	if customerEmail != "" {
		form.Set("customer_email", customerEmail)
	}
	if customerID != "" {
		form.Set("customer", customerID)
	}

	parsed, err := stripeRequest("POST", "/checkout/sessions", form)
	if err != nil {
		return Value{}, err
	}
	out := NewOrderedMap()
	if u, ok := parsed["url"].(string); ok {
		out.Set("url", StringValue(u))
	}
	if id, ok := parsed["id"].(string); ok {
		out.Set("id", StringValue(id))
	}
	return ObjectValue(out), nil
}

// stripe.customer_create(email, opts?) — create a Customer record and
// return { id, email }. Pass the returned id to subsequent
// subscription / portal calls so charges and invoices stay associated.
//
//	let c = stripe.customer_create("alice@app.com", { name: "Alice" })
//	// persist c.id alongside your user row
func builtinStripeCustomerCreate(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindString {
		return Value{}, fmt.Errorf("stripe.customer_create(email, opts?)")
	}
	email := args[0].String

	name := ""
	metadata := map[string]string{}
	if len(args) > 1 && args[1].Kind == KindObject {
		opts := args[1].Object
		if v, ok := opts.Get("name"); ok && v.Kind == KindString {
			name = v.String
		}
		if v, ok := opts.Get("metadata"); ok && v.Kind == KindObject {
			for _, k := range v.Object.Keys {
				mv, _ := v.Object.Get(k)
				if mv.Kind == KindString {
					metadata[k] = mv.String
				}
			}
		}
	}

	form := url.Values{}
	form.Set("email", email)
	if name != "" {
		form.Set("name", name)
	}
	for k, v := range metadata {
		form.Set("metadata["+k+"]", v)
	}

	parsed, err := stripeRequest("POST", "/customers", form)
	if err != nil {
		return Value{}, err
	}
	out := NewOrderedMap()
	if id, ok := parsed["id"].(string); ok {
		out.Set("id", StringValue(id))
	}
	out.Set("email", StringValue(email))
	return ObjectValue(out), nil
}

// stripe.customer_portal(customer_id, return_url) — create a Customer
// Portal session URL the user can be redirected to. Stripe handles the
// entire "manage subscription / update card / view invoices" surface
// without any code from you.
//
//	let p = stripe.customer_portal(user.stripe_id, "https://app.example.com/account")
//	return redirect(p.url)
func builtinStripeCustomerPortal(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 || args[0].Kind != KindString || args[1].Kind != KindString {
		return Value{}, fmt.Errorf("stripe.customer_portal(customer_id, return_url)")
	}
	form := url.Values{}
	form.Set("customer", args[0].String)
	form.Set("return_url", args[1].String)

	parsed, err := stripeRequest("POST", "/billing_portal/sessions", form)
	if err != nil {
		return Value{}, err
	}
	out := NewOrderedMap()
	if u, ok := parsed["url"].(string); ok {
		out.Set("url", StringValue(u))
	}
	if id, ok := parsed["id"].(string); ok {
		out.Set("id", StringValue(id))
	}
	return ObjectValue(out), nil
}

// stripe.subscription_create(customer_id, price_id, opts?) — start a
// subscription server-side (no Checkout). For most apps the Checkout
// Session flow is friendlier; this call is here for backend automation
// flows where you already have a saved payment method on the customer.
//
//	let sub = stripe.subscription_create("cus_xxx", "price_yyy")
//	// sub.id, sub.status
func builtinStripeSubscriptionCreate(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 || args[0].Kind != KindString || args[1].Kind != KindString {
		return Value{}, fmt.Errorf("stripe.subscription_create(customer_id, price_id, opts?)")
	}
	form := url.Values{}
	form.Set("customer", args[0].String)
	form.Set("items[0][price]", args[1].String)
	if len(args) > 2 && args[2].Kind == KindObject {
		opts := args[2].Object
		if v, ok := opts.Get("trial_period_days"); ok && v.Kind == KindNumber {
			form.Set("trial_period_days", fmt.Sprintf("%d", int(v.Number)))
		}
	}

	parsed, err := stripeRequest("POST", "/subscriptions", form)
	if err != nil {
		return Value{}, err
	}
	out := NewOrderedMap()
	if id, ok := parsed["id"].(string); ok {
		out.Set("id", StringValue(id))
	}
	if status, ok := parsed["status"].(string); ok {
		out.Set("status", StringValue(status))
	}
	return ObjectValue(out), nil
}
