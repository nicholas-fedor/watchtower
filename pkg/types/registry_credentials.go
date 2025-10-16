package types

// RegistryCredentials holds basic auth credentials.
type RegistryCredentials struct {
	Username string `json:"username"` // Registry username.
	Password string `json:"password"` // Registry token or password.
}
