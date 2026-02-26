package llm

import "net/http"

type AIConfig struct {
	URL          string
	Token        string
	Model        string
	SystemPrompt string
	HTTPClient   *http.Client
}

type AIClient struct {
	url        string
	token      string
	model      string
	system     string
	httpClient *http.Client
}
