package v8

import "net/http"

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
	return debugTransportPolicy{
		Host:                host,
		Port:                port,
		AllowAnyOrigin:      true,
		ExposeSourceContent: true,
		ReplaceSession:      true,
		Trace:               debugTracePolicy{Enabled: true, Path: "/tmp/cdp_trace.log"},
	}
}

func (policy debugTransportPolicy) CheckOrigin(r *http.Request) bool {
	if policy.AllowAnyOrigin {
		return true
	}
	return true
}
