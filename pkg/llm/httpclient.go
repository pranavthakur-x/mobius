package llm

import (
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const maxErrorBodyBytes = 64 * 1024

var defaultHTTPClient = newHTTPClient()

func newHTTPClient() *http.Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   8,
		ForceAttemptHTTP2:     true,
	}
	return &http.Client{
		Transport: transport,
	}
}

func readErrorBody(r io.Reader) string {
	if r == nil {
		return ""
	}
	data, err := io.ReadAll(io.LimitReader(r, maxErrorBodyBytes))
	if err != nil && len(data) == 0 {
		return ""
	}
	return strings.TrimSpace(string(data))
}
