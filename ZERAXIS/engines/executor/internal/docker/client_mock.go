package docker

import (
	"context"
	"sync"
)

// MockClient is a thread-safe test double for the Docker Client interface.
// It supports full configuration for all Phase 7.2/7.3 verification scenarios.
type MockClient struct {
	// ContainerStopErr, if non-nil, is returned by ContainerStop.
	ContainerStopErr error

	// ContainerStopPanic, if true, simulates a hard crash during mutation.
	ContainerStopPanic bool

	// InspectErr, if non-nil, is returned by InspectContainer.
	InspectErr error

	// InspectResult, if set, is returned verbatim by InspectContainer (when InspectErr is nil).
	// If nil, InspectContainer builds a default state from ExpectedDigest and ContainerState.
	InspectResult *ContainerState

	// ExpectedDigest is the authoritative Phase 3 registry digest embedded in the
	// default InspectResult. Wrapped as "mock.registry/image@<ExpectedDigest>".
	ExpectedDigest string

	// ContainerState is the Docker State.Status in the default InspectResult.
	// NewMockClient sets this to "exited" (the Phase 7.3 terminal success state).
	ContainerState string

	// NetworkID is the network name included in the default InspectResult.
	NetworkID string

	mu           sync.Mutex
	stopCounts   map[string]int
	inspectCalls int
}

// NewMockClient returns a MockClient pre-configured for a successful execution
// and successful Phase 7.3 verification: ContainerStop succeeds, InspectContainer
// returns the given digest with State = "exited".
func NewMockClient(digest, networkID string) *MockClient {
	return &MockClient{
		ExpectedDigest: digest,
		ContainerState: "exited",
		NetworkID:      networkID,
		stopCounts:     make(map[string]int),
	}
}

// ContainerStop records the invocation and returns ContainerStopErr (nil by default).
func (m *MockClient) ContainerStop(ctx context.Context, containerID string) error {
	if m.ContainerStopPanic {
		panic("MOCK PANIC: Crash during mutation")
	}
	if m.ContainerStopErr != nil {
		return m.ContainerStopErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopCounts[containerID]++
	return nil
}

// NetworkDisconnect returns ContainerStopErr when set, otherwise succeeds.
func (m *MockClient) NetworkDisconnect(ctx context.Context, containerID, networkID string) error {
	if m.ContainerStopPanic {
		panic("MOCK PANIC: Crash during mutation")
	}
	if m.ContainerStopErr != nil {
		return m.ContainerStopErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.NetworkID == networkID {
		m.NetworkID = ""
	}
	return nil
}

// InspectContainer records the call count, returns InspectResult when set,
// otherwise builds a default ContainerState from ExpectedDigest and ContainerState.
func (m *MockClient) InspectContainer(ctx context.Context, containerID string) (*ContainerState, error) {
	m.mu.Lock()
	m.inspectCalls++
	m.mu.Unlock()

	if m.InspectErr != nil {
		return nil, m.InspectErr
	}
	if m.InspectResult != nil {
		return m.InspectResult, nil
	}
	repoDigest := "mock.registry/image@" + m.ExpectedDigest
	var netIDs []string
	if m.NetworkID != "" {
		netIDs = []string{m.NetworkID}
	}
	return &ContainerState{
		ID:          containerID,
		ImageID:     "sha256:internal_id_not_registry_digest",
		RepoDigests: []string{repoDigest},
		State:       m.ContainerState,
		NetworkIDs:  netIDs,
	}, nil
}

// GetStopCount returns the number of ContainerStop calls for a specific containerID.
func (m *MockClient) GetStopCount(containerID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopCounts[containerID]
}

// TotalMutationCount returns the total ContainerStop calls across all containers.
// Used in concurrency tests to independently prove exactly one Docker mutation occurred.
func (m *MockClient) TotalMutationCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	total := 0
	for _, v := range m.stopCounts {
		total += v
	}
	return total
}

// InspectCallCount returns the number of InspectContainer calls.
// Used by the read-only guarantee test to prove verification never calls ContainerStop.
func (m *MockClient) InspectCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.inspectCalls
}
