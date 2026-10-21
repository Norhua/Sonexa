package lyrics

// FetchError is the consumer-owned failure contract. Native codes and protocol
// decisions are interpreted by adapters, not by the service or UI.
type FetchError struct {
	Kind       string
	StopSource bool
	Cause      error
}

func (e *FetchError) Error() string { return e.Cause.Error() }
func (e *FetchError) Unwrap() error { return e.Cause }
