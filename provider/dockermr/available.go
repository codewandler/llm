package dockermr

import (
	"context"
	"net/http"
	"time"
)

// dmrProbeTimeout is how long Available() waits for the local DMR endpoint
// to respond before concluding it is not running. 500 ms is generous for a
// loopback TCP connection but conservative enough to avoid blocking auto.New()
// on machines without DMR.
const dmrProbeTimeout = 500 * time.Millisecond

// Available reports whether a Docker Model Runner server is reachable at
// DefaultBaseURL (http://localhost:12434).
//
// It sends a GET request to the model list endpoint and returns true only
// when the response status is 200 OK. This makes the probe dual-purpose:
// it confirms both that the TCP port is open and that the API is operational.
//
// The sharedTransport parameter allows callers to reuse an existing
// http.Transport (e.g. to honour proxy configuration or custom TLS settings).
// Pass nil to use http.DefaultTransport.
//
// This function is called by provider/auto during auto-detection. The timeout
// ensures it does not block auto.New() on machines without Docker Desktop.
func Available(sharedTransport http.RoundTripper) bool {
	c := &http.Client{
		Transport: sharedTransport, // nil falls back to http.DefaultTransport
		Timeout:   dmrProbeTimeout,
	}

	probeURL := DefaultBaseURL + "/engines/" + defaultEngine + "/v1/models"
	ctx, cancel := context.WithTimeout(context.Background(), dmrProbeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return false
	}

	resp, err := c.Do(req)
	if err != nil {
		return false
	}
	//nolint:errcheck
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
