package testutils

type MockObject struct {
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
	Content string `json:"content,omitempty"`
}
