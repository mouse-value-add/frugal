package routing

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"testing"
)

func TestClassifyHTTPStatus(t *testing.T) {
	cases := []struct {
		status int
		want   Kind
	}{
		{200, KindUnknown},
		{301, KindUnknown},
		{400, KindPermanent},
		{401, KindPermanent},
		{402, KindPermanent}, // quota/billing
		{403, KindPermanent},
		{404, KindPermanent},
		{408, KindTransient},
		{422, KindPermanent},
		{429, KindTransient},
		{500, KindTransient},
		{502, KindTransient},
		{503, KindTransient},
		{504, KindTransient},
		{599, KindTransient},
	}
	for _, c := range cases {
		if got := ClassifyHTTPStatus(c.status); got != c.want {
			t.Errorf("ClassifyHTTPStatus(%d) = %v, want %v", c.status, got, c.want)
		}
	}
}

func TestClassifyNetwork(t *testing.T) {
	if got := ClassifyNetwork(nil); got != KindUnknown {
		t.Errorf("nil err: got %v want unknown", got)
	}
	if got := ClassifyNetwork(context.DeadlineExceeded); got != KindTransient {
		t.Errorf("deadline: got %v want transient", got)
	}
	if got := ClassifyNetwork(context.Canceled); got != KindPermanent {
		t.Errorf("canceled: got %v want permanent (caller bailed)", got)
	}
	// url.Error wrapping a timeout-like net.Error
	timeoutErr := &timeoutNetError{}
	wrapped := &url.Error{Op: "Get", URL: "x", Err: timeoutErr}
	if got := ClassifyNetwork(wrapped); got != KindTransient {
		t.Errorf("url.Error timeout: got %v want transient", got)
	}
	// Other net errors default transient.
	dnsErr := &net.OpError{Op: "dial", Err: errors.New("no such host")}
	if got := ClassifyNetwork(dnsErr); got != KindTransient {
		t.Errorf("dns err: got %v want transient", got)
	}
}

func TestIsTransientWithWrappedError(t *testing.T) {
	inner := Transient("tavily", http.StatusServiceUnavailable, fmt.Errorf("upstream"))
	wrapped := fmt.Errorf("frugal__search: %w", inner)
	if !IsTransient(wrapped) {
		t.Errorf("IsTransient should walk the error chain")
	}
	if IsPermanent(wrapped) {
		t.Errorf("IsPermanent should be false for a wrapped transient")
	}
}

func TestErrorFormatting(t *testing.T) {
	e := Transient("tavily", 503, errors.New("upstream blip"))
	want := "tavily: transient: status 503: upstream blip"
	if got := e.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	noStatus := Permanent("tavily", 0, errors.New("empty query"))
	want2 := "tavily: permanent: empty query"
	if got := noStatus.Error(); got != want2 {
		t.Errorf("Error() = %q, want %q", got, want2)
	}
}

// timeoutNetError is a minimal net.Error stub whose Timeout() returns true.
type timeoutNetError struct{}

func (*timeoutNetError) Error() string   { return "i/o timeout" }
func (*timeoutNetError) Timeout() bool   { return true }
func (*timeoutNetError) Temporary() bool { return true }
