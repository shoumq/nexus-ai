package llm

import "net/http"

type HFConfig struct {
	URL          string
	Token        string
	Model        string
	SystemPrompt string
	HTTPClient   *http.Client
}

type HFClient struct {
	url        string
	token      string
	model      string
	system     string
	httpClient *http.Client
}
