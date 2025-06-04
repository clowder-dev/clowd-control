package inventorymanager

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockNodeProvider is a mock implementation of NodeProvider for testing.
type MockNodeProvider struct {
	mock.Mock
}

func (m *MockNodeProvider) GetNodeByID(ctx context.Context, localNodeID string) (Node, error) {
	args := m.Called(ctx, localNodeID)
	return args.Get(0).(Node), args.Error(1)
}

func (m *MockNodeProvider) ListNodes(ctx context.Context, labels map[string]string) ([]Node, error) {
	args := m.Called(ctx, labels)
	val := args.Get(0)
	if val == nil {
		return nil, args.Error(1)
	}
	return val.([]Node), args.Error(1)
}

func (m *MockNodeProvider) DeployPod(ctx context.Context, localNodeID string, spec PodSpecification) (Pod, error) {
	args := m.Called(ctx, localNodeID, spec)
	return args.Get(0).(Pod), args.Error(1)
}

func (m *MockNodeProvider) RemovePod(ctx context.Context, localPodID string) error {
	args := m.Called(ctx, localPodID)
	return args.Error(0)
}

func (m *MockNodeProvider) GetPodByID(ctx context.Context, localPodID string) (Pod, error) {
	args := m.Called(ctx, localPodID)
	return args.Get(0).(Pod), args.Error(1)
}

func (m *MockNodeProvider) ListPods(ctx context.Context, filters ListPodFilters) ([]Pod, error) {
	args := m.Called(ctx, filters)
	val := args.Get(0)
	if val == nil {
		return nil, args.Error(1)
	}
	return val.([]Pod), args.Error(1)
}

func TestNewInventoryManager(t *testing.T) {
	im := NewInventoryManager()
	assert.NotNil(t, im)
	assert.NotNil(t, im.providers)
	assert.Empty(t, im.providers)
}

func TestInventoryManager_RegisterNodeProvider(t *testing.T) {
	im := NewInventoryManager()
	mockProvider := new(MockNodeProvider)

	// Test successful registration
	err := im.RegisterNodeProvider("test-provider", mockProvider)
	assert.NoError(t, err)
	assert.Contains(t, im.providers, "test-provider")
	assert.Equal(t, mockProvider, im.providers["test-provider"])

	// Test registering with empty ID
	err = im.RegisterNodeProvider("", mockProvider)
	assert.Error(t, err)
	assert.EqualError(t, err, "provider ID cannot be empty")

	// Test registering with ID containing separator
	err = im.RegisterNodeProvider("id"+GlobalIDSeparator+"invalid", mockProvider)
	assert.Error(t, err)
	assert.EqualError(t, err, fmt.Sprintf("provider ID 'id%sinvalid' cannot contain the separator '%s'", GlobalIDSeparator, GlobalIDSeparator))

	// Test registering nil provider
	err = im.RegisterNodeProvider("nil-provider", nil)
	assert.Error(t, err)
	assert.EqualError(t, err, "provider cannot be nil")

	// Test registering duplicate provider ID
	err = im.RegisterNodeProvider("test-provider", mockProvider)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrBackendAlreadyExists)
	assert.EqualError(t, err, fmt.Sprintf("%s: provider with ID 'test-provider' already registered", ErrBackendAlreadyExists.Error()))
}

func TestInventoryManager_UnregisterNodeProvider(t *testing.T) {
	im := NewInventoryManager()
	mockProvider := new(MockNodeProvider)
	_ = im.RegisterNodeProvider("test-provider", mockProvider) // Assume success

	// Test successful unregistration
	err := im.UnregisterNodeProvider("test-provider")
	assert.NoError(t, err)
	assert.NotContains(t, im.providers, "test-provider")

	// Test unregistering non-existent provider
	err = im.UnregisterNodeProvider("non-existent-provider")
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrBackendNotFound)
	assert.EqualError(t, err, fmt.Sprintf("%s: provider with ID 'non-existent-provider' not found", ErrBackendNotFound.Error()))
}

func TestParseGlobalID(t *testing.T) {
	tests := []struct {
		name          string
		globalID      string
		wantBackendID string
		wantLocalID   string
		wantErr       error
	}{
		{"valid id", "backend1:nodeA", "backend1", "nodeA", nil},
		{"valid id with separator in local id", "backend1:node:A", "backend1", "node:A", nil},
		{"empty id", "", "", "", ErrInvalidNodeID},
		{"missing separator", "backend1nodeA", "", "", fmt.Errorf("%w: backend1nodeA", ErrInvalidGlobalIDFormat)},
		{"missing backend id", ":nodeA", "", "", fmt.Errorf("%w: :nodeA", ErrInvalidGlobalIDFormat)},
		{"missing local id", "backend1:", "", "", fmt.Errorf("%w: backend1:", ErrInvalidGlobalIDFormat)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backendID, localNodeID, err := parseGlobalID(tt.globalID)
			assert.Equal(t, tt.wantBackendID, backendID)
			assert.Equal(t, tt.wantLocalID, localNodeID)
			if tt.wantErr != nil {
				assert.Error(t, err)
				assert.EqualError(t, err, tt.wantErr.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatGlobalID(t *testing.T) {
	assert.Equal(t, "backend1:nodeA", formatGlobalID("backend1", "nodeA"))
	assert.Equal(t, "b:n", formatGlobalID("b", "n"))
}

func TestInventoryManager_GetNodeByID(t *testing.T) {
	ctx := context.Background()
	im := NewInventoryManager()
	mockProvider1 := new(MockNodeProvider)
	_ = im.RegisterNodeProvider("p1", mockProvider1)

	node1 := Node{ID: "nodeA", Name: "Node A"} // Local ID

	// Test successful GetNodeByID
	mockProvider1.On("GetNodeByID", ctx, "nodeA").Return(node1, nil).Once()
	retrievedNode, err := im.GetNodeByID(ctx, "p1:nodeA")
	assert.NoError(t, err)
	assert.Equal(t, "p1:nodeA", retrievedNode.ID) // Should have global ID
	assert.Equal(t, "Node A", retrievedNode.Name)
	mockProvider1.AssertExpectations(t)

	// Test GetNodeByID with backend not found
	_, err = im.GetNodeByID(ctx, "p2:nodeB")
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrBackendNotFound)
	assert.Contains(t, err.Error(), "p2")

	// Test GetNodeByID when provider returns ErrNodeNotFound
	mockProvider1.On("GetNodeByID", ctx, "nodeC").Return(Node{}, ErrNodeNotFound).Once()
	_, err = im.GetNodeByID(ctx, "p1:nodeC")
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrNodeNotFound)
	mockProvider1.AssertExpectations(t)

	// Test GetNodeByID with provider returning other error
	providerErr := fmt.Errorf("provider internal error")
	mockProvider1.On("GetNodeByID", ctx, "nodeD").Return(Node{}, providerErr).Once()
	_, err = im.GetNodeByID(ctx, "p1:nodeD")
	assert.Error(t, err)
	assert.EqualError(t, err, providerErr.Error())
	mockProvider1.AssertExpectations(t)

	// Test GetNodeByID with invalid global ID
	_, err = im.GetNodeByID(ctx, "invalidid")
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidGlobalIDFormat)
}

func TestInventoryManager_ListNodes(t *testing.T) {
	ctx := context.Background()
	im := NewInventoryManager()
	mockProvider1 := new(MockNodeProvider)
	mockProvider2 := new(MockNodeProvider)

	_ = im.RegisterNodeProvider("p1", mockProvider1)
	_ = im.RegisterNodeProvider("p2", mockProvider2)

	nodesP1 := []Node{
		{ID: "nodeA", Name: "Node A from P1"}, // Local ID
		{ID: "nodeB", Name: "Node B from P1"}, // Local ID
	}
	nodesP2 := []Node{
		{ID: "nodeC", Name: "Node C from P2"}, // Local ID
	}
	emptyLabels := map[string]string{}

	// Test successful ListNodes with multiple providers
	mockProvider1.On("ListNodes", ctx, emptyLabels).Return(nodesP1, nil).Once()
	mockProvider2.On("ListNodes", ctx, emptyLabels).Return(nodesP2, nil).Once()

	allNodes, err := im.ListNodes(ctx, emptyLabels)
	assert.NoError(t, err)
	assert.Len(t, allNodes, 3)

	foundP1NodeA := false
	foundP1NodeB := false
	foundP2NodeC := false

	for _, node := range allNodes {
		if node.ID == "p1:nodeA" && node.Name == "Node A from P1" {
			foundP1NodeA = true
		}
		if node.ID == "p1:nodeB" && node.Name == "Node B from P1" {
			foundP1NodeB = true
		}
		if node.ID == "p2:nodeC" && node.Name == "Node C from P2" {
			foundP2NodeC = true
		}
	}
	assert.True(t, foundP1NodeA, "Expected to find p1:nodeA")
	assert.True(t, foundP1NodeB, "Expected to find p1:nodeB")
	assert.True(t, foundP2NodeC, "Expected to find p2:nodeC")

	mockProvider1.AssertExpectations(t)
	mockProvider2.AssertExpectations(t)

	// Test ListNodes when one provider returns an error
	providerErr := fmt.Errorf("p1 list error")
	mockProvider1.On("ListNodes", ctx, emptyLabels).Return([]Node{}, providerErr).Once()
	// If mockProvider2.ListNodes happens to be called before mockProvider1 errors,
	// it should use this new expectation. .Maybe() means it's okay if it's not called.
	mockProvider2.On("ListNodes", ctx, emptyLabels).Return(nodesP2, nil).Maybe()

	_, err = im.ListNodes(ctx, emptyLabels)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list nodes from provider 'p1'")
	assert.ErrorIs(t, err, providerErr)
	mockProvider1.AssertExpectations(t)
	// mockProvider2.AssertNotCalled(t, "ListNodes", ctx, emptyLabels) // Ensure it short-circuited

	// Test ListNodes with labels (passed through to providers)
	labels := map[string]string{"key": "value"}
	mockProvider1.On("ListNodes", ctx, labels).Return([]Node{}, nil).Once()
	mockProvider2.On("ListNodes", ctx, labels).Return([]Node{}, nil).Once()
	_, err = im.ListNodes(ctx, labels)
	assert.NoError(t, err)
	mockProvider1.AssertExpectations(t)
	mockProvider2.AssertExpectations(t)

	// Test ListNodes with no providers
	imEmpty := NewInventoryManager()
	emptyNodes, err := imEmpty.ListNodes(ctx, emptyLabels)
	assert.NoError(t, err)
	assert.Empty(t, emptyNodes)
}

func TestInventoryManager_DeployPod(t *testing.T) {
	ctx := context.Background()
	im := NewInventoryManager()
	mockProvider := new(MockNodeProvider)
	_ = im.RegisterNodeProvider("p1", mockProvider)

	spec := PodSpecification{ModelID: "test-model"}
	localPod := Pod{ID: "localPodID1", NodeID: "localNode1", Specification: spec, Status: PodStatusPending}

	// Successful deployment
	mockProvider.On("DeployPod", ctx, "localNode1", spec).Return(localPod, nil).Once()
	deployedPod, err := im.DeployPod(ctx, "p1:localNode1", spec)
	assert.NoError(t, err)
	assert.Equal(t, "p1:localPodID1", deployedPod.ID)
	assert.Equal(t, "p1:localNode1", deployedPod.NodeID)
	assert.Equal(t, spec.ModelID, deployedPod.Specification.ModelID)
	mockProvider.AssertExpectations(t)

	// Invalid global node ID
	_, err = im.DeployPod(ctx, "invalidnodeid", spec)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidGlobalIDFormat)

	// Backend not found
	_, err = im.DeployPod(ctx, "p2:localNode1", spec)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrBackendNotFound)

	// Provider returns error
	providerErr := fmt.Errorf("provider deploy error")
	mockProvider.On("DeployPod", ctx, "localNode2", spec).Return(Pod{}, providerErr).Once()
	_, err = im.DeployPod(ctx, "p1:localNode2", spec)
	assert.Error(t, err)
	assert.ErrorIs(t, err, providerErr)
	mockProvider.AssertExpectations(t)
}

func TestInventoryManager_RemovePod(t *testing.T) {
	ctx := context.Background()
	im := NewInventoryManager()
	mockProvider := new(MockNodeProvider)
	_ = im.RegisterNodeProvider("p1", mockProvider)

	// Successful removal
	mockProvider.On("RemovePod", ctx, "localPodID1").Return(nil).Once()
	err := im.RemovePod(ctx, "p1:localPodID1")
	assert.NoError(t, err)
	mockProvider.AssertExpectations(t)

	// Invalid global pod ID
	err = im.RemovePod(ctx, "invalidpodid")
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidGlobalIDFormat) // parseGlobalID returns ErrInvalidGlobalIDFormat

	// Backend not found
	err = im.RemovePod(ctx, "p2:localPodID1")
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrBackendNotFound)

	// Provider returns error
	providerErr := fmt.Errorf("provider remove error")
	mockProvider.On("RemovePod", ctx, "localPodID2").Return(providerErr).Once()
	err = im.RemovePod(ctx, "p1:localPodID2")
	assert.Error(t, err)
	assert.ErrorIs(t, err, providerErr)
	mockProvider.AssertExpectations(t)
}

func TestInventoryManager_GetPodByID(t *testing.T) {
	ctx := context.Background()
	im := NewInventoryManager()
	mockProvider := new(MockNodeProvider)
	_ = im.RegisterNodeProvider("p1", mockProvider)

	localPod := Pod{ID: "localPodID1", NodeID: "localNode1", Status: PodStatusRunning}

	// Successful GetPodByID
	mockProvider.On("GetPodByID", ctx, "localPodID1").Return(localPod, nil).Once()
	retrievedPod, err := im.GetPodByID(ctx, "p1:localPodID1")
	assert.NoError(t, err)
	assert.Equal(t, "p1:localPodID1", retrievedPod.ID)
	assert.Equal(t, "p1:localNode1", retrievedPod.NodeID)
	assert.Equal(t, PodStatusRunning, retrievedPod.Status)
	mockProvider.AssertExpectations(t)

	// Invalid global pod ID
	_, err = im.GetPodByID(ctx, "invalidpodid")
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidGlobalIDFormat)

	// Backend not found
	_, err = im.GetPodByID(ctx, "p2:localPodID1")
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrBackendNotFound)

	// Provider returns ErrPodNotFound
	mockProvider.On("GetPodByID", ctx, "localPodID2").Return(Pod{}, ErrPodNotFound).Once()
	_, err = im.GetPodByID(ctx, "p1:localPodID2")
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrPodNotFound)
	mockProvider.AssertExpectations(t)

	// Provider returns other error
	providerErr := fmt.Errorf("provider get error")
	mockProvider.On("GetPodByID", ctx, "localPodID3").Return(Pod{}, providerErr).Once()
	_, err = im.GetPodByID(ctx, "p1:localPodID3")
	assert.Error(t, err)
	assert.ErrorIs(t, err, providerErr)
	mockProvider.AssertExpectations(t)
}

func TestInventoryManager_ListPods(t *testing.T) {
	ctx := context.Background()
	im := NewInventoryManager()
	mockP1 := new(MockNodeProvider)
	mockP2 := new(MockNodeProvider)
	_ = im.RegisterNodeProvider("p1", mockP1)
	_ = im.RegisterNodeProvider("p2", mockP2)

	podsP1 := []Pod{
		{ID: "podA", NodeID: "node1", Specification: PodSpecification{ModelID: "modelX"}},
		{ID: "podB", NodeID: "node2", Specification: PodSpecification{ModelID: "modelY"}},
	}
	podsP2 := []Pod{
		{ID: "podC", NodeID: "node3", Specification: PodSpecification{ModelID: "modelX"}},
	}

	// Test successful ListPods with multiple providers, no filter
	emptyFilters := ListPodFilters{}
	mockP1.On("ListPods", ctx, emptyFilters).Return(podsP1, nil).Once()
	mockP2.On("ListPods", ctx, emptyFilters).Return(podsP2, nil).Once()

	allPods, err := im.ListPods(ctx, emptyFilters)
	assert.NoError(t, err)
	assert.Len(t, allPods, 3)
	// Check if IDs are globalized
	foundP1PodA := false
	for _, p := range allPods {
		if p.ID == "p1:podA" && p.NodeID == "p1:node1" {
			foundP1PodA = true
		}
	}
	assert.True(t, foundP1PodA, "Expected to find p1:podA with globalized IDs")
	mockP1.AssertExpectations(t)
	mockP2.AssertExpectations(t)

	// Test ListPods with NodeID filter (global)
	nodeFilterP1 := ListPodFilters{NodeID: "p1:node1"}
	expectedP1FilterForNode1 := ListPodFilters{NodeID: "node1"} // local node ID for p1
	expectedP2FilterForP1Node1 := ListPodFilters{NodeID: ""}    // p2 should not filter by p1's node
	mockP1.On("ListPods", ctx, expectedP1FilterForNode1).Return([]Pod{podsP1[0]}, nil).Once()
	mockP2.On("ListPods", ctx, expectedP2FilterForP1Node1).Return(podsP2, nil).Once() // p2 returns all its pods as filter is not for it

	filteredPods, err := im.ListPods(ctx, nodeFilterP1)
	assert.NoError(t, err)
	assert.Len(t, filteredPods, 2) // podA from p1, podC from p2
	foundP1PodA = false
	foundP2PodC := false
	for _, p := range filteredPods {
		if p.ID == "p1:podA" {
			foundP1PodA = true
		}
		if p.ID == "p2:podC" {
			foundP2PodC = true
		}
	}
	assert.True(t, foundP1PodA, "Expected p1:podA with NodeID filter p1:node1")
	assert.True(t, foundP2PodC, "Expected p2:podC with NodeID filter p1:node1 (p2 ignores non-local node filter)")
	mockP1.AssertExpectations(t)
	mockP2.AssertExpectations(t)

	// Test ListPods with ModelID filter
	modelFilter := ListPodFilters{ModelID: "modelX"}
	mockP1.On("ListPods", ctx, modelFilter).Return([]Pod{podsP1[0]}, nil).Once() // podA is modelX
	mockP2.On("ListPods", ctx, modelFilter).Return([]Pod{podsP2[0]}, nil).Once() // podC is modelX
	modelFilteredPods, err := im.ListPods(ctx, modelFilter)
	assert.NoError(t, err)
	assert.Len(t, modelFilteredPods, 2)
	mockP1.AssertExpectations(t)
	mockP2.AssertExpectations(t)

	// Test ListPods when one provider returns an error
	providerErr := fmt.Errorf("p1 list pods error")
	mockP1.On("ListPods", ctx, emptyFilters).Return(nil, providerErr).Once()
	mockP2.On("ListPods", ctx, emptyFilters).Return(podsP2, nil).Once() // This might or might not be called depending on map iteration order
	_, err = im.ListPods(ctx, emptyFilters)
	assert.Error(t, err)
	assert.ErrorIs(t, err, providerErr)
	// Asserting expectations here is tricky due to map iteration.
	// We expect at least mockP1 to have its call attempted.
	mockP1.AssertExpectations(t)
	// mockP2 might not be called if p1 errors first. Resetting for next test.
	mockP2.Mock.ExpectedCalls = []*mock.Call{} // Clear expectations for p2 for next test run

	// Test ListPods with no providers
	imEmpty := NewInventoryManager()
	emptyResultPods, err := imEmpty.ListPods(ctx, emptyFilters)
	assert.NoError(t, err)
	assert.Empty(t, emptyResultPods)
}
