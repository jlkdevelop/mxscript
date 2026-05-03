// s3.go — S3-compatible object storage. Implements AWS Signature V4
// directly (no external SDK) so the same builtins work against AWS
// S3, Cloudflare R2, Backblaze B2, DigitalOcean Spaces, MinIO, Wasabi,
// and any other S3-compatible store. The API surface is intentionally
// small — five calls cover every common need:
//
//   s3.put(bucket, key, body, opts?)         upload
//   s3.get(bucket, key, opts?)               download (returns body string)
//   s3.delete(bucket, key, opts?)            delete
//   s3.list(bucket, prefix?, opts?)          array of keys
//   s3.presign(bucket, key, opts?)           presigned GET URL with expiry
//
// All read AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY / AWS_REGION
// from the environment by default. opts.endpoint overrides the host
// for non-AWS providers; opts.region overrides the region for
// requests outside us-east-1.
package interpreter

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

const s3UnsignedPayload = "UNSIGNED-PAYLOAD"

// s3Config bundles every per-call parameter the SigV4 builder needs.
// Centralising them avoids a 7-arg helper signature.
type s3Config struct {
	Bucket      string
	Key         string
	Region      string
	Endpoint    string // empty = AWS S3 (s3.<region>.amazonaws.com)
	AccessKey   string
	SecretKey   string
	Method      string
	Body        []byte
	ContentType string
}

// s3OptsToConfig pulls AWS credentials and endpoint overrides out of
// the env + the optional opts object.
func s3OptsToConfig(bucket, key, method string, body []byte, optsArg Value) (*s3Config, error) {
	cfg := &s3Config{
		Bucket:    bucket,
		Key:       key,
		Method:    method,
		Body:      body,
		Region:    envOrDefault("AWS_REGION", "us-east-1"),
		AccessKey: os.Getenv("AWS_ACCESS_KEY_ID"),
		SecretKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
	}
	if cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("s3: AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY must be set")
	}
	if optsArg.Kind == KindObject {
		if v, ok := optsArg.Object.Get("endpoint"); ok && v.Kind == KindString {
			cfg.Endpoint = strings.TrimSuffix(v.String, "/")
		}
		if v, ok := optsArg.Object.Get("region"); ok && v.Kind == KindString {
			cfg.Region = v.String
		}
		if v, ok := optsArg.Object.Get("content_type"); ok && v.Kind == KindString {
			cfg.ContentType = v.String
		}
		if v, ok := optsArg.Object.Get("access_key"); ok && v.Kind == KindString {
			cfg.AccessKey = v.String
		}
		if v, ok := optsArg.Object.Get("secret_key"); ok && v.Kind == KindString {
			cfg.SecretKey = v.String
		}
	}
	return cfg, nil
}

func envOrDefault(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

// s3Host returns the hostname for the request: either the user's
// custom endpoint (R2 / B2 / MinIO) or the standard AWS S3 host.
func s3Host(cfg *s3Config) string {
	if cfg.Endpoint != "" {
		// Strip protocol if user passed e.g. "https://r2.cloudflarestorage.com".
		host := cfg.Endpoint
		host = strings.TrimPrefix(host, "https://")
		host = strings.TrimPrefix(host, "http://")
		return host
	}
	return fmt.Sprintf("s3.%s.amazonaws.com", cfg.Region)
}

// s3RequestURL builds the canonical URL for the given config. We use
// path-style addressing (host/bucket/key) so it works uniformly
// across providers — virtual-host-style requires per-region cert
// gymnastics that aren't worth the complexity here.
func s3RequestURL(cfg *s3Config) string {
	host := s3Host(cfg)
	scheme := "https"
	if strings.HasPrefix(cfg.Endpoint, "http://") {
		scheme = "http"
	}
	return fmt.Sprintf("%s://%s/%s/%s", scheme, host, cfg.Bucket, escapeS3Path(cfg.Key))
}

// escapeS3Path percent-encodes everything except `/` so nested keys
// like `users/123/avatar.png` stay readable.
func escapeS3Path(p string) string {
	parts := strings.Split(p, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

// signV4 builds an Authorization header per AWS Signature Version 4
// spec. https://docs.aws.amazon.com/general/latest/gr/sigv4-signed-request-examples.html
func signV4(req *http.Request, cfg *s3Config, payloadHash string) {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	req.Header.Set("Host", req.URL.Host)
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	// Canonical request.
	canonicalQuery := canonicalQueryString(req.URL.Query())
	canonicalHeaders, signedHeaders := canonicalHeaders(req)
	canonicalRequest := strings.Join([]string{
		req.Method,
		"/" + strings.TrimPrefix(req.URL.Path, "/"),
		canonicalQuery,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	credentialScope := fmt.Sprintf("%s/%s/s3/aws4_request", dateStamp, cfg.Region)
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credentialScope,
		hexSHA256([]byte(canonicalRequest)),
	}, "\n")

	signingKey := deriveSigningKey(cfg.SecretKey, dateStamp, cfg.Region, "s3")
	signature := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))

	auth := fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		cfg.AccessKey, credentialScope, signedHeaders, signature,
	)
	req.Header.Set("Authorization", auth)
}

func canonicalQueryString(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		for _, v := range q[k] {
			parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(v))
		}
	}
	return strings.Join(parts, "&")
}

func canonicalHeaders(req *http.Request) (string, string) {
	keys := make([]string, 0, len(req.Header))
	for k := range req.Header {
		keys = append(keys, strings.ToLower(k))
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		v := req.Header.Get(k)
		// Trim and collapse interior whitespace per the spec.
		v = strings.TrimSpace(v)
		fmt.Fprintf(&b, "%s:%s\n", k, v)
	}
	return b.String(), strings.Join(keys, ";")
}

func deriveSigningKey(secret, dateStamp, region, service string) []byte {
	k1 := hmacSHA256([]byte("AWS4"+secret), dateStamp)
	k2 := hmacSHA256(k1, region)
	k3 := hmacSHA256(k2, service)
	return hmacSHA256(k3, "aws4_request")
}

func hmacSHA256(key []byte, s string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(s))
	return mac.Sum(nil)
}

func hexSHA256(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// ===== Builtins =====

func builtinS3Put(_ *Interpreter, args []Value) (Value, error) {
	bucket, key, body, opts, err := s3CommonArgs(args, "s3.put", true)
	if err != nil {
		return Value{}, err
	}
	cfg, err := s3OptsToConfig(bucket, key, "PUT", body, opts)
	if err != nil {
		return Value{}, err
	}
	req, err := http.NewRequest("PUT", s3RequestURL(cfg), bytes.NewReader(body))
	if err != nil {
		return Value{}, err
	}
	if cfg.ContentType != "" {
		req.Header.Set("Content-Type", cfg.ContentType)
	}
	signV4(req, cfg, hexSHA256(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Value{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return Value{}, fmt.Errorf("s3.put failed (%d): %s", resp.StatusCode, string(raw))
	}
	return NullValue(), nil
}

func builtinS3Get(_ *Interpreter, args []Value) (Value, error) {
	bucket, key, _, opts, err := s3CommonArgs(args, "s3.get", false)
	if err != nil {
		return Value{}, err
	}
	cfg, err := s3OptsToConfig(bucket, key, "GET", nil, opts)
	if err != nil {
		return Value{}, err
	}
	req, err := http.NewRequest("GET", s3RequestURL(cfg), nil)
	if err != nil {
		return Value{}, err
	}
	signV4(req, cfg, hexSHA256(nil))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Value{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return Value{}, fmt.Errorf("s3.get failed (%d): %s", resp.StatusCode, string(body))
	}
	return StringValue(string(body)), nil
}

func builtinS3Delete(_ *Interpreter, args []Value) (Value, error) {
	bucket, key, _, opts, err := s3CommonArgs(args, "s3.delete", false)
	if err != nil {
		return Value{}, err
	}
	cfg, err := s3OptsToConfig(bucket, key, "DELETE", nil, opts)
	if err != nil {
		return Value{}, err
	}
	req, err := http.NewRequest("DELETE", s3RequestURL(cfg), nil)
	if err != nil {
		return Value{}, err
	}
	signV4(req, cfg, hexSHA256(nil))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Value{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return Value{}, fmt.Errorf("s3.delete failed (%d): %s", resp.StatusCode, string(raw))
	}
	return NullValue(), nil
}

// s3.list(bucket, prefix?, opts?) — returns up to 1000 matching keys
// per request. Prefix is optional; pass "" to list everything.
func builtinS3List(_ *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindString {
		return Value{}, fmt.Errorf("s3.list(bucket, prefix?, opts?) requires a bucket")
	}
	prefix := ""
	var opts Value
	if len(args) > 1 && args[1].Kind == KindString {
		prefix = args[1].String
	}
	if len(args) > 2 {
		opts = args[2]
	}
	cfg, err := s3OptsToConfig(args[0].String, "", "GET", nil, opts)
	if err != nil {
		return Value{}, err
	}
	host := s3Host(cfg)
	scheme := "https"
	if strings.HasPrefix(cfg.Endpoint, "http://") {
		scheme = "http"
	}
	listURL := fmt.Sprintf("%s://%s/%s?list-type=2", scheme, host, cfg.Bucket)
	if prefix != "" {
		listURL += "&prefix=" + url.QueryEscape(prefix)
	}
	req, err := http.NewRequest("GET", listURL, nil)
	if err != nil {
		return Value{}, err
	}
	signV4(req, cfg, hexSHA256(nil))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Value{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return Value{}, fmt.Errorf("s3.list failed (%d): %s", resp.StatusCode, string(body))
	}
	type listBucketResult struct {
		Contents []struct {
			Key string `xml:"Key"`
		} `xml:"Contents"`
	}
	var parsed listBucketResult
	if err := xml.Unmarshal(body, &parsed); err != nil {
		return Value{}, err
	}
	out := make([]Value, len(parsed.Contents))
	for i, c := range parsed.Contents {
		out[i] = StringValue(c.Key)
	}
	return ArrayValue(out), nil
}

// s3.presign(bucket, key, opts?) — returns a presigned GET URL valid
// for `expires` seconds (default 3600). Hand to the browser to let
// users download private objects without exposing credentials.
func builtinS3Presign(_ *Interpreter, args []Value) (Value, error) {
	bucket, key, _, opts, err := s3CommonArgs(args, "s3.presign", false)
	if err != nil {
		return Value{}, err
	}
	cfg, err := s3OptsToConfig(bucket, key, "GET", nil, opts)
	if err != nil {
		return Value{}, err
	}
	expires := 3600
	if opts.Kind == KindObject {
		if v, ok := opts.Object.Get("expires"); ok && v.Kind == KindNumber {
			expires = int(v.Number)
		}
	}

	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	credentialScope := fmt.Sprintf("%s/%s/s3/aws4_request", dateStamp, cfg.Region)

	host := s3Host(cfg)
	scheme := "https"
	if strings.HasPrefix(cfg.Endpoint, "http://") {
		scheme = "http"
	}
	canonicalURI := "/" + cfg.Bucket + "/" + escapeS3Path(cfg.Key)

	q := url.Values{}
	q.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	q.Set("X-Amz-Credential", cfg.AccessKey+"/"+credentialScope)
	q.Set("X-Amz-Date", amzDate)
	q.Set("X-Amz-Expires", fmt.Sprintf("%d", expires))
	q.Set("X-Amz-SignedHeaders", "host")

	canonicalQuery := canonicalQueryString(q)
	canonicalRequest := strings.Join([]string{
		"GET",
		canonicalURI,
		canonicalQuery,
		"host:" + host + "\n",
		"host",
		s3UnsignedPayload,
	}, "\n")

	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credentialScope,
		hexSHA256([]byte(canonicalRequest)),
	}, "\n")

	signingKey := deriveSigningKey(cfg.SecretKey, dateStamp, cfg.Region, "s3")
	signature := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))
	q.Set("X-Amz-Signature", signature)

	final := fmt.Sprintf("%s://%s%s?%s", scheme, host, canonicalURI, q.Encode())
	return StringValue(final), nil
}

// s3CommonArgs validates the common (bucket, key, [body], [opts])
// shape every builtin uses. needsBody=true requires a third arg.
func s3CommonArgs(args []Value, name string, needsBody bool) (string, string, []byte, Value, error) {
	if len(args) < 2 || args[0].Kind != KindString || args[1].Kind != KindString {
		return "", "", nil, Value{}, fmt.Errorf("%s(bucket, key, ...) requires (string, string, ...)", name)
	}
	bucket := args[0].String
	key := args[1].String
	var body []byte
	var opts Value
	if needsBody {
		if len(args) < 3 {
			return "", "", nil, Value{}, fmt.Errorf("%s requires a body argument", name)
		}
		switch args[2].Kind {
		case KindString:
			body = []byte(args[2].String)
		case KindNull:
			body = nil
		default:
			body = []byte(args[2].Display())
		}
		if len(args) > 3 {
			opts = args[3]
		}
	} else if len(args) > 2 {
		opts = args[2]
	}
	return bucket, key, body, opts, nil
}
