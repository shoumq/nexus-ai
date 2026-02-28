package llm

import "net/http"

type AIConfig struct {
	URL          string
	Token        string
	Model        string
	SystemPrompt string
	Fast         bool
	MaxTokens    int
	HTTPClient   *http.Client
}

type AIClient struct {
	url        string
	token      string
	model      string
	system     string
	fast       bool
	maxTokens  int
	httpClient *http.Client
}
