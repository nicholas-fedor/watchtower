package types

// GitImage is a non-secret per-image Git association.
type GitImage struct {
	Repo       string `json:"repo"`
	Ref        string `json:"ref"`
	Policy     string `json:"policy"`
	Dockerfile string `json:"dockerfile,omitempty"`
	Context    string `json:"context,omitempty"`
}
