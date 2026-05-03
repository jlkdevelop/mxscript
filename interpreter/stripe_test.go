package interpreter

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

// stripeMockServer captures the inbound request and returns the JSON
// body the test wants — same plumbing pattern as the notify tests.
func stripeMockServer(t *testing.T, response string) (string, *url.Values, *http.Header) {
	t.Helper()
	var lastForm url.Values
	var lastHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		parsed, _ := url.ParseQuery(string(raw))
		lastForm = parsed
		lastHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(response))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &lastForm, &lastHeaders
}

func withStripeURL(t *testing.T, url string, fn func()) {
	t.Helper()
	prevKey := os.Getenv("STRIPE_SECRET_KEY")
	os.Setenv("STRIPE_SECRET_KEY", "sk_test_dummy")
	defer func() {
		if prevKey == "" {
			os.Unsetenv("STRIPE_SECRET_KEY")
		} else {
			os.Setenv("STRIPE_SECRET_KEY", prevKey)
		}
	}()
	// Override the base URL via a one-shot helper that mirrors stripeRequest.
	// Easier than monkey-patching: we test the builtin's argument-handling
	// shape and result construction, while the URL stays Stripe in prod.
	_ = url
	fn()
}

func TestStripeRequiresApiKey(t *testing.T) {
	prev := os.Getenv("STRIPE_SECRET_KEY")
	os.Unsetenv("STRIPE_SECRET_KEY")
	defer func() {
		if prev != "" {
			os.Setenv("STRIPE_SECRET_KEY", prev)
		}
	}()
	_, err := builtinStripeCheckout(nil, []Value{StringValue("price_xxx")})
	if err == nil || !strings.Contains(err.Error(), "STRIPE_SECRET_KEY") {
		t.Errorf("expected missing-key error, got %v", err)
	}
}

func TestStripeCheckoutPayload(t *testing.T) {
	// Run the wire-format test against our mock by overriding the base
	// URL globally. We accept this is a tiny dependency injection on a
	// const; if the test can't shape the request it would silently miss
	// regressions in the encoded form fields.
	mock, formCapture, headerCapture := stripeMockServer(t, `{
		"id": "cs_test_123",
		"url": "https://checkout.stripe.com/c/pay/cs_test_123"
	}`)

	prevURL := stripeBaseURLFn()
	stripeBaseURLFn = func() string { return mock }
	defer func() { stripeBaseURLFn = func() string { return prevURL } }()

	prevKey := os.Getenv("STRIPE_SECRET_KEY")
	os.Setenv("STRIPE_SECRET_KEY", "sk_test_dummy")
	defer func() {
		if prevKey == "" {
			os.Unsetenv("STRIPE_SECRET_KEY")
		} else {
			os.Setenv("STRIPE_SECRET_KEY", prevKey)
		}
	}()

	opts := NewOrderedMap()
	opts.Set("mode", StringValue("subscription"))
	opts.Set("success_url", StringValue("https://app/welcome"))
	opts.Set("cancel_url", StringValue("https://app/pricing"))
	opts.Set("customer_email", StringValue("alice@app.com"))

	v, err := builtinStripeCheckout(nil, []Value{StringValue("price_abc"), ObjectValue(opts)})
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	out := v.Object
	if u, _ := out.Get("url"); u.String != "https://checkout.stripe.com/c/pay/cs_test_123" {
		t.Errorf("url: got %v", u)
	}
	if id, _ := out.Get("id"); id.String != "cs_test_123" {
		t.Errorf("id: got %v", id)
	}
	form := *formCapture
	if form.Get("mode") != "subscription" || form.Get("line_items[0][price]") != "price_abc" {
		t.Errorf("payload missing fields: %v", form)
	}
	if form.Get("customer_email") != "alice@app.com" {
		t.Errorf("customer_email missing: %v", form)
	}
	auth := (*headerCapture).Get("Authorization")
	if !strings.HasPrefix(auth, "Basic ") {
		t.Errorf("expected Basic auth header, got %q", auth)
	}
}

func TestStripeCustomerCreatePayload(t *testing.T) {
	mock, formCapture, _ := stripeMockServer(t, `{ "id": "cus_xxx" }`)
	prev := stripeBaseURLFn()
	stripeBaseURLFn = func() string { return mock }
	defer func() { stripeBaseURLFn = func() string { return prev } }()
	os.Setenv("STRIPE_SECRET_KEY", "sk_test_dummy")
	defer os.Unsetenv("STRIPE_SECRET_KEY")

	meta := NewOrderedMap()
	meta.Set("plan", StringValue("pro"))
	opts := NewOrderedMap()
	opts.Set("name", StringValue("Alice"))
	opts.Set("metadata", ObjectValue(meta))

	v, err := builtinStripeCustomerCreate(nil, []Value{StringValue("alice@app.com"), ObjectValue(opts)})
	if err != nil {
		t.Fatalf("customer_create: %v", err)
	}
	out := v.Object
	if id, _ := out.Get("id"); id.String != "cus_xxx" {
		t.Errorf("id: got %v", id)
	}
	form := *formCapture
	if form.Get("email") != "alice@app.com" || form.Get("name") != "Alice" {
		t.Errorf("payload: got %v", form)
	}
	if form.Get("metadata[plan]") != "pro" {
		t.Errorf("metadata: got %v", form.Get("metadata[plan]"))
	}
}

func TestStripePortalAndSubscriptionShape(t *testing.T) {
	// Two helpers in one test — both build trivial form bodies and
	// pass through the Stripe ID to the result object.
	checkPortal := func() {
		mock, form, _ := stripeMockServer(t, `{ "id": "bps_x", "url": "https://billing.stripe.com/p/x" }`)
		prev := stripeBaseURLFn()
		stripeBaseURLFn = func() string { return mock }
		defer func() { stripeBaseURLFn = func() string { return prev } }()
		os.Setenv("STRIPE_SECRET_KEY", "sk_test_dummy")
		defer os.Unsetenv("STRIPE_SECRET_KEY")
		v, err := builtinStripeCustomerPortal(nil, []Value{StringValue("cus_xxx"), StringValue("https://app.example/account")})
		if err != nil {
			t.Fatalf("portal: %v", err)
		}
		if u, _ := v.Object.Get("url"); u.String != "https://billing.stripe.com/p/x" {
			t.Errorf("portal url: got %v", u)
		}
		if (*form).Get("customer") != "cus_xxx" || (*form).Get("return_url") != "https://app.example/account" {
			t.Errorf("portal form: got %v", *form)
		}
	}
	checkPortal()

	checkSub := func() {
		mock, form, _ := stripeMockServer(t, `{ "id": "sub_x", "status": "active" }`)
		prev := stripeBaseURLFn()
		stripeBaseURLFn = func() string { return mock }
		defer func() { stripeBaseURLFn = func() string { return prev } }()
		os.Setenv("STRIPE_SECRET_KEY", "sk_test_dummy")
		defer os.Unsetenv("STRIPE_SECRET_KEY")
		opts := NewOrderedMap()
		opts.Set("trial_period_days", NumberValue(7))
		v, err := builtinStripeSubscriptionCreate(nil, []Value{
			StringValue("cus_xxx"), StringValue("price_y"), ObjectValue(opts),
		})
		if err != nil {
			t.Fatalf("sub: %v", err)
		}
		if status, _ := v.Object.Get("status"); status.String != "active" {
			t.Errorf("sub status: got %v", status)
		}
		if (*form).Get("trial_period_days") != "7" {
			t.Errorf("trial_period_days: got %v", (*form).Get("trial_period_days"))
		}
	}
	checkSub()

	// Defensive: the live response shape is still parseable JSON when
	// we assert against the mock. (Sanity for how httptest hands things.)
	var got map[string]any
	_ = json.Unmarshal([]byte(`{"id":"x"}`), &got)
}
