package ai

// Provider is a model service AI Mode can use. To add one, write a Requester
// for its API and add an entry to providers.
type Provider struct {
	ID      string // how it's named in config.json
	Name    string // for people, such as "Anthropic"
	Model   string // recorded with the recommendations it produced
	KeyEnv  string // the environment variable that can hold its API key
	KeyHint string // what a key looks like, for the Settings page
	New     func(apiKey string) Requester
}

// providers lists every model service. The first is the default.
var providers = []Provider{
	{
		ID:      "anthropic",
		Name:    "Anthropic",
		Model:   HaikuModel,
		KeyEnv:  "ANTHROPIC_API_KEY",
		KeyHint: "sk-ant-…",
		New:     func(apiKey string) Requester { return NewClient(apiKey) },
	},
}

// Providers returns every model service, default first.
func Providers() []Provider {
	return providers
}

// FindProvider returns the provider with this ID. Empty means the default, and
// so does an ID this build doesn't know, so a stale setting can't turn AI Mode
// off without saying why.
func FindProvider(id string) Provider {
	for _, p := range providers {
		if p.ID == id {
			return p
		}
	}
	return providers[0]
}
