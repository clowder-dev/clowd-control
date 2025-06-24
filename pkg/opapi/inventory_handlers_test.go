package opapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings" // Required for strings.NewReader in TestInventoryHandlers_DeployPodOnNode
	"testing"

	"github.com/aifoundry-org/clowd-control/pkg/inventorymanager"
	"github.com/aifoundry-org/clowd-control/pkg/modelmanager" // Added import
	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockInventoryManager is a mock implementation of InventoryManagerInterface.
type MockInventoryManager struct {
	mock.Mock
}

func (m *MockInventoryManager) GetNodeByID(ctx context.Context, globalNodeID string) (inventorymanager.Node, error) {
	args := m.Called(ctx, globalNodeID)
	// Handle potential nil for the Node object if error is not nil
	if args.Get(0) == nil {
		return inventorymanager.Node{}, args.Error(1)
	}
	return args.Get(0).(inventorymanager.Node), args.Error(1)
}

func (m *MockInventoryManager) ListNodes(ctx context.Context, labels map[string]string) ([]inventorymanager.Node, error) {
	args := m.Called(ctx, labels)
	// Handle potential nil for the slice if error is not nil
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]inventorymanager.Node), args.Error(1)
}

func (m *MockInventoryManager) DeployPod(ctx context.Context, globalNodeID string, spec inventorymanager.PodSpecification) (inventorymanager.Pod, error) {
	args := m.Called(ctx, globalNodeID, spec)
	if args.Get(0) == nil {
		return inventorymanager.Pod{}, args.Error(1)
	}
	return args.Get(0).(inventorymanager.Pod), args.Error(1)
}

func (m *MockInventoryManager) RemovePod(ctx context.Context, globalPodID string) error {
	args := m.Called(ctx, globalPodID)
	return args.Error(0)
}

func (m *MockInventoryManager) GetPodByID(ctx context.Context, globalPodID string) (inventorymanager.Pod, error) {
	args := m.Called(ctx, globalPodID)
	if args.Get(0) == nil {
		return inventorymanager.Pod{}, args.Error(1)
	}
	return args.Get(0).(inventorymanager.Pod), args.Error(1)
}

func (m *MockInventoryManager) ListPods(ctx context.Context, filters inventorymanager.ListPodFilters) ([]inventorymanager.Pod, error) {
	args := m.Called(ctx, filters)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]inventorymanager.Pod), args.Error(1)
}

func newTestLoggerInventory() *logrus.Logger { // Renamed to avoid conflict if model_handlers_test is in same package view
	logger := logrus.New()
	logger.SetOutput(io.Discard) // Suppress log output during tests
	return logger
}

func setupTestServerWithInventoryHandlers(im *MockInventoryManager) (*httptest.Server, *InventoryHandlers) {
	logger := newTestLoggerInventory()
	parentEntry := logger.WithField("component", "test-opapi-server")
	ih := NewInventoryHandlers(im, parentEntry)

	router := mux.NewRouter()
	apiV1Router := router.PathPrefix("/api/v1").Subrouter()
	ih.RegisterRoutes(apiV1Router)

	return httptest.NewServer(router), ih
}

func TestInventoryHandlers_ListNodes(t *testing.T) {
	im := new(MockInventoryManager)
	server, _ := setupTestServerWithInventoryHandlers(im)
	defer server.Close()

	t.Run("successful list nodes", func(t *testing.T) {
		expectedNodes := []inventorymanager.Node{
			{ID: "p1:nodeA", Name: "Node A"},
			{ID: "p2:nodeB", Name: "Node B"},
		}
		im.On("ListNodes", mock.Anything, map[string]string{}).Return(expectedNodes, nil).Once()

		resp, err := http.Get(server.URL + "/api/v1/nodes")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		var actualNodes []inventorymanager.Node
		err = json.NewDecoder(resp.Body).Decode(&actualNodes)
		require.NoError(t, err)
		assert.Equal(t, expectedNodes, actualNodes)
		im.AssertExpectations(t)
	})

	t.Run("successful list nodes with label filter", func(t *testing.T) {
		expectedNodes := []inventorymanager.Node{
			{ID: "p1:nodeFiltered", Name: "Node Filtered", Labels: map[string]string{"env": "prod"}},
		}
		filter := map[string]string{"env": "prod"}
		im.On("ListNodes", mock.Anything, filter).Return(expectedNodes, nil).Once()

		resp, err := http.Get(server.URL + "/api/v1/nodes?label.env=prod")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		var actualNodes []inventorymanager.Node
		err = json.NewDecoder(resp.Body).Decode(&actualNodes)
		require.NoError(t, err)
		assert.Equal(t, expectedNodes, actualNodes)
		im.AssertExpectations(t)
	})

	t.Run("successful list nodes with direct label filter", func(t *testing.T) {
		expectedNodes := []inventorymanager.Node{
			{ID: "p1:nodeFiltered", Name: "Node Filtered", Labels: map[string]string{"zone": "us-east-1"}},
		}
		filter := map[string]string{"zone": "us-east-1"}
		im.On("ListNodes", mock.Anything, filter).Return(expectedNodes, nil).Once()

		resp, err := http.Get(server.URL + "/api/v1/nodes?zone=us-east-1")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		var actualNodes []inventorymanager.Node
		err = json.NewDecoder(resp.Body).Decode(&actualNodes)
		require.NoError(t, err)
		assert.Equal(t, expectedNodes, actualNodes)
		im.AssertExpectations(t)
	})

	t.Run("empty list nodes", func(t *testing.T) {
		im.On("ListNodes", mock.Anything, map[string]string{}).Return([]inventorymanager.Node{}, nil).Once()

		resp, err := http.Get(server.URL + "/api/v1/nodes")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		bodyBytes, _ := io.ReadAll(resp.Body)
		assert.JSONEq(t, `[]`, string(bodyBytes))
		im.AssertExpectations(t)
	})

	t.Run("inventory manager returns error", func(t *testing.T) {
		im.On("ListNodes", mock.Anything, map[string]string{}).Return(nil, fmt.Errorf("internal error")).Once()

		resp, err := http.Get(server.URL + "/api/v1/nodes")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		var errResp ErrorResponse
		err = json.NewDecoder(resp.Body).Decode(&errResp)
		require.NoError(t, err)
		assert.Equal(t, "Failed to list nodes", errResp.Error)
		assert.Equal(t, "internal error", errResp.Details)
		im.AssertExpectations(t)
	})
}

func TestInventoryHandlers_GetNode(t *testing.T) {
	im := new(MockInventoryManager)
	server, _ := setupTestServerWithInventoryHandlers(im)
	defer server.Close()

	nodeID := "p1:nodeA"
	expectedNode := inventorymanager.Node{ID: nodeID, Name: "Node A"}

	t.Run("successful get node", func(t *testing.T) {
		im.On("GetNodeByID", mock.Anything, nodeID).Return(expectedNode, nil).Once()

		resp, err := http.Get(server.URL + "/api/v1/nodes/" + nodeID)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		var actualNode inventorymanager.Node
		err = json.NewDecoder(resp.Body).Decode(&actualNode)
		require.NoError(t, err)
		assert.Equal(t, expectedNode, actualNode)
		im.AssertExpectations(t)
	})

	t.Run("node not found", func(t *testing.T) {
		nonExistentID := "p1:nodeNotFound"
		im.On("GetNodeByID", mock.Anything, nonExistentID).Return(inventorymanager.Node{}, inventorymanager.ErrNodeNotFound).Once()

		resp, err := http.Get(server.URL + "/api/v1/nodes/" + nonExistentID)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
		var errResp ErrorResponse
		err = json.NewDecoder(resp.Body).Decode(&errResp)
		require.NoError(t, err)
		assert.Equal(t, "Node not found", errResp.Error)
		im.AssertExpectations(t)
	})

	t.Run("invalid node ID format - no separator", func(t *testing.T) {
		invalidID := "p1nodeA"
		// No mock expectation as it should fail before calling manager

		resp, err := http.Get(server.URL + "/api/v1/nodes/" + invalidID)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		var errResp ErrorResponse
		err = json.NewDecoder(resp.Body).Decode(&errResp)
		require.NoError(t, err)
		assert.Equal(t, "Invalid Node ID format", errResp.Error)
	})

	t.Run("invalid node ID format - empty provider", func(t *testing.T) {
		invalidID := ":nodeA"
		// No mock expectation

		resp, err := http.Get(server.URL + "/api/v1/nodes/" + invalidID)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("invalid node ID format - empty localId", func(t *testing.T) {
		invalidID := "provider:"
		// No mock expectation

		resp, err := http.Get(server.URL + "/api/v1/nodes/" + invalidID)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("inventory manager returns ErrInvalidGlobalIDFormat", func(t *testing.T) {
		badFormatID := "p1:stillbad" // ID that passes handler's basic check but fails in manager
		im.On("GetNodeByID", mock.Anything, badFormatID).Return(inventorymanager.Node{}, inventorymanager.ErrInvalidGlobalIDFormat).Once()

		resp, err := http.Get(server.URL + "/api/v1/nodes/" + badFormatID)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		var errResp ErrorResponse
		err = json.NewDecoder(resp.Body).Decode(&errResp)
		require.NoError(t, err)
		assert.Equal(t, "Invalid or unknown Node ID", errResp.Error)
		im.AssertExpectations(t)
	})

	t.Run("inventory manager returns other error", func(t *testing.T) {
		otherErrorID := "p1:nodeError"
		internalErr := fmt.Errorf("some internal manager error")
		im.On("GetNodeByID", mock.Anything, otherErrorID).Return(inventorymanager.Node{}, internalErr).Once()

		resp, err := http.Get(server.URL + "/api/v1/nodes/" + otherErrorID)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		var errResp ErrorResponse
		err = json.NewDecoder(resp.Body).Decode(&errResp)
		require.NoError(t, err)
		assert.Equal(t, "Failed to get node", errResp.Error)
		assert.Equal(t, internalErr.Error(), errResp.Details)
		im.AssertExpectations(t)
	})

	t.Run("node ID not provided in path", func(t *testing.T) {
		// This test case is tricky with current mux setup, as "/api/v1/nodes/" would match ListNodes.
		// A specific test for this would require a route that explicitly ends with a slash
		// or a different router configuration. Gorilla Mux by default treats /path and /path/ differently.
		// For now, we assume the router setup correctly distinguishes.
		// If a route like `nodesRouter.HandleFunc("/", ...)` existed, it would catch this.
		// The current `nodesRouter.HandleFunc("/{id}", ...)` requires an ID.
		// An empty ID like `/api/v1/nodes/` would likely be a 404 or 405 from mux itself if not matched by ListNodes.
		// Let's test `GET /api/v1/nodes/` which should be caught by ListNodes if strict slash is not enforced,
		// or result in 404/405 if it is.
		// The current implementation of GetNode checks `id == ""`, which is good.
		// Mux usually ensures `id` is non-empty if the route `/{id}` matches.
		// So, this specific "ID not provided" by an empty segment is hard to test without more complex routing.
		// The `!ok || id == ""` check in GetNode is a safeguard.
		t.Skip("Skipping test for empty node ID in path as mux typically handles this by not matching or providing empty var")
	})
}

func TestInventoryHandlers_DeployPodOnNode(t *testing.T) {
	im := new(MockInventoryManager)
	server, _ := setupTestServerWithInventoryHandlers(im)
	defer server.Close()

	nodeID := "p1:nodeA"
	podSpec := inventorymanager.PodSpecification{
		ModelID: "test-model",
		Image:   "test-image",
		ResourceRequest: modelmanager.ResourceRequirements{ // Corrected type
			RAM: func(i int) *int { return &i }(1024),
		},
	}
	expectedPod := inventorymanager.Pod{
		ID:            "p1:podXYZ",
		NodeID:        nodeID,
		Specification: podSpec,
		Status:        inventorymanager.PodStatusPending,
	}

	t.Run("successful pod deployment", func(t *testing.T) {
		im.On("DeployPod", mock.Anything, nodeID, podSpec).Return(expectedPod, nil).Once()

		body, _ := json.Marshal(podSpec)
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/nodes/"+nodeID+"/pods", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusCreated, resp.StatusCode)
		var actualPod inventorymanager.Pod
		err = json.NewDecoder(resp.Body).Decode(&actualPod)
		require.NoError(t, err)
		assert.Equal(t, expectedPod, actualPod)
		im.AssertExpectations(t)
	})

	t.Run("invalid node ID in path", func(t *testing.T) {
		body, _ := json.Marshal(podSpec)
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/nodes/invalidNodeID/pods", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("malformed JSON payload", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/nodes/"+nodeID+"/pods", strings.NewReader("{malformed"))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("missing required field in spec (e.g., ModelID)", func(t *testing.T) {
		invalidSpec := inventorymanager.PodSpecification{Image: "test"} // Missing ModelID and ResourceRequest
		body, _ := json.Marshal(invalidSpec)
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/nodes/"+nodeID+"/pods", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		// Further check error message if desired
	})

	t.Run("node not found by manager", func(t *testing.T) {
		im.On("DeployPod", mock.Anything, nodeID, podSpec).Return(inventorymanager.Pod{}, inventorymanager.ErrNodeNotFound).Once()
		body, _ := json.Marshal(podSpec)
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/nodes/"+nodeID+"/pods", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
		im.AssertExpectations(t)
	})

	t.Run("deployment failed by manager", func(t *testing.T) {
		im.On("DeployPod", mock.Anything, nodeID, podSpec).Return(inventorymanager.Pod{}, inventorymanager.ErrDeploymentFailed).Once()
		body, _ := json.Marshal(podSpec)
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/nodes/"+nodeID+"/pods", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		im.AssertExpectations(t)
	})
}

func TestInventoryHandlers_ListPods(t *testing.T) {
	im := new(MockInventoryManager)
	server, _ := setupTestServerWithInventoryHandlers(im)
	defer server.Close()

	expectedPods := []inventorymanager.Pod{
		{ID: "p1:podA", NodeID: "p1:node1", Status: inventorymanager.PodStatusRunning},
		{ID: "p2:podB", NodeID: "p2:node2", Status: inventorymanager.PodStatusPending},
	}

	t.Run("successful list pods", func(t *testing.T) {
		filters := inventorymanager.ListPodFilters{} // Empty filters
		im.On("ListPods", mock.Anything, filters).Return(expectedPods, nil).Once()

		resp, err := http.Get(server.URL + "/api/v1/pods")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		var actualPods []inventorymanager.Pod
		err = json.NewDecoder(resp.Body).Decode(&actualPods)
		require.NoError(t, err)
		assert.Equal(t, expectedPods, actualPods)
		im.AssertExpectations(t)
	})

	t.Run("successful list pods with filters", func(t *testing.T) {
		filters := inventorymanager.ListPodFilters{
			NodeID:  "p1:node1",
			ModelID: "modelX",
			Status:  inventorymanager.PodStatusRunning,
			Labels:  map[string]string{"env": "prod"},
		}
		im.On("ListPods", mock.Anything, filters).Return([]inventorymanager.Pod{expectedPods[0]}, nil).Once()

		reqURL := fmt.Sprintf("%s/api/v1/pods?node_id=p1:node1&model_id=modelX&status=Running&label.env=prod", server.URL)
		resp, err := http.Get(reqURL)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		im.AssertExpectations(t)
	})

	t.Run("empty list pods", func(t *testing.T) {
		filters := inventorymanager.ListPodFilters{}
		im.On("ListPods", mock.Anything, filters).Return([]inventorymanager.Pod{}, nil).Once()

		resp, err := http.Get(server.URL + "/api/v1/pods")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		bodyBytes, _ := io.ReadAll(resp.Body)
		assert.JSONEq(t, `[]`, string(bodyBytes))
		im.AssertExpectations(t)
	})

	t.Run("inventory manager returns error", func(t *testing.T) {
		filters := inventorymanager.ListPodFilters{}
		im.On("ListPods", mock.Anything, filters).Return(nil, fmt.Errorf("internal list error")).Once()

		resp, err := http.Get(server.URL + "/api/v1/pods")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		im.AssertExpectations(t)
	})
}

func TestInventoryHandlers_GetPodByID(t *testing.T) {
	im := new(MockInventoryManager)
	server, _ := setupTestServerWithInventoryHandlers(im)
	defer server.Close()

	podID := "p1:podXYZ"
	expectedPod := inventorymanager.Pod{ID: podID, NodeID: "p1:nodeA", Status: inventorymanager.PodStatusRunning}

	t.Run("successful get pod", func(t *testing.T) {
		im.On("GetPodByID", mock.Anything, podID).Return(expectedPod, nil).Once()

		resp, err := http.Get(server.URL + "/api/v1/pods/" + podID)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		var actualPod inventorymanager.Pod
		err = json.NewDecoder(resp.Body).Decode(&actualPod)
		require.NoError(t, err)
		assert.Equal(t, expectedPod, actualPod)
		im.AssertExpectations(t)
	})

	t.Run("pod not found", func(t *testing.T) {
		im.On("GetPodByID", mock.Anything, "p1:notFound").Return(inventorymanager.Pod{}, inventorymanager.ErrPodNotFound).Once()
		resp, err := http.Get(server.URL + "/api/v1/pods/p1:notFound")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
		im.AssertExpectations(t)
	})

	t.Run("invalid pod ID format", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/v1/pods/invalidID")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("manager returns other error", func(t *testing.T) {
		im.On("GetPodByID", mock.Anything, podID).Return(inventorymanager.Pod{}, fmt.Errorf("internal error")).Once()
		resp, err := http.Get(server.URL + "/api/v1/pods/" + podID)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		im.AssertExpectations(t)
	})
}

func TestInventoryHandlers_RemovePod(t *testing.T) {
	im := new(MockInventoryManager)
	server, _ := setupTestServerWithInventoryHandlers(im)
	defer server.Close()

	podID := "p1:podToDelete"

	t.Run("successful pod removal", func(t *testing.T) {
		im.On("RemovePod", mock.Anything, podID).Return(nil).Once()

		req, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/v1/pods/"+podID, nil)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusNoContent, resp.StatusCode)
		im.AssertExpectations(t)
	})

	t.Run("pod not found for removal", func(t *testing.T) {
		im.On("RemovePod", mock.Anything, "p1:notFound").Return(inventorymanager.ErrPodNotFound).Once()
		req, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/v1/pods/p1:notFound", nil)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
		im.AssertExpectations(t)
	})

	t.Run("invalid pod ID format for removal", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/v1/pods/invalidID", nil)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("manager returns other error on removal", func(t *testing.T) {
		im.On("RemovePod", mock.Anything, podID).Return(fmt.Errorf("internal delete error")).Once()
		req, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/v1/pods/"+podID, nil)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		im.AssertExpectations(t)
	})
}
