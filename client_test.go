// Copyright 2026 Spectrayan — Proprietary

package ebbmem

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func mockScript(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		candidate := filepath.Join(dir, "contract", "mock_server.py")
		if _, statErr := os.Stat(candidate); statErr == nil {
			return candidate
		}
		nested := filepath.Join(dir, "sdks", "contract", "mock_server.py")
		if _, statErr := os.Stat(nested); statErr == nil {
			return nested
		}
		dir = filepath.Dir(dir)
	}
	t.Skip("contract mock is not in this module")
	return ""
}

func startMock(t *testing.T) (string, *exec.Cmd) {
	t.Helper()
	cmd := exec.Command("python3", mockScript(t))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	origin, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		_ = cmd.Process.Kill()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	return origin[:len(origin)-1], cmd
}

func TestExplicitArgumentWins(t *testing.T) {
	t.Setenv("EBBMEM_ORIGIN", "https://from-env.example")
	t.Setenv("EBBMEM_TOKEN", "env-secret")
	client, err := NewClient(WithBaseURL("https://explicit.example"), WithToken("secret-token"))
	if err != nil {
		t.Fatal(err)
	}
	if client.baseURL != "https://explicit.example" {
		t.Fatalf("base %s", client.baseURL)
	}
	rendered := client.String()
	if rendered == "" || strings.Contains(rendered, "secret-token") || strings.Contains(rendered, "env-secret") {
		t.Fatalf("token leaked: %s", rendered)
	}
}

func TestMissingOrigin(t *testing.T) {
	t.Setenv("EBBMEM_ORIGIN", "")
	if _, err := NewClient(); err == nil {
		t.Fatal("expected missing origin")
	}
}

func TestRetryPolicy(t *testing.T) {
	seconds := 0.0
	limited := &RateLimitError{Base: &Error{Status: 429, Code: "EBB-429-RATE-LIMIT", RetryAfter: &seconds}}
	if !retryable(limited, true) {
		t.Fatal("429 should retry")
	}
	if retryable(limited, false) {
		t.Fatal("non-idempotent 429 should not retry")
	}
	invalid := &ValidationError{Base: &Error{Status: 422}}
	if retryable(invalid, true) {
		t.Fatal("422 should not retry")
	}
	if SDKVersion != "0.2.0" {
		t.Fatal(SDKVersion)
	}
}

func TestContract(t *testing.T) {
	origin, _ := startMock(t)
	client, err := NewClient(WithBaseURL(origin), WithToken("contract-token"), WithMaxRetries(2))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	stored, err := client.Memory.Remember(ctx, "hello", nil, nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if stored == nil || stored.Status == nil || *stored.Status != "stored" || stored.MemoryId == nil || stored.MemoryId.String == nil {
		t.Fatalf("remember %#v", stored)
	}
	recalled, err := client.Memory.Recall(ctx, "hello", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if recalled.GetQuery() != "hello" || recalled.GetCount() != 1 || recalled.GetProfile() != "BALANCED" || len(recalled.GetResults()) == 0 {
		t.Fatalf("recall %#v", recalled)
	}
	var texts []string
	for row, rowErr := range client.Memory.List(ctx, 1) {
		if rowErr != nil {
			t.Fatal(rowErr)
		}
		texts = append(texts, row.GetText())
	}
	if len(texts) != 2 || texts[0] != "alpha" || texts[1] != "beta" {
		t.Fatalf("list %v", texts)
	}
	_, err = client.Memory.Get(ctx, "missing")
	var missing *NotFoundError
	if !errors.As(err, &missing) || missing.Base.Status != 404 || missing.Base.Code != "EBB-404-NOT-FOUND" || missing.Base.RequestID != "req-404" {
		t.Fatalf("404 %#v", err)
	}
	var base *Error
	if !errors.As(err, &base) {
		t.Fatal("errors.As *Error")
	}
	status, err := client.Memory.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.GetEngine() != "ebb" {
		t.Fatalf("status %#v", status)
	}
	bare, err := NewClient(WithBaseURL(origin), WithToken(""), WithMaxRetries(0))
	if err != nil {
		t.Fatal(err)
	}
	_, err = bare.Memory.Remember(ctx, "nope", nil, nil, nil, "")
	var denied *AuthError
	if !errors.As(err, &denied) || denied.Base.Status != 401 {
		t.Fatalf("401 %#v", err)
	}
}

func TestLiveSmoke(t *testing.T) {
	if os.Getenv("EBBMEM_ORIGIN") == "" {
		t.Skip("EBBMEM_ORIGIN is not set")
	}
	client, err := NewClient(WithMaxRetries(2))
	if err != nil {
		t.Fatal(err)
	}
	vector := make([]float32, 384)
	for i := range vector {
		vector[i] = 0.01
	}
	stored, err := client.Memory.Remember(context.Background(), "sdk smoke fact", vector, nil, nil, "")
	if err != nil || stored == nil || stored.Status == nil || *stored.Status == "" {
		t.Fatal(err)
	}
	recalled, err := client.Memory.Recall(context.Background(), "sdk smoke fact", nil, vector)
	if err != nil || recalled.GetQuery() == "" {
		t.Fatal(err)
	}
}
