package ai_tools

import "fmt"

// ProviderError preserves the provider HTTP status and trace id without exposing keys.
type ProviderError struct {
	Provider string
	Status   int
	TraceID  string
	Body     string
}

func (e *ProviderError) Error() string {
	if e.TraceID == "" {
		return fmt.Sprintf("%s returned HTTP %d: %s", e.Provider, e.Status, e.Body)
	}
	return fmt.Sprintf("%s returned HTTP %d (trace %s): %s", e.Provider, e.Status, e.TraceID, e.Body)
}
