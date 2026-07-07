package oteldiff

import (
	"net/url"
	"strconv"
	"strings"
)

func extractEndpoints(g graph) map[string]struct {
	component ComponentRef
	value     EndpointValue
} {
	out := map[string]struct {
		component ComponentRef
		value     EndpointValue
	}{}
	for _, cat := range componentCategories {
		for _, id := range sortedCompKeys(g.components[cat]) {
			c := g.components[cat][id]
			walk(c.cfg, c.ref.Path, func(path string, v any) {
				key := lastPathPart(path)
				if isEndpointKey(key) {
					if s, ok := scalarString(v); ok {
						out[path] = struct {
							component ComponentRef
							value     EndpointValue
						}{c.ref, parseEndpoint(s, c.ref)}
					}
				}
			})
		}
	}
	return out
}
func isEndpointKey(k string) bool {
	switch strings.ToLower(k) {
	case "endpoint", "url", "server", "address", "listen_address":
		return true
	default:
		return false
	}
}
func endpointKind(ref ComponentRef) string {
	if strings.HasPrefix(ref.Type, "otlphttp") {
		return "otlp_http"
	}
	if strings.HasPrefix(ref.Type, "otlp") {
		return "otlp_grpc"
	}
	return "generic"
}
func endpointRisk(ref ComponentRef, b, t EndpointValue) Risk {
	if strings.HasPrefix(ref.Type, "otlp") && ref.Category == "exporters" && (b.Host != t.Host || b.Port != t.Port || b.Scheme != t.Scheme) {
		return RiskHigh
	}
	if ref.Category == "receivers" {
		return RiskMedium
	}
	if b.Scheme == "https" && t.Scheme == "http" {
		return RiskHigh
	}
	return RiskMedium
}
func endpointRules(ref ComponentRef, b, t EndpointValue) []string {
	var r []string
	if strings.HasPrefix(ref.Type, "otlp") && ref.Category == "exporters" && (b.Host != t.Host || b.Port != t.Port || b.Scheme != t.Scheme) {
		r = appendRule(r, "otlp_endpoint_changed")
	}
	if ref.Category == "receivers" {
		r = appendRule(r, "receiver_endpoint_exposure_changed")
	}
	if b.Scheme == "https" && t.Scheme == "http" {
		r = appendRule(r, "transport_security_weakened")
	}
	return r
}
func parseEndpoint(raw string, ref ComponentRef) EndpointValue {
	sanitized := sanitizeEndpoint(raw)
	e := EndpointValue{Raw: sanitized, Normalized: sanitized}
	parse := sanitized
	if !strings.Contains(parse, "://") {
		if strings.HasPrefix(ref.Type, "otlphttp") {
			parse = "http://" + parse
		} else {
			parse = "grpc://" + parse
		}
	}
	if u, err := url.Parse(parse); err == nil {
		e.Scheme = u.Scheme
		e.Host = strings.ToLower(u.Hostname())
		if p := u.Port(); p != "" {
			if n, err := strconv.Atoi(p); err == nil {
				e.Port = n
			}
		}
		e.Path = u.EscapedPath()
		e.Normalized = normalizeURL(u)
		e.Insecure = (u.Scheme == "http" || u.Scheme == "grpc")
		e.TLSEnabled = (u.Scheme == "https")
	}
	return e
}
func sanitizeEndpoint(raw string) string {
	raw = strings.TrimSpace(raw)
	parse := raw
	added := false
	if !strings.Contains(parse, "://") {
		parse = "https://" + parse
		added = true
	}
	u, err := url.Parse(parse)
	if err != nil {
		return redactSecretLikeString(raw)
	}
	u.User = nil
	q := u.Query()
	for k := range q {
		if isSecretKey(k) {
			q.Set(k, MaskedValue)
		}
	}
	u.RawQuery = q.Encode()
	u.Fragment = ""
	s := u.String()
	if added {
		s = strings.TrimPrefix(s, "https://")
	}
	return s
}
func normalizeURL(u *url.URL) string {
	cp := *u
	cp.Host = strings.ToLower(cp.Host)
	cp.User = nil
	cp.Fragment = ""
	return strings.TrimRight(cp.String(), "/")
}
