package oauth

// Credential is the TrueNAS identity a code or token is bound to.
//
// Username is optional and carried only so the resource owner can recognize
// which account a consent screen or client list refers to. Authentication is
// entirely by APIKey: truenas.Client.Login takes no username, and neither
// does anything downstream of it.
type Credential struct {
	Username string `json:"username,omitempty"`
	APIKey   string `json:"api_key"`
}
