package oauth

import (
	"html/template"
	"net/http"
)

// consentTemplate renders the authorization endpoint's HTML form. It ships
// with no third-party script or stylesheet -- see design.md's Risks /
// Trade-offs on the consent screen being a new place a TrueNAS API key is
// typed into a browser.
var consentTemplate = template.Must(template.New("consent").Parse(`<!doctype html>
<html>
<head>
<meta charset="utf-8">
<title>Connect to truenas-mcp</title>
<style>
body { font-family: system-ui, sans-serif; max-width: 32rem; margin: 3rem auto; padding: 0 1rem; color: #1a1a1a; }
p.client { color: #555; }
.destination { background: #f4f4f4; border-radius: 0.25rem; padding: 0.75rem 1rem; margin-top: 1rem; word-break: break-all; }
.destination code { font-size: 0.95rem; }
label { display: block; margin-top: 1rem; font-weight: bold; }
input { width: 100%; padding: 0.5rem; margin-top: 0.25rem; box-sizing: border-box; font-size: 1rem; }
button { margin-top: 1.5rem; padding: 0.6rem 1.2rem; font-size: 1rem; }
.error { color: #b00020; margin-top: 1rem; }
</style>
</head>
<body>
<h1>Connect to truenas-mcp</h1>
<p class="client">{{.ClientName}} is requesting access to a TrueNAS instance through your own credential. Nothing is granted until you submit a TrueNAS API key below.</p>
<p class="destination">Access will be delivered to:<br><code>{{.RedirectURI}}</code></p>
<p class="client">Only continue if you recognize this application and destination. Anyone can register a client with any name -- the destination above is the only thing this page can verify.</p>
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}
<form method="post" action="{{.Action}}">
<input type="hidden" name="client_id" value="{{.ClientID}}">
<input type="hidden" name="redirect_uri" value="{{.RedirectURI}}">
<input type="hidden" name="state" value="{{.State}}">
<input type="hidden" name="code_challenge" value="{{.CodeChallenge}}">
<input type="hidden" name="code_challenge_method" value="{{.CodeChallengeMethod}}">
<label for="username">TrueNAS username (optional, shown only to you)</label>
<input type="text" id="username" name="username" autocomplete="off" value="{{.Username}}">
<label for="api_key">TrueNAS API key</label>
<input type="password" id="api_key" name="api_key" autocomplete="off" required>
<button type="submit">Authorize</button>
</form>
</body>
</html>
`))

type consentView struct {
	Action              string
	ClientID            string
	ClientName          string
	RedirectURI         string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
	Username            string
	Error               string
}

func renderConsentForm(w http.ResponseWriter, req authorizeRequest, username, errMsg string) {
	name := req.ClientMeta.ClientName
	if name == "" {
		name = "This application"
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = consentTemplate.Execute(w, consentView{
		Action:              AuthorizePath,
		ClientID:            req.ClientID,
		ClientName:          name,
		RedirectURI:         req.RedirectURI,
		State:               req.State,
		CodeChallenge:       req.CodeChallenge,
		CodeChallengeMethod: req.CodeChallengeMethod,
		Username:            username,
		Error:               errMsg,
	})
}
