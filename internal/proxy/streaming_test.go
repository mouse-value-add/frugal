package proxy

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/frugalsh/frugal/internal/provider"
	"github.com/frugalsh/frugal/internal/types"
)

func TestStreamResponse_EmitsDoneOnChannelClose(t *testing.T) {
	w := httptest.NewRecorder()
	ch := make(chan provider.StreamChunk, 1)
	ch <- provider.StreamChunk{Data: &types.ChatCompletionChunk{ID: "chunk-1"}}
	close(ch)

	if err := streamResponse(w, ch); err != nil {
		t.Fatalf("streamResponse returned error: %v", err)
	}

	body := w.Body.String()
	if !strings.Contains(body, "data: [DONE]\n\n") {
		t.Fatalf("expected [DONE] terminator, got %q", body)
	}
}

func TestStreamResponse_EmitsErrorEventAndReturnsErr(t *testing.T) {
	w := httptest.NewRecorder()
	targetErr := errors.New("upstream exploded")
	ch := make(chan provider.StreamChunk, 1)
	ch <- provider.StreamChunk{Err: targetErr}
	close(ch)

	err := streamResponse(w, ch)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, targetErr) {
		t.Fatalf("expected %v, got %v", targetErr, err)
	}

	body := w.Body.String()
	if !strings.Contains(body, "upstream exploded") {
		t.Fatalf("expected error payload in SSE stream, got %q", body)
	}
	if strings.Contains(body, "data: [DONE]\n\n") {
		t.Fatalf("did not expect [DONE] after error, got %q", body)
	}
}
