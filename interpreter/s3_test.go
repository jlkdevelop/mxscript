package interpreter

import (
	"net/url"
	"os"
	"strings"
	"testing"
)

// AWS publishes a canonical SigV4 example with known input + output;
// reproducing it byte-for-byte is the surest way to verify our
// implementation is correct.
//
// Inputs and expected output sourced from AWS's published docs.
func TestSigV4DerivedKeyMatchesAWSExample(t *testing.T) {
	secret := "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY"
	dateStamp := "20150830"
	region := "us-east-1"
	service := "iam"

	key := deriveSigningKey(secret, dateStamp, region, service)
	want := "c4afb1cc5771d871763a393e44b703571b55cc28424d1a5e86da6ed3c154a4b9"
	got := hexBytesS3(key)
	if got != want {
		t.Errorf("AWS canonical signing-key mismatch:\n  got  %s\n  want %s", got, want)
	}
}

func hexBytesS3(b []byte) string {
	const hexd = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hexd[v>>4]
		out[i*2+1] = hexd[v&0x0F]
	}
	return string(out)
}

func TestS3HostUsesEndpointWhenSet(t *testing.T) {
	cfg := &s3Config{Region: "us-east-1"}
	if h := s3Host(cfg); h != "s3.us-east-1.amazonaws.com" {
		t.Errorf("default host: got %q", h)
	}
	cfg.Endpoint = "https://r2.cloudflarestorage.com"
	if h := s3Host(cfg); h != "r2.cloudflarestorage.com" {
		t.Errorf("R2 host: got %q", h)
	}
	cfg.Endpoint = "http://localhost:9000"
	if h := s3Host(cfg); h != "localhost:9000" {
		t.Errorf("MinIO host: got %q", h)
	}
}

func TestEscapeS3PathPreservesSlashes(t *testing.T) {
	if got := escapeS3Path("users/123/avatar.png"); got != "users/123/avatar.png" {
		t.Errorf("got %q", got)
	}
	if got := escapeS3Path("with space.jpg"); got != "with%20space.jpg" {
		t.Errorf("space: got %q", got)
	}
}

func TestCanonicalQueryStringSorted(t *testing.T) {
	q := url.Values{}
	q.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	q.Set("X-Amz-Date", "20260503T000000Z")
	q.Set("X-Amz-Expires", "60")
	got := canonicalQueryString(q)
	idxAlg := strings.Index(got, "X-Amz-Algorithm")
	idxDate := strings.Index(got, "X-Amz-Date")
	idxExp := strings.Index(got, "X-Amz-Expires")
	if idxAlg < 0 || idxDate < 0 || idxExp < 0 {
		t.Fatalf("missing keys: %s", got)
	}
	if !(idxAlg < idxDate && idxDate < idxExp) {
		t.Errorf("query keys not sorted: %s", got)
	}
}

func TestS3RequiresCredentials(t *testing.T) {
	prevAK := os.Getenv("AWS_ACCESS_KEY_ID")
	prevSK := os.Getenv("AWS_SECRET_ACCESS_KEY")
	os.Unsetenv("AWS_ACCESS_KEY_ID")
	os.Unsetenv("AWS_SECRET_ACCESS_KEY")
	defer func() {
		if prevAK != "" {
			os.Setenv("AWS_ACCESS_KEY_ID", prevAK)
		}
		if prevSK != "" {
			os.Setenv("AWS_SECRET_ACCESS_KEY", prevSK)
		}
	}()
	_, err := builtinS3Get(nil, []Value{StringValue("bucket"), StringValue("key.txt")})
	if err == nil || !strings.Contains(err.Error(), "AWS_ACCESS_KEY_ID") {
		t.Errorf("expected creds error, got %v", err)
	}
}

func TestS3PresignReturnsURL(t *testing.T) {
	os.Setenv("AWS_ACCESS_KEY_ID", "AKIAIOSFODNN7EXAMPLE")
	os.Setenv("AWS_SECRET_ACCESS_KEY", "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY")
	defer os.Unsetenv("AWS_ACCESS_KEY_ID")
	defer os.Unsetenv("AWS_SECRET_ACCESS_KEY")

	v, err := builtinS3Presign(nil, []Value{StringValue("my-bucket"), StringValue("file.txt")})
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	if v.Kind != KindString {
		t.Fatalf("got %v", v)
	}
	for _, want := range []string{
		"X-Amz-Algorithm=AWS4-HMAC-SHA256",
		"X-Amz-Credential=AKIAIOSFODNN7EXAMPLE",
		"X-Amz-Expires=3600",
		"X-Amz-Signature=",
		"my-bucket/file.txt",
	} {
		if !strings.Contains(v.String, want) {
			t.Errorf("presign URL missing %q in %s", want, v.String)
		}
	}
}
