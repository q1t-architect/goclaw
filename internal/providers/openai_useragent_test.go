package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestOpenAIProvider_UserAgent verifies that WithUserAgent injects the header
// when set, and leaves the Go default in place when unset. This guards the
// Kimi Code API allowlist path and ensures the change does not leak the UA
// to other providers that should keep the default.
func TestOpenAIProvider_UserAgent(t *testing.T) {
	tests := []struct {
		name      string
		userAgent string
		// wantHeader is the exact value expected; empty means we expect the
		// Go default ("Go-http-client/*") and assert it is NOT our custom UA.
		wantHeader string
	}{
		{name: "default UA when unset", userAgent: "", wantHeader: ""},
		{name: "Kimi CLI UA when set", userAgent: "KimiCLI/1.5", wantHeader: "KimiCLI/1.5"},
		{name: "arbitrary UA passes through", userAgent: "custom-agent/9.9", wantHeader: "custom-agent/9.9"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotUA string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotUA = r.Header.Get("User-Agent")
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id":     "test",
					"object": "chat.completion",
					"choices": []map[string]any{{
						"index":         0,
						"message":       map[string]any{"role": "assistant", "content": "ok"},
						"finish_reason": "stop",
					}},
				})
			}))
			defer srv.Close()

			p := NewOpenAIProvider("test", "key", srv.URL, "test-model")
			if tc.userAgent != "" {
				p.WithUserAgent(tc.userAgent)
			}

			body, err := p.doRequest(context.Background(), map[string]any{"model": "test-model", "messages": []any{}})
			if err != nil {
				t.Fatalf("doRequest: %v", err)
			}
			io.Copy(io.Discard, body)
			body.Close()

			if tc.wantHeader == "" {
				if gotUA == "" {
					t.Fatalf("expected Go default User-Agent, got empty")
				}
				if !strings.HasPrefix(gotUA, "Go-http-client/") {
					t.Fatalf("expected Go default User-Agent prefix, got %q", gotUA)
				}
			} else {
				if gotUA != tc.wantHeader {
					t.Fatalf("User-Agent = %q, want %q", gotUA, tc.wantHeader)
				}
			}
		})
	}
}
