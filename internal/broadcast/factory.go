package broadcast

// NewStubBroadcaster creates a test broadcaster with 12 confirmations.
func NewStubBroadcaster() Broadcaster {
	return StubBroadcaster{ConfirmationsValue: 12}
}
