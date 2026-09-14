package oauth

import (
	"net/http"
	"net/url"
	"strings"
	"time"
)

// authorizeRequest is a validated authorization request: everything needed
// to render the consent form and, on submission, to seal an authorization
// code.
type authorizeRequest struct {
	ClientID            string
	RedirectURI         string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
	ClientMeta          ClientMetadata
}

// parseAuthorizeRequest validates values against the requirements every
// authorization request must meet before the consent form can be trusted to
// render: a client_id that decodes and verifies, a redirect_uri registered
// for it, and an S256 PKCE challenge. Run identically for the initial GET
// and the consent form's POST, since a POST's hidden fields are exactly as
// untrusted as a GET's query parameters.
func (h *Handler) parseAuthorizeRequest(values url.Values) (authorizeRequest, string) {
	req := authorizeRequest{
		ClientID:            values.Get("client_id"),
		RedirectURI:         values.Get("redirect_uri"),
		State:               values.Get("state"),
		CodeChallenge:       values.Get("code_challenge"),
		CodeChallengeMethod: values.Get("code_challenge_method"),
	}

	if rt := values.Get("response_type"); rt != "" && rt != "code" {
		return req, `response_type must be "code"`
	}
	if req.ClientID == "" {
		return req, "client_id is required"
	}

	meta, err := DecodeClientID(h.cfg.Keys, req.ClientID)
	if err != nil {
		return req, "client_id is not recognized; register a client first"
	}
	req.ClientMeta = meta

	if req.RedirectURI == "" {
		return req, "redirect_uri is required"
	}
	if err := meta.ValidateRedirectURI(req.RedirectURI); err != nil {
		return req, "redirect_uri is not registered for this client"
	}

	if req.CodeChallengeMethod != "S256" || req.CodeChallenge == "" {
		return req, "a code_challenge using code_challenge_method=S256 (PKCE) is required"
	}

	return req, ""
}

func (h *Handler) handleAuthorizeGet(w http.ResponseWriter, r *http.Request) {
	req, problem := h.parseAuthorizeRequest(r.URL.Query())
	if problem != "" {
		writeOAuthProblem(w, http.StatusBadRequest, problem)
		return
	}
	renderConsentForm(w, req, "", "")
}

func (h *Handler) handleAuthorizePost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeOAuthProblem(w, http.StatusBadRequest, "malformed form body")
		return
	}

	req, problem := h.parseAuthorizeRequest(r.PostForm)
	if problem != "" {
		writeOAuthProblem(w, http.StatusBadRequest, problem)
		return
	}

	username := strings.TrimSpace(r.PostFormValue("username"))
	apiKey := strings.TrimSpace(r.PostFormValue("api_key"))
	if apiKey == "" {
		renderConsentForm(w, req, username, "a TrueNAS API key is required")
		return
	}

	if err := h.cfg.Validator.Validate(r.Context(), apiKey); err != nil {
		// Never echo the submitted key back, in the form or anywhere else:
		// a rejected credential is still a credential.
		renderConsentForm(w, req, username, "TrueNAS rejected that username or API key")
		return
	}

	now := time.Now()
	code, err := EncodeCode(h.cfg.Keys, CodePayload{
		Credential:    Credential{Username: username, APIKey: apiKey},
		ClientID:      req.ClientID,
		RedirectURI:   req.RedirectURI,
		CodeChallenge: req.CodeChallenge,
	}, now)
	if err != nil {
		writeOAuthProblem(w, http.StatusInternalServerError, "could not issue an authorization code")
		return
	}

	redirect, err := url.Parse(req.RedirectURI)
	if err != nil {
		writeOAuthProblem(w, http.StatusInternalServerError, "redirect_uri is not a valid URL")
		return
	}
	q := redirect.Query()
	q.Set("code", code)
	if req.State != "" {
		q.Set("state", req.State)
	}
	redirect.RawQuery = q.Encode()

	http.Redirect(w, r, redirect.String(), http.StatusFound)
}
