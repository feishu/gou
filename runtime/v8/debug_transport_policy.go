package v8

import (
	"net/http"
	"net/url"
	"strings"
)

type debugTracePolicy struct {
	Enabled bool
	Path    string
}

type debugTransportPolicy struct {
	Host                string
	Port                int
	AllowAnyOrigin      bool
	ExposeSourceContent bool
	ReplaceSession      bool
	Trace               debugTracePolicy
}

func currentDebugTransportPolicy(inspect Inspect) debugTransportPolicy {
	host := inspect.Host
	if host == "" {
		host = "127.0.0.1"
	}
	port := inspect.Port
	if port == 0 {
		port = 9229
	}
	tracePath := inspect.TracePath
	if tracePath == "" {
		tracePath = "/tmp/cdp_trace.log"
	}
	exposeSourceContent := true
	if inspect.ExposeSourceContent != nil {
		exposeSourceContent = *inspect.ExposeSourceContent
	}

	return debugTransportPolicy{
		Host:                host,
		Port:                port,
		AllowAnyOrigin:      false,
		ExposeSourceContent: exposeSourceContent,
		ReplaceSession:      true,
		Trace:               debugTracePolicy{Enabled: inspect.Trace, Path: tracePath},
	}
}

func (policy debugTransportPolicy) CheckOrigin(r *http.Request) bool {
	if policy.AllowAnyOrigin {
		return true
	}

	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	switch u.Scheme {
	case "devtools", "vscode-file":
		return true
	case "http", "https":
		host := u.Hostname()
		return host == "127.0.0.1" || host == "localhost" || host == "::1"
	default:
		return false
	}
}
