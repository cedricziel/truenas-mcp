package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testMetadataClientID = "https://client.example/oauth/client.json"

type metadataRoundTripFunc func(*http.Request) (*http.Response, error)

func (f metadataRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func clientMetadataJSON(t *testing.T, clientID string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"client_id": clientID, "client_name": "Metadata Client", "redirect_uris": []string{testRedirectURI}, "token_endpoint_auth_method": "none", "grant_types": []string{"authorization_code"}, "response_types": []string{"code"}})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func metadataResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func metadataHandler(t *testing.T, body string) (*Handler, *http.ServeMux) {
	t.Helper()
	h, mux := newTestMux(t, fakeValidator{acceptedKey: "1-goodkey"}, time.Hour)
	h.clients.client.Transport = metadataRoundTripFunc(func(*http.Request) (*http.Response, error) { return metadataResponse(body), nil })
	return h, mux
}

func cimdAuthorizeValues() url.Values {
	_, challenge := pkcePair()
	return url.Values{"response_type": {"code"}, "client_id": {testMetadataClientID}, "redirect_uri": {testRedirectURI}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}, "state": {"unchanged-state"}, "api_key": {"1-goodkey"}}
}

func TestCIMDAuthorizationAndTokenExchangePreserveClientAndPKCEBinding(t *testing.T) {
	h, mux := metadataHandler(t, clientMetadataJSON(t, testMetadataClientID))
	get := httptest.NewRecorder()
	mux.ServeHTTP(get, httptest.NewRequest(http.MethodGet, AuthorizePath+"?"+cimdAuthorizeValues().Encode(), nil))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), "Metadata Client") || !strings.Contains(get.Body.String(), testMetadataClientID) {
		t.Fatalf("CIMD consent = %d: %s", get.Code, get.Body)
	}
	auth := doAuthorizePost(t, mux, cimdAuthorizeValues())
	if auth.Code != http.StatusFound {
		t.Fatalf("CIMD authorize = %d: %s", auth.Code, auth.Body)
	}
	redirect, err := url.Parse(auth.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if redirect.Query().Get("iss") != h.issuerBase() || redirect.Query().Get("state") != "unchanged-state" {
		t.Fatalf("CIMD redirect = %s", redirect)
	}
	code := redirect.Query().Get("code")
	payload, err := DecodeCode(h.cfg.Keys, code, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if payload.ClientID != testMetadataClientID || payload.RedirectURI != testRedirectURI {
		t.Fatalf("code binding = %+v", payload)
	}
	verifier, _ := pkcePair()
	wrongClient := doTokenPost(t, mux, url.Values{"grant_type": {"authorization_code"}, "client_id": {"https://client.example:443/oauth/client.json"}, "redirect_uri": {testRedirectURI}, "code": {code}, "code_verifier": {verifier}})
	if wrongClient.Code != http.StatusBadRequest {
		t.Fatalf("equivalent URL redeemed another exact client ID: %d", wrongClient.Code)
	}
	wrongVerifier := doTokenPost(t, mux, url.Values{"grant_type": {"authorization_code"}, "client_id": {testMetadataClientID}, "redirect_uri": {testRedirectURI}, "code": {code}, "code_verifier": {"wrong"}})
	if wrongVerifier.Code != http.StatusBadRequest {
		t.Fatalf("CIMD PKCE was bypassed: %d", wrongVerifier.Code)
	}
	token := doTokenPost(t, mux, url.Values{"grant_type": {"authorization_code"}, "client_id": {testMetadataClientID}, "redirect_uri": {testRedirectURI}, "code": {code}, "code_verifier": {verifier}})
	if token.Code != http.StatusOK {
		t.Fatalf("CIMD token exchange = %d: %s", token.Code, token.Body)
	}
	var response tokenResponse
	if err := json.Unmarshal(token.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeAccessToken(h.cfg.Keys, response.AccessToken, time.Now())
	if err != nil || decoded.Credential.APIKey != "1-goodkey" {
		t.Fatalf("CIMD access token lost caller credential: %v", err)
	}
}

func TestCIMDConsentPOSTRechecksRegisteredRedirect(t *testing.T) {
	_, mux := metadataHandler(t, clientMetadataJSON(t, testMetadataClientID))
	form := cimdAuthorizeValues()
	form.Set("redirect_uri", "https://client.example/attacker-callback")
	rec := doAuthorizePost(t, mux, form)
	if rec.Code != http.StatusBadRequest || rec.Header().Get("Location") != "" {
		t.Fatalf("unregistered CIMD redirect accepted: %d %s", rec.Code, rec.Body)
	}
}

func TestCIMDRejectsInvalidDocumentsWithoutRedirecting(t *testing.T) {
	cases := map[string]string{
		"wrong client":            "{\"client_id\":\"https://client.example:443/oauth/client.json\",\"client_name\":\"Client\",\"redirect_uris\":[\"" + testRedirectURI + "\"]}",
		"missing name":            "{\"client_id\":\"" + testMetadataClientID + "\",\"redirect_uris\":[\"" + testRedirectURI + "\"]}",
		"missing redirects":       "{\"client_id\":\"" + testMetadataClientID + "\",\"client_name\":\"Client\"}",
		"malformed JSON":          "{",
		"trailing JSON":           clientMetadataJSON(t, testMetadataClientID) + "{}",
		"unsafe redirect":         "{\"client_id\":\"" + testMetadataClientID + "\",\"client_name\":\"Client\",\"redirect_uris\":[\"https:///callback\"]}",
		"fragment redirect":       "{\"client_id\":\"" + testMetadataClientID + "\",\"client_name\":\"Client\",\"redirect_uris\":[\"https://client.example/callback#fragment\"]}",
		"private key published":   strings.TrimSuffix(clientMetadataJSON(t, testMetadataClientID), "}") + `,"jwks":{"keys":[{"kty":"RSA","d":"private"}]}}`,
		"conflicting key sources": strings.TrimSuffix(clientMetadataJSON(t, testMetadataClientID), "}") + `,"jwks":{"keys":[]},"jwks_uri":"https://client.example/keys"}`,
		"confidential client":     strings.Replace(clientMetadataJSON(t, testMetadataClientID), "\"none\"", "\"private_key_jwt\"", 1),
		"secret published":        strings.TrimSuffix(clientMetadataJSON(t, testMetadataClientID), "}") + ",\"client_secret\":\"secret\"}",
		"unsupported grant":       strings.Replace(clientMetadataJSON(t, testMetadataClientID), "authorization_code", "client_credentials", 1),
		"unsupported response":    strings.Replace(clientMetadataJSON(t, testMetadataClientID), "[\"code\"]", "[\"token\"]", 1),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, mux := metadataHandler(t, body)
			rec := doAuthorizePost(t, mux, cimdAuthorizeValues())
			if rec.Code != http.StatusBadRequest || rec.Header().Get("Location") != "" {
				t.Fatalf("invalid metadata accepted: %d %s", rec.Code, rec.Body)
			}
		})
	}
}

func TestCIMDRejectsUnsafeClientURLsBeforeFetching(t *testing.T) {
	urls := []string{"http://client.example/client.json", "https://client.example", "https://client.example/", "https://client.example/client.json#", "https://client.example/a/../client.json", "https://client.example/a/%2e%2e/client.json", "https://user:password@client.example/client.json", "https://client.example:99999/client.json", "https://127.0.0.1/client.json", "https://localhost/client.json", "https://sub.localhost/client.json", "https://10.0.0.1/client.json", "https://[::1]/client.json", "https://[::ffff:127.0.0.1]/client.json", "https://[fe80::1%25en0]/client.json"}
	for _, clientID := range urls {
		t.Run(clientID, func(t *testing.T) {
			f := newClientMetadataFetcher()
			f.client.Transport = metadataRoundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("unsafe URL reached network transport")
				return nil, errors.New("unreachable")
			})
			if _, err := f.fetch(context.Background(), clientID); err == nil {
				t.Fatal("unsafe URL accepted")
			}
		})
	}
}

func TestCIMDRejectsMixedOrPrivateDNSBeforeDial(t *testing.T) {
	cases := map[string][]netip.Addr{
		"private IPv4":             {netip.MustParseAddr("10.0.0.1")},
		"mixed public and private": {netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("127.0.0.1")},
		"IPv6 unique local":        {netip.MustParseAddr("fd00::1")},
		"documentation":            {netip.MustParseAddr("192.0.2.1")},
		"carrier NAT":              {netip.MustParseAddr("100.64.0.1")},
		"IPv6 NAT64":               {netip.MustParseAddr("64:ff9b::7f00:1")},
	}
	for name, addresses := range cases {
		t.Run(name, func(t *testing.T) {
			dial := metadataDialContext(func(context.Context, string, string) ([]netip.Addr, error) { return addresses, nil }, func(context.Context, string, string) (net.Conn, error) {
				t.Fatal("non-public DNS address reached dialer")
				return nil, errors.New("unreachable")
			})
			if _, err := dial(context.Background(), "tcp", "client.example:443"); err == nil {
				t.Fatal("unsafe DNS accepted")
			}
		})
	}
}

func TestCIMDDialsValidatedIPAddressWithoutResolvingAgain(t *testing.T) {
	expected := errors.New("test dial result")
	dial := metadataDialContext(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
	}, func(_ context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" || address != "8.8.8.8:443" {
			t.Fatalf("dial used unvalidated destination %s %s", network, address)
		}
		return nil, expected
	})
	if _, err := dial(context.Background(), "tcp", "client.example:443"); !errors.Is(err, expected) {
		t.Fatalf("dial error = %v", err)
	}
}

func TestCIMDFetchForwardsNoCredentialsAndDoesNotFollowRedirects(t *testing.T) {
	var requests atomic.Int32
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("X-TrueNAS-API-Key") != "" {
			t.Error("credentials forwarded to metadata endpoint")
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept = %q", r.Header.Get("Accept"))
		}
		http.Redirect(w, r, "https://127.0.0.1/private", http.StatusFound)
	}))
	defer upstream.Close()
	h, mux := newTestMux(t, fakeValidator{}, time.Hour)
	target, _ := url.Parse(upstream.URL)
	testTransport := upstream.Client().Transport
	h.clients.client.Transport = metadataRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		forwarded := request.Clone(request.Context())
		rewritten := *request.URL
		rewritten.Host = target.Host
		forwarded.URL = &rewritten
		return testTransport.RoundTrip(forwarded)
	})
	request := httptest.NewRequest(http.MethodGet, AuthorizePath+"?"+cimdAuthorizeValues().Encode(), nil)
	request.Header.Set("Authorization", "Bearer must-not-leak")
	request.Header.Set("Cookie", "credential=must-not-leak")
	request.Header.Set("X-TrueNAS-API-Key", "must-not-leak")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, request)
	if rec.Code != http.StatusBadRequest || requests.Load() != 1 {
		t.Fatalf("metadata redirect followed or accepted: status=%d requests=%d", rec.Code, requests.Load())
	}
}

func TestCIMDFetchRejectsOversizedResponsesAndUpstreamErrors(t *testing.T) {
	cases := map[string]func(*http.Request) (*http.Response, error){
		"oversized": func(*http.Request) (*http.Response, error) {
			return metadataResponse(strings.Repeat(" ", maxClientMetadataBytes+1)), nil
		},
		"HTTP error": func(*http.Request) (*http.Response, error) {
			response := metadataResponse("error")
			response.StatusCode = http.StatusServiceUnavailable
			return response, nil
		},
		"transport error": func(*http.Request) (*http.Response, error) { return nil, errors.New("upstream unavailable") },
		"wrong content type": func(*http.Request) (*http.Response, error) {
			response := metadataResponse("{}")
			response.Header.Set("Content-Type", "text/html")
			return response, nil
		},
	}
	for name, transport := range cases {
		t.Run(name, func(t *testing.T) {
			f := newClientMetadataFetcher()
			f.client.Transport = metadataRoundTripFunc(transport)
			if _, err := f.fetch(context.Background(), testMetadataClientID); err == nil {
				t.Fatal("bad metadata endpoint accepted")
			}
			if len(f.cache) != 0 {
				t.Fatal("failed response was cached")
			}
		})
	}
}

func TestCIMDFetchTimeoutCancelsUpstreamRequest(t *testing.T) {
	f := newClientMetadataFetcher()
	f.client.Timeout = time.Millisecond
	f.client.Transport = metadataRoundTripFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
	_, err := f.fetch(context.Background(), testMetadataClientID)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error = %v", err)
	}
}

func TestCIMDCacheExpiresWithinMaximumTTL(t *testing.T) {
	f := newClientMetadataFetcher()
	now := time.Unix(1000, 0)
	f.now = func() time.Time { return now }
	var requests int
	f.client.Transport = metadataRoundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		response := metadataResponse(clientMetadataJSON(t, testMetadataClientID))
		response.Header.Set("Cache-Control", "max-age=600")
		return response, nil
	})
	if _, err := f.fetch(context.Background(), testMetadataClientID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.fetch(context.Background(), testMetadataClientID); err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("valid metadata was not reused: requests=%d", requests)
	}
	now = now.Add(maxClientMetadataCacheTTL)
	if _, err := f.fetch(context.Background(), testMetadataClientID); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("metadata cache exceeded maximum TTL: requests=%d", requests)
	}
}

func TestCIMDLoopbackRedirectPortException(t *testing.T) {
	cases := []struct {
		name, registered, requested string
		allowed                     bool
	}{
		{"IPv4 ephemeral port", "http://127.0.0.1/callback", "http://127.0.0.1:49152/callback", true},
		{"IPv6 ephemeral port", "http://[::1]/callback", "http://[::1]:49152/callback", true},
		{"changed explicit port", "http://127.0.0.1:1234/callback?fixed=1", "http://127.0.0.1:49152/callback?fixed=1", true},
		{"localhost stays exact", "http://localhost/callback", "http://localhost:49152/callback", false},
		{"different host", "http://127.0.0.1/callback", "http://127.0.0.2:49152/callback", false},
		{"different path", "http://127.0.0.1/callback", "http://127.0.0.1:49152/other", false},
		{"different query", "http://127.0.0.1/callback?fixed=1", "http://127.0.0.1:49152/callback?fixed=2", false},
		{"different encoded path", "http://127.0.0.1/callback", "http://127.0.0.1:49152/%63allback", false},
		{"fragment", "http://127.0.0.1/callback", "http://127.0.0.1:49152/callback#fragment", false},
		{"HTTPS stays exact", "https://client.example/callback", "https://client.example:49152/callback", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := strings.Replace(clientMetadataJSON(t, testMetadataClientID), testRedirectURI, tc.registered, 1)
			h, mux := metadataHandler(t, body)
			form := cimdAuthorizeValues()
			form.Set("redirect_uri", tc.requested)
			response := doAuthorizePost(t, mux, form)
			if !tc.allowed {
				if response.Code != http.StatusBadRequest {
					t.Fatalf("changed redirect accepted: %d", response.Code)
				}
				return
			}
			if response.Code != http.StatusFound {
				t.Fatalf("native CIMD port was rejected: %d %s", response.Code, response.Body)
			}
			location, err := url.Parse(response.Header().Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			payload, err := DecodeCode(h.cfg.Keys, location.Query().Get("code"), time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if payload.RedirectURI != tc.requested {
				t.Fatalf("code bound registered redirect %q instead of actual callback %q", payload.RedirectURI, tc.requested)
			}
		})
	}
}

func TestCIMDInvalidRedirectMetadataIsNeverCached(t *testing.T) {
	f := newClientMetadataFetcher()
	body := strings.Replace(clientMetadataJSON(t, testMetadataClientID), testRedirectURI, "https:///callback", 1)
	f.client.Transport = metadataRoundTripFunc(func(*http.Request) (*http.Response, error) { return metadataResponse(body), nil })
	if _, err := f.fetch(context.Background(), testMetadataClientID); err == nil {
		t.Fatal("metadata containing hostless HTTPS redirect accepted")
	}
	if len(f.cache) != 0 {
		t.Fatal("invalid redirect metadata was cached")
	}
}

func TestCIMDCacheHonorsNoStoreAndDoesNotCacheFailures(t *testing.T) {
	f := newClientMetadataFetcher()
	var requests int
	f.client.Transport = metadataRoundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			return metadataResponse("{}"), nil
		}
		response := metadataResponse(clientMetadataJSON(t, testMetadataClientID))
		response.Header.Set("Cache-Control", "no-store")
		return response, nil
	})
	if _, err := f.fetch(context.Background(), testMetadataClientID); err == nil {
		t.Fatal("invalid metadata accepted")
	}
	if _, err := f.fetch(context.Background(), testMetadataClientID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.fetch(context.Background(), testMetadataClientID); err != nil {
		t.Fatal(err)
	}
	if requests != 3 || len(f.cache) != 0 {
		t.Fatalf("failure or no-store document cached: requests=%d entries=%d", requests, len(f.cache))
	}
}

func TestCIMDMetadataCacheHasBoundedCardinality(t *testing.T) {
	f := newClientMetadataFetcher()
	f.client.Transport = metadataRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		return metadataResponse(clientMetadataJSON(t, request.URL.String())), nil
	})
	for n := 0; n <= maxClientMetadataCacheEntries; n++ {
		if _, err := f.fetch(context.Background(), testMetadataClientID+"?instance="+strconv.Itoa(n)); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.cache) != maxClientMetadataCacheEntries {
		t.Fatalf("metadata cache entries=%d, limit=%d", len(f.cache), maxClientMetadataCacheEntries)
	}
}

func TestCIMDTokenGrantRejectsUnexpectedPublicClientAuthentication(t *testing.T) {
	_, mux := metadataHandler(t, clientMetadataJSON(t, testMetadataClientID))
	auth := doAuthorizePost(t, mux, cimdAuthorizeValues())
	location, err := url.Parse(auth.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	verifier, _ := pkcePair()
	values := url.Values{"grant_type": {"authorization_code"}, "client_id": {testMetadataClientID}, "redirect_uri": {testRedirectURI}, "code": {location.Query().Get("code")}, "code_verifier": {verifier}, "client_secret": {"not-supported"}}
	response := doTokenPost(t, mux, values)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "invalid_client") {
		t.Fatalf("public-client secret was accepted: %d %s", response.Code, response.Body)
	}
	values["client_secret"] = []string{"", "not-supported"}
	if response := doTokenPost(t, mux, values); response.Code != http.StatusBadRequest {
		t.Fatalf("nonempty repeated client secret was hidden: %d", response.Code)
	}
	values.Del("client_secret")
	request := httptest.NewRequest(http.MethodPost, TokenPath, strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.SetBasicAuth("client", "not-supported")
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || response.Header().Get("WWW-Authenticate") != `Basic realm="truenas-mcp"` {
		t.Fatalf("public-client Basic rejection = %d, challenge=%q", response.Code, response.Header().Get("WWW-Authenticate"))
	}
	values.Set("client_assertion", "not-supported")
	if response := doTokenPost(t, mux, values); response.Code != http.StatusBadRequest {
		t.Fatalf("public-client JWT assertion was accepted: %d", response.Code)
	}
	values.Set("client_secret", "")
	values.Set("client_assertion", "")
	values.Set("client_assertion_type", "")
	if response := doTokenPost(t, mux, values); response.Code != http.StatusOK {
		t.Fatalf("public authentication rejection consumed code: %d %s", response.Code, response.Body)
	}
}
