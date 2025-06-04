package inventorymanager

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

const (
	// GlobalIDSeparator is the character used to separate the backend provider ID
	// from the local node ID in a global node ID string.
	GlobalIDSeparator = ":"
)

// InventoryManager provides a unified view of nodes from multiple underlying NodeProviders.
// It handles the prefixing of node IDs with their backend provider ID to ensure global uniqueness.
type InventoryManager struct {
	mu        sync.RWMutex
	providers map[string]NodeProvider
}

// NewInventoryManager creates a new instance of InventoryManager.
func NewInventoryManager() *InventoryManager {
	return &InventoryManager{
		providers: make(map[string]NodeProvider),
	}
}

// RegisterNodeProvider adds a new NodeProvider to the InventoryManager.
// The id provided will be used as a prefix for all nodes managed by this provider.
func (im *InventoryManager) RegisterNodeProvider(id string, provider NodeProvider) error {
	if id == "" {
		return fmt.Errorf("provider ID cannot be empty")
	}
	if strings.Contains(id, GlobalIDSeparator) {
		return fmt.Errorf("provider ID '%s' cannot contain the separator '%s'", id, GlobalIDSeparator)
	}
	if provider == nil {
		return fmt.Errorf("provider cannot be nil")
	}

	im.mu.Lock()
	defer im.mu.Unlock()

	if _, exists := im.providers[id]; exists {
		return fmt.Errorf("%w: provider with ID '%s' already registered", ErrBackendAlreadyExists, id)
	}
	im.providers[id] = provider
	return nil
}

// UnregisterNodeProvider removes a NodeProvider from the InventoryManager.
func (im *InventoryManager) UnregisterNodeProvider(id string) error {
	im.mu.Lock()
	defer im.mu.Unlock()

	if _, exists := im.providers[id]; !exists {
		return fmt.Errorf("%w: provider with ID '%s' not found", ErrBackendNotFound, id)
	}
	delete(im.providers, id)
	return nil
}

// parseGlobalID splits a global node ID (e.g., "backend1:nodeA") into backend ID and local node ID.
func parseGlobalID(globalID string) (backendID string, localNodeID string, err error) {
	if globalID == "" {
		return "", "", ErrInvalidNodeID
	}
	parts := strings.SplitN(globalID, GlobalIDSeparator, 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("%w: %s", ErrInvalidGlobalIDFormat, globalID)
	}
	return parts[0], parts[1], nil
}

// formatGlobalID creates a global ID from a backend ID and a local node ID.
func formatGlobalID(backendID string, localNodeID string) string {
	return backendID + GlobalIDSeparator + localNodeID
}

// GetNodeByID retrieves a specific node by its global ID.
// The returned Node will have its ID field set to the global ID.
func (im *InventoryManager) GetNodeByID(ctx context.Context, globalNodeID string) (Node, error) {
	backendID, localNodeID, err := parseGlobalID(globalNodeID)
	if err != nil {
		return Node{}, err
	}

	im.mu.RLock()
	provider, exists := im.providers[backendID]
	im.mu.RUnlock()

	if !exists {
		return Node{}, fmt.Errorf("%w: %s", ErrBackendNotFound, backendID)
	}

	node, err := provider.GetNodeByID(ctx, localNodeID)
	if err != nil {
		return Node{}, err // Provider should return ErrNodeNotFound if applicable
	}

	// Ensure the returned node has the global ID
	node.ID = formatGlobalID(backendID, node.ID) // node.ID from provider is local
	return node, nil
}

// ListNodes returns a slice of all nodes from all registered providers.
// Node IDs in the returned slice are global IDs.
// It accepts a map of labels to filter the nodes; filtering is delegated to providers.
func (im *InventoryManager) ListNodes(ctx context.Context, labels map[string]string) ([]Node, error) {
	im.mu.RLock()
	defer im.mu.RUnlock()

	var allNodes []Node
	for backendID, provider := range im.providers {
		nodes, err := provider.ListNodes(ctx, labels)
		if err != nil {
			// Potentially log this error and continue, or return an aggregate error
			return nil, fmt.Errorf("failed to list nodes from provider '%s': %w", backendID, err)
		}
		for _, node := range nodes {
			// Ensure the node has the global ID
			node.ID = formatGlobalID(backendID, node.ID) // node.ID from provider is local
			allNodes = append(allNodes, node)
		}
	}
	return allNodes, nil
}

// DeployPod deploys a new pod on the specified node.
// globalNodeID is the global identifier of the node.
// spec is the specification for the pod to be deployed.
// The returned Pod will have its ID and NodeID fields set to global identifiers.
func (im *InventoryManager) DeployPod(ctx context.Context, globalNodeID string, spec PodSpecification) (Pod, error) {
	backendID, localNodeID, err := parseGlobalID(globalNodeID)
	if err != nil {
		// Wrap error for context, ErrInvalidNodeID might be confusing for a node ID.
		if errors.Is(err, ErrInvalidNodeID) {
			return Pod{}, fmt.Errorf("invalid global node ID '%s': %w", globalNodeID, ErrInvalidGlobalIDFormat)
		}
		return Pod{}, err
	}

	im.mu.RLock()
	provider, exists := im.providers[backendID]
	im.mu.RUnlock()

	if !exists {
		return Pod{}, fmt.Errorf("provider for backend ID '%s' not found: %w", backendID, ErrBackendNotFound)
	}

	pod, err := provider.DeployPod(ctx, localNodeID, spec)
	if err != nil {
		return Pod{}, fmt.Errorf("provider '%s' failed to deploy pod on node '%s': %w", backendID, localNodeID, err)
	}

	// Ensure the returned pod has global IDs.
	// Provider returns localPodID in pod.ID and localNodeID in pod.NodeID.
	pod.ID = formatGlobalID(backendID, pod.ID)
	pod.NodeID = formatGlobalID(backendID, pod.NodeID) // This should match the input globalNodeID if provider behaves correctly.

	return pod, nil
}

// RemovePod removes a pod by its global ID.
// globalPodID is the global identifier of the pod (e.g., "backendID:localPodID").
func (im *InventoryManager) RemovePod(ctx context.Context, globalPodID string) error {
	backendID, localPodID, err := parseGlobalID(globalPodID)
	if err != nil {
		// Wrap error for context, ErrInvalidNodeID might be confusing for a pod ID.
		if errors.Is(err, ErrInvalidNodeID) {
			return fmt.Errorf("invalid global pod ID '%s': %w", globalPodID, ErrInvalidPodIDFormat)
		}
		return err
	}

	im.mu.RLock()
	provider, exists := im.providers[backendID]
	im.mu.RUnlock()

	if !exists {
		return fmt.Errorf("provider for backend ID '%s' not found: %w", backendID, ErrBackendNotFound)
	}

	err = provider.RemovePod(ctx, localPodID)
	if err != nil {
		return fmt.Errorf("provider '%s' failed to remove pod '%s': %w", backendID, localPodID, err)
	}
	return nil
}

// GetPodByID retrieves a specific pod by its global ID.
// The returned Pod will have its ID and NodeID fields set to global identifiers.
func (im *InventoryManager) GetPodByID(ctx context.Context, globalPodID string) (Pod, error) {
	backendID, localPodID, err := parseGlobalID(globalPodID)
	if err != nil {
		if errors.Is(err, ErrInvalidNodeID) {
			return Pod{}, fmt.Errorf("invalid global pod ID '%s': %w", globalPodID, ErrInvalidPodIDFormat)
		}
		return Pod{}, err
	}

	im.mu.RLock()
	provider, exists := im.providers[backendID]
	im.mu.RUnlock()

	if !exists {
		return Pod{}, fmt.Errorf("provider for backend ID '%s' not found: %w", backendID, ErrBackendNotFound)
	}

	pod, err := provider.GetPodByID(ctx, localPodID)
	if err != nil {
		return Pod{}, fmt.Errorf("provider '%s' failed to get pod '%s': %w", backendID, localPodID, err)
	}

	// Ensure the returned pod has global IDs.
	// Provider returns localPodID in pod.ID and localNodeID in pod.NodeID.
	pod.ID = formatGlobalID(backendID, pod.ID)
	pod.NodeID = formatGlobalID(backendID, pod.NodeID)

	return pod, nil
}

// ListPods returns a slice of all pods from all registered providers, matching the filters.
// Pod IDs and NodeIDs in the returned slice are global IDs.
// Filters like ListPodFilters.NodeID should use global node IDs.
func (im *InventoryManager) ListPods(ctx context.Context, filters ListPodFilters) ([]Pod, error) {
	im.mu.RLock()
	defer im.mu.RUnlock()

	var allPods []Pod
	var multiError error

	for backendID, provider := range im.providers {
		providerFilters := filters // Make a copy to potentially modify for the specific provider

		// Adapt NodeID filter if it's global
		if filters.NodeID != "" {
			filterBackendID, filterLocalNodeID, err := parseGlobalID(filters.NodeID)
			if err != nil {
				// If global NodeID filter is invalid, it won't match anything.
				// We could error out, or let it result in empty results from providers.
				// For now, let it pass, providers will likely find nothing.
				// Or, more strictly:
				// return nil, fmt.Errorf("invalid global NodeID filter '%s': %w", filters.NodeID, err)
			} else {
				if filterBackendID == backendID {
					providerFilters.NodeID = filterLocalNodeID // Use local ID for this provider
				} else {
					// NodeID filter is for a different backend, so this provider should not match it.
					// We can skip this provider for this filter, or pass an impossible filter.
					// Easiest is to set a NodeID that won't match, or rely on provider to handle empty if NodeID is not for it.
					// For simplicity, if NodeID is for another backend, this provider won't match it.
					// We can make this explicit by telling the provider to list pods for a non-existent local node.
					// Or, more simply, if the filter is for a different backend, this provider should return all its pods
					// that match *other* filters, as the NodeID filter is not applicable to it.
					// So, we clear the NodeID filter for this specific provider call if it's for another backend.
					providerFilters.NodeID = ""
				}
			}
		}

		pods, err := provider.ListPods(ctx, providerFilters)
		if err != nil {
			// Collect errors from providers
			err = fmt.Errorf("failed to list pods from provider '%s': %w", backendID, err)
			if multiError == nil {
				multiError = err
			} else {
				multiError = fmt.Errorf("%v; %w", multiError, err)
			}
			continue // Continue with other providers
		}

		for _, pod := range pods {
			// Ensure the pod has global IDs
			// Provider returns localPodID in pod.ID and localNodeID in pod.NodeID.
			pod.ID = formatGlobalID(backendID, pod.ID)
			pod.NodeID = formatGlobalID(backendID, pod.NodeID)
			allPods = append(allPods, pod)
		}
	}

	if multiError != nil {
		// If we collected some pods, we might want to return them along with the error.
		// For now, if any provider errors, the whole operation errors.
		return nil, multiError
	}

	return allPods, nil
}
