package opapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/aifoundry-org/clowd-control/pkg/inventorymanager"
	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
)

// InventoryManagerInterface defines the operations required by InventoryHandlers.
type InventoryManagerInterface interface {
	GetNodeByID(ctx context.Context, globalNodeID string) (inventorymanager.Node, error)
	ListNodes(ctx context.Context, labels map[string]string) ([]inventorymanager.Node, error)
	DeployPod(ctx context.Context, globalNodeID string, spec inventorymanager.PodSpecification) (inventorymanager.Pod, error)
	RemovePod(ctx context.Context, globalPodID string) error
	GetPodByID(ctx context.Context, globalPodID string) (inventorymanager.Pod, error)
	ListPods(ctx context.Context, filters inventorymanager.ListPodFilters) ([]inventorymanager.Pod, error)
}

// InventoryHandlers provides HTTP handlers for inventory operations.
type InventoryHandlers struct {
	im     InventoryManagerInterface
	logger *logrus.Entry
}

// NewInventoryHandlers creates a new InventoryHandlers instance.
func NewInventoryHandlers(im InventoryManagerInterface, parentLogger *logrus.Entry) *InventoryHandlers {
	return &InventoryHandlers{
		im:     im,
		logger: parentLogger.WithField("sub_component", "inventory_handlers"),
	}
}

// RegisterRoutes registers the inventory management API endpoints on the given router.
func (ih *InventoryHandlers) RegisterRoutes(router *mux.Router) {
	nodesRouter := router.PathPrefix("/nodes").Subrouter()
	podsRouter := router.PathPrefix("/pods").Subrouter()

	// Node routes
	nodesRouter.HandleFunc("", ih.ListNodes).Methods(http.MethodGet)
	nodesRouter.HandleFunc("/{node_id}", ih.GetNode).Methods(http.MethodGet) // Renamed {id} to {node_id} for clarity
	nodesRouter.HandleFunc("/{node_id}/pods", ih.DeployPodOnNode).Methods(http.MethodPost)
	ih.logger.Info("Registered node management endpoints under /nodes prefix")

	// Pod routes
	podsRouter.HandleFunc("", ih.ListPods).Methods(http.MethodGet)
	podsRouter.HandleFunc("/{pod_id}", ih.GetPodByID).Methods(http.MethodGet)
	podsRouter.HandleFunc("/{pod_id}", ih.RemovePod).Methods(http.MethodDelete)
	ih.logger.Info("Registered pod management endpoints under /pods prefix")
}

// respondWithJSON and respondWithError are duplicated from model_handlers.go
// Consider refactoring to a shared utility if more handlers need them.

func (ih *InventoryHandlers) respondWithJSON(w http.ResponseWriter, code int, payload any) {
	response, err := json.Marshal(payload)
	if err != nil {
		ih.logger.WithError(err).Error("Failed to marshal JSON response")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"failed to marshal response"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, err = w.Write(response)
	if err != nil {
		ih.logger.WithError(err).Error("Failed to write JSON response")
	}
}

func (ih *InventoryHandlers) respondWithError(w http.ResponseWriter, code int, message string, details string) {
	ih.logger.WithFields(logrus.Fields{
		"status_code": code,
		"details":     details,
	}).Error(message)
	ih.respondWithJSON(w, code, ErrorResponse{Error: message, Details: details})
}

// ListNodes handles GET requests to list all nodes.
// It supports label filtering via query parameters.
// e.g., /nodes?label.foo=bar&baz=bat will filter for labels foo:bar and baz:bat
func (ih *InventoryHandlers) ListNodes(w http.ResponseWriter, r *http.Request) {
	filterLabels := make(map[string]string)
	query := r.URL.Query()
	for key, values := range query {
		if len(values) > 0 {
			// Allow "label.key=value" or "key=value" for filtering
			labelKey := strings.TrimPrefix(key, "label.")
			filterLabels[labelKey] = values[0] // Use the first value if multiple are provided for the same key
		}
	}

	nodes, err := ih.im.ListNodes(r.Context(), filterLabels)
	if err != nil {
		ih.respondWithError(w, http.StatusInternalServerError, "Failed to list nodes", err.Error())
		return
	}
	if nodes == nil { // Ensure we return an empty array instead of null for an empty list
		nodes = []inventorymanager.Node{}
	}
	ih.respondWithJSON(w, http.StatusOK, nodes)
	ih.logger.Debug("Successfully listed nodes")
}

// GetNode handles GET requests to retrieve a node by its ID.
func (ih *InventoryHandlers) GetNode(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	nodeID, ok := vars["node_id"] // Changed "id" to "node_id"
	if !ok || nodeID == "" {
		ih.respondWithError(w, http.StatusBadRequest, "Node ID not provided in path", "")
		return
	}

	// Basic validation for global ID format (contains separator, non-empty parts)
	// More robust validation is done by inventorymanager.parseGlobalID
	if !strings.Contains(nodeID, inventorymanager.GlobalIDSeparator) || strings.HasPrefix(nodeID, inventorymanager.GlobalIDSeparator) || strings.HasSuffix(nodeID, inventorymanager.GlobalIDSeparator) {
		ih.respondWithError(w, http.StatusBadRequest, "Invalid Node ID format", "Expected 'provider:localNodeId'")
		return
	}

	node, err := ih.im.GetNodeByID(r.Context(), nodeID)
	if err != nil {
		if errors.Is(err, inventorymanager.ErrNodeNotFound) {
			ih.respondWithError(w, http.StatusNotFound, "Node not found", err.Error())
		} else if errors.Is(err, inventorymanager.ErrInvalidGlobalIDFormat) || errors.Is(err, inventorymanager.ErrBackendNotFound) {
			ih.respondWithError(w, http.StatusBadRequest, "Invalid or unknown Node ID", err.Error())
		} else {
			ih.respondWithError(w, http.StatusInternalServerError, "Failed to get node", err.Error())
		}
		return
	}
	ih.respondWithJSON(w, http.StatusOK, node)
	ih.logger.Debugf("Successfully retrieved node with ID: %s", nodeID)
}

// DeployPodOnNode handles POST requests to deploy a pod on a specific node.
func (ih *InventoryHandlers) DeployPodOnNode(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	nodeID, ok := vars["node_id"]
	if !ok || nodeID == "" {
		ih.respondWithError(w, http.StatusBadRequest, "Node ID not provided in path", "")
		return
	}

	// Basic validation for global Node ID format
	if !strings.Contains(nodeID, inventorymanager.GlobalIDSeparator) || strings.HasPrefix(nodeID, inventorymanager.GlobalIDSeparator) || strings.HasSuffix(nodeID, inventorymanager.GlobalIDSeparator) {
		ih.respondWithError(w, http.StatusBadRequest, "Invalid Node ID format", "Expected 'provider:localNodeId'")
		return
	}

	// FIXME: we don't accept specs anymore
	// var spec inventorymanager.PodSpecification
	// if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
	// 	ih.respondWithError(w, http.StatusBadRequest, "Invalid request payload", err.Error())
	// 	return
	// }
	defer r.Body.Close()

	var finalSpec inventorymanager.PodSpecification

	var req DeployPodRequest // Defined below or in a shared types file for opapi
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Try to decode as raw PodSpecification for backward compatibility or direct use
		// This requires resetting the reader or careful handling.
		// For now, let's assume the new structure or fail.
		// A more robust solution would involve trying to unmarshal into DeployPodRequest,
		// and if that fails due to unknown fields (if strict decoding is on),
		// then try to unmarshal into PodSpecification.
		// However, json.Decoder consumes the body.
		// The simplest for now is to expect DeployPodRequest.
		// If the user sends a raw PodSpecification, it might partially match or fail.
		// Let's refine this: the request body is *always* DeployPodRequest.
		// If user wants to send full spec, they use req.Specification.
		ih.respondWithError(w, http.StatusBadRequest, "Invalid request payload format", err.Error())
		return
	}
	// Re-close, as Decode might have finished reading but not closed.
	// defer r.Body.Close() // Already deferred

	if req.TemplateID != "" {
		// Template-based deployment
		if req.Specification != nil {
			ih.respondWithError(w, http.StatusBadRequest, "Invalid request", "Cannot provide both 'specification' and 'template_id'")
			return
		}

		// GetPodTemplateByID now returns the struct directly, not a pointer or interface.
		template, exists := inventorymanager.GetPodTemplateByID(req.TemplateID)
		if !exists {
			ih.respondWithError(w, http.StatusNotFound, "Pod template not found", fmt.Sprintf("Template with ID '%s' does not exist", req.TemplateID))
			return
		}

		var errRender error
		finalSpec, errRender = template.Render(req.TemplateParams)
		if errRender != nil {
			ih.respondWithError(w, http.StatusBadRequest, "Failed to render pod template", errRender.Error())
			return
		}
		ih.logger.Infof("Rendered pod specification from template '%s' for node '%s'", req.TemplateID, nodeID)

	} else if req.Specification != nil {
		// Direct specification-based deployment
		finalSpec = *req.Specification
		ih.logger.Infof("Using direct pod specification for node '%s'", nodeID)
	} else {
		ih.respondWithError(w, http.StatusBadRequest, "Invalid request", "Must provide either 'specification' or 'template_id' in the request body")
		return
	}

	// Basic validation for the final PodSpecification (whether from template or direct)
	if finalSpec.ModelID == "" {
		ih.respondWithError(w, http.StatusBadRequest, "Invalid pod specification", "model_id is required")
		return
	}
	if finalSpec.ResourceRequest.RAM == nil || *finalSpec.ResourceRequest.RAM <= 0 {
		ih.respondWithError(w, http.StatusBadRequest, "Invalid pod specification", "resource_request.ram_mb must be a positive integer")
		return
	}
	if finalSpec.Image == "" {
		// This could be made optional if the system can infer/default it later
		ih.respondWithError(w, http.StatusBadRequest, "Invalid pod specification", "image is required")
		return
	}

	pod, err := ih.im.DeployPod(r.Context(), nodeID, finalSpec)
	if err != nil {
		if errors.Is(err, inventorymanager.ErrNodeNotFound) {
			ih.respondWithError(w, http.StatusNotFound, "Node not found for pod deployment", err.Error())
		} else if errors.Is(err, inventorymanager.ErrInvalidGlobalIDFormat) { // Should be caught by earlier check, but good to have
			ih.respondWithError(w, http.StatusBadRequest, "Invalid Node ID for pod deployment", err.Error())
		} else if errors.Is(err, inventorymanager.ErrDeploymentFailed) || errors.Is(err, inventorymanager.ErrResourceUnavailable) {
			ih.respondWithError(w, http.StatusInternalServerError, "Pod deployment failed", err.Error())
		} else {
			ih.respondWithError(w, http.StatusInternalServerError, "Failed to deploy pod", err.Error())
		}
		return
	}

	ih.respondWithJSON(w, http.StatusCreated, pod)
	ih.logger.Infof("Successfully deployed pod %s on node %s", pod.ID, nodeID)
}

// DeployPodRequest defines the structure for the request body of DeployPodOnNode.
// It allows specifying a pod either directly or via a template.
type DeployPodRequest struct {
	// Option 1: Direct specification
	Specification *inventorymanager.PodSpecification `json:"specification,omitempty"`

	// Option 2: Template-based deployment
	TemplateID     string         `json:"template_id,omitempty"`
	TemplateParams map[string]any `json:"template_params,omitempty"`
}

// ListPods handles GET requests to list all pods, with optional filtering.
func (ih *InventoryHandlers) ListPods(w http.ResponseWriter, r *http.Request) {
	filters := inventorymanager.ListPodFilters{}
	query := r.URL.Query()

	if nodeID := query.Get("node_id"); nodeID != "" {
		filters.NodeID = nodeID
	}
	if modelID := query.Get("model_id"); modelID != "" {
		filters.ModelID = modelID
	}
	if status := query.Get("status"); status != "" {
		filters.Status = inventorymanager.PodStatus(status) // Basic cast, could add validation
	}

	labelFilters := make(map[string]string)
	for key, values := range query {
		if strings.HasPrefix(key, "label.") && len(values) > 0 {
			labelKey := strings.TrimPrefix(key, "label.")
			labelFilters[labelKey] = values[0]
		}
	}
	if len(labelFilters) > 0 {
		filters.Labels = labelFilters
	}

	pods, err := ih.im.ListPods(r.Context(), filters)
	if err != nil {
		ih.respondWithError(w, http.StatusInternalServerError, "Failed to list pods", err.Error())
		return
	}
	if pods == nil {
		pods = []inventorymanager.Pod{}
	}
	ih.respondWithJSON(w, http.StatusOK, pods)
	ih.logger.Debug("Successfully listed pods")
}

// GetPodByID handles GET requests to retrieve a pod by its global ID.
func (ih *InventoryHandlers) GetPodByID(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	podID, ok := vars["pod_id"]
	if !ok || podID == "" {
		ih.respondWithError(w, http.StatusBadRequest, "Pod ID not provided in path", "")
		return
	}

	if !strings.Contains(podID, inventorymanager.GlobalIDSeparator) || strings.HasPrefix(podID, inventorymanager.GlobalIDSeparator) || strings.HasSuffix(podID, inventorymanager.GlobalIDSeparator) {
		ih.respondWithError(w, http.StatusBadRequest, "Invalid Pod ID format", "Expected 'provider:localPodId'")
		return
	}

	pod, err := ih.im.GetPodByID(r.Context(), podID)
	if err != nil {
		if errors.Is(err, inventorymanager.ErrPodNotFound) {
			ih.respondWithError(w, http.StatusNotFound, "Pod not found", err.Error())
		} else if errors.Is(err, inventorymanager.ErrInvalidPodIDFormat) || errors.Is(err, inventorymanager.ErrBackendNotFound) { // BackendNotFound can occur if provider part of ID is wrong
			ih.respondWithError(w, http.StatusBadRequest, "Invalid or unknown Pod ID", err.Error())
		} else {
			ih.respondWithError(w, http.StatusInternalServerError, "Failed to get pod", err.Error())
		}
		return
	}
	ih.respondWithJSON(w, http.StatusOK, pod)
	ih.logger.Debugf("Successfully retrieved pod with ID: %s", podID)
}

// RemovePod handles DELETE requests to remove a pod by its global ID.
func (ih *InventoryHandlers) RemovePod(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	podID, ok := vars["pod_id"]
	if !ok || podID == "" {
		ih.respondWithError(w, http.StatusBadRequest, "Pod ID not provided in path", "")
		return
	}

	if !strings.Contains(podID, inventorymanager.GlobalIDSeparator) || strings.HasPrefix(podID, inventorymanager.GlobalIDSeparator) || strings.HasSuffix(podID, inventorymanager.GlobalIDSeparator) {
		ih.respondWithError(w, http.StatusBadRequest, "Invalid Pod ID format", "Expected 'provider:localPodId'")
		return
	}

	err := ih.im.RemovePod(r.Context(), podID)
	if err != nil {
		if errors.Is(err, inventorymanager.ErrPodNotFound) {
			ih.respondWithError(w, http.StatusNotFound, "Pod not found for removal", err.Error())
		} else if errors.Is(err, inventorymanager.ErrInvalidPodIDFormat) || errors.Is(err, inventorymanager.ErrBackendNotFound) {
			ih.respondWithError(w, http.StatusBadRequest, "Invalid or unknown Pod ID for removal", err.Error())
		} else {
			ih.respondWithError(w, http.StatusInternalServerError, "Failed to remove pod", err.Error())
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
	ih.logger.Infof("Successfully initiated removal of pod with ID: %s", podID)
}
