package netutil

import (
	"net"
	neturl "net/url"
	"strings"
)

// NormalizeAgentBaseURL ensures agent URL has scheme and port.
// If port is missing, defaults to 9000 (nodax-server default).
func NormalizeAgentBaseURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}

	u, err := neturl.Parse(s)
	if err != nil || u.Host == "" {
		return strings.TrimRight(s, "/")
	}

	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "9000"
	}
	u.Host = net.JoinHostPort(host, port)
	u.Path = strings.TrimRight(u.Path, "/")
	if u.Path == "/" {
		u.Path = ""
	}
	return u.String()
}
