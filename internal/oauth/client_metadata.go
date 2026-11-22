package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxClientMetadataBytes        = 5 * 1024
	maxClientMetadataCacheEntries = 128
	maxClientMetadataCacheTTL     = 5 * time.Minute
	clientMetadataTimeout         = 5 * time.Second
)

type clientMetadataCacheEntry struct {
	metadata ClientMetadata
	expires  time.Time
}

type clientMetadataFetcher struct {
	client *http.Client
	now    func() time.Time
	mu     sync.Mutex
	cache  map[string]clientMetadataCacheEntry
}

func newClientMetadataFetcher() *clientMetadataFetcher {
	transport := &http.Transport{
		DialContext:            metadataDialContext(net.DefaultResolver.LookupNetIP, (&net.Dialer{Timeout: clientMetadataTimeout}).DialContext),
		TLSHandshakeTimeout:    clientMetadataTimeout,
		ResponseHeaderTimeout:  clientMetadataTimeout,
		MaxResponseHeaderBytes: 16 * 1024,
		MaxIdleConns:           16,
		IdleConnTimeout:        30 * time.Second,
	}
	return &clientMetadataFetcher{
		client: &http.Client{Transport: transport, Timeout: clientMetadataTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		now:    time.Now,
		cache:  make(map[string]clientMetadataCacheEntry),
	}
}

func validateClientMetadataURL(raw string) (*url.URL, error) {
	if len(raw) > maxClientMetadataBytes {
		return nil, errors.New("client_id exceeds metadata size limit")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Opaque != "" || strings.Contains(raw, "#") || parsed.Path == "" || parsed.Path == "/" {
		return nil, errors.New("client_id must be an HTTPS URL with a non-root path and no userinfo or fragment")
	}
	if port := parsed.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return nil, errors.New("client_id has an invalid port")
		}
	}
	for _, segment := range strings.Split(parsed.Path, "/") {
		if segment == "." || segment == ".." {
			return nil, errors.New("client_id must not contain dot path segments")
		}
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.Contains(host, "%") {
		return nil, errors.New("client_id host is not public")
	}
	if ip, err := netip.ParseAddr(host); err == nil && !publicClientMetadataIP(ip) {
		return nil, errors.New("client_id address is not public")
	}
	return parsed, nil
}

// IsGlobalUnicast includes special-purpose networks; CIMD must not reach any IANA special-use address.
var clientMetadataBlockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.31.196.0/24"),
	netip.MustParsePrefix("192.52.193.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("192.175.48.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/96"),
	netip.MustParsePrefix("::ffff:0:0/96"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("100:0:0:1::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("2620:4f:8000::/48"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("5f00::/16"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("fec0::/10"),
}

func publicClientMetadataIP(ip netip.Addr) bool {
	if !ip.IsGlobalUnicast() || ip.Zone() != "" {
		return false
	}
	for _, prefix := range clientMetadataBlockedPrefixes {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

func metadataDialContext(lookup func(context.Context, string, string) ([]netip.Addr, error), dial func(context.Context, string, string) (net.Conn, error)) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		var addresses []netip.Addr
		if ip, err := netip.ParseAddr(host); err == nil {
			addresses = []netip.Addr{ip}
		} else {
			addresses, err = lookup(ctx, "ip", host)
			if err != nil {
				return nil, err
			}
		}
		if len(addresses) == 0 {
			return nil, errors.New("client metadata host has no addresses")
		}
		for _, ip := range addresses {
			if !publicClientMetadataIP(ip) {
				return nil, errors.New("client metadata host resolves to a non-public address")
			}
		}
		var lastErr error
		for _, ip := range addresses {
			conn, err := dial(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		return nil, lastErr
	}
}

func (f *clientMetadataFetcher) fetch(ctx context.Context, clientID string) (ClientMetadata, error) {
	var empty ClientMetadata
	if _, err := validateClientMetadataURL(clientID); err != nil {
		return empty, err
	}
	f.mu.Lock()
	cached, ok := f.cache[clientID]
	if ok && f.now().Before(cached.expires) {
		f.mu.Unlock()
		return cached.metadata, nil
	}
	delete(f.cache, clientID)
	f.mu.Unlock()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, clientID, nil)
	if err != nil {
		return empty, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := f.client.Do(request)
	if err != nil {
		return empty, fmt.Errorf("fetching client metadata: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return empty, errors.New("client metadata endpoint did not return HTTP 200")
	}
	contentType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	isJSON := contentType == "application/json" || strings.HasPrefix(contentType, "application/") && strings.HasSuffix(contentType, "+json")
	if err != nil || !isJSON {
		return empty, errors.New("client metadata must be served as JSON")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxClientMetadataBytes+1))
	if err != nil {
		return empty, err
	}
	if len(raw) > maxClientMetadataBytes {
		return empty, errors.New("client metadata exceeds size limit")
	}
	metadata, err := decodeClientMetadataDocument(raw, clientID)
	if err != nil {
		return empty, err
	}
	now := f.now()
	if ttl := clientMetadataCacheTTL(response.Header, now); ttl > 0 {
		f.mu.Lock()
		for id, entry := range f.cache {
			if !now.Before(entry.expires) {
				delete(f.cache, id)
			}
		}
		if len(f.cache) >= maxClientMetadataCacheEntries {
			for id := range f.cache {
				delete(f.cache, id)
				break
			}
		}
		f.cache[clientID] = clientMetadataCacheEntry{metadata: metadata, expires: now.Add(ttl)}
		f.mu.Unlock()
	}
	return metadata, nil
}

func decodeClientMetadataDocument(raw []byte, clientID string) (ClientMetadata, error) {
	var document struct {
		ClientID                          string          `json:"client_id"`
		ClientName                        string          `json:"client_name"`
		RedirectURIs                      []string        `json:"redirect_uris"`
		TokenEndpointAuthMethod           string          `json:"token_endpoint_auth_method"`
		TokenEndpointAuthMethodsSupported []string        `json:"token_endpoint_auth_methods_supported"`
		GrantTypes                        []string        `json:"grant_types"`
		ResponseTypes                     []string        `json:"response_types"`
		ClientSecret                      json.RawMessage `json:"client_secret"`
		ClientSecretExpiresAt             json.RawMessage `json:"client_secret_expires_at"`
		JWKS                              json.RawMessage `json:"jwks"`
		JWKSURI                           string          `json:"jwks_uri"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return ClientMetadata{}, err
	}
	if document.ClientID != clientID || strings.TrimSpace(document.ClientName) == "" || len(document.RedirectURIs) == 0 {
		return ClientMetadata{}, errors.New("client metadata must contain its exact client_id, a client_name, and redirect_uris")
	}
	if document.ClientSecret != nil || document.ClientSecretExpiresAt != nil || document.TokenEndpointAuthMethod != "" && document.TokenEndpointAuthMethod != "none" {
		return ClientMetadata{}, errors.New("only public OAuth clients without shared secrets are supported")
	}
	if err := validateClientMetadataKeys(document.JWKS, document.JWKSURI); err != nil {
		return ClientMetadata{}, err
	}
	if document.TokenEndpointAuthMethodsSupported != nil && !containsClientMetadataValue(document.TokenEndpointAuthMethodsSupported, "none") {
		return ClientMetadata{}, errors.New("client does not support public authentication")
	}
	for _, method := range document.TokenEndpointAuthMethodsSupported {
		if strings.HasPrefix(method, "client_secret_") {
			return ClientMetadata{}, errors.New("client metadata must not declare shared-secret authentication")
		}
	}
	if document.GrantTypes != nil && !containsClientMetadataValue(document.GrantTypes, "authorization_code") || document.ResponseTypes != nil && !containsClientMetadataValue(document.ResponseTypes, "code") {
		return ClientMetadata{}, errors.New("client does not support authorization code responses")
	}
	for _, redirectURI := range document.RedirectURIs {
		if err := validateRedirectURIScheme(redirectURI); err != nil {
			return ClientMetadata{}, err
		}
	}
	return ClientMetadata{ClientName: document.ClientName, RedirectURIs: document.RedirectURIs, allowLoopbackPort: true}, nil
}

func validateClientMetadataKeys(raw json.RawMessage, uri string) error {
	if raw == nil {
		return nil
	}
	if uri != "" {
		return errors.New("client metadata cannot contain both jwks and jwks_uri")
	}
	var set struct {
		Keys []map[string]json.RawMessage `json:"keys"`
	}
	if err := json.Unmarshal(raw, &set); err != nil {
		return err
	}
	if set.Keys == nil {
		return errors.New("client metadata jwks must contain a keys array")
	}
	for _, key := range set.Keys {
		for _, secret := range []string{"d", "p", "q", "dp", "dq", "qi", "oth", "k"} {
			if _, present := key[secret]; present {
				return errors.New("client metadata must not contain private or symmetric key material")
			}
		}
	}
	return nil
}

func containsClientMetadataValue(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func clientMetadataCacheTTL(header http.Header, now time.Time) time.Duration {
	ttl := time.Minute
	for _, directive := range strings.Split(header.Get("Cache-Control"), ",") {
		key, value, _ := strings.Cut(strings.TrimSpace(directive), "=")
		switch strings.ToLower(key) {
		case "no-store", "no-cache":
			return 0
		case "max-age":
			seconds, err := strconv.ParseInt(strings.Trim(value, "\""), 10, 64)
			if err != nil || seconds <= 0 {
				return 0
			}
			ttl = maxClientMetadataCacheTTL
			if seconds < int64(maxClientMetadataCacheTTL/time.Second) {
				ttl = time.Duration(seconds) * time.Second
			}
		}
	}
	if header.Get("Cache-Control") == "" && header.Get("Expires") != "" {
		expiry, err := http.ParseTime(header.Get("Expires"))
		if err != nil {
			return 0
		}
		ttl = expiry.Sub(now)
	}
	if age, err := strconv.ParseInt(header.Get("Age"), 10, 64); err == nil && age > 0 {
		if age >= int64(ttl/time.Second) {
			return 0
		}
		ttl -= time.Duration(age) * time.Second
	}
	if ttl > maxClientMetadataCacheTTL {
		return maxClientMetadataCacheTTL
	}
	return ttl
}
