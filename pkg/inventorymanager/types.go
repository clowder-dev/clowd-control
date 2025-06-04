package inventorymanager

import (
	"context"
	"errors"
	"time"

	"github.com/aifoundry-org/clowd-control/pkg/modelmanager"
)

// Common errors for the inventory manager package.
var (
	ErrNodeNotFound          = errors.New("node not found")
	ErrNodeAlreadyExists     = errors.New("node already exists")
	ErrBackendNotFound       = errors.New("backend node provider not found")
	ErrBackendAlreadyExists  = errors.New("backend node provider already exists")
	ErrInvalidGlobalIDFormat = errors.New("invalid global ID format; expected 'backendID:localID'") // Updated for generic localID
	ErrInvalidNodeID         = errors.New("node ID is invalid or empty")

	ErrPodNotFound         = errors.New("pod not found")
	ErrPodAlreadyExists    = errors.New("pod already exists")
	ErrInvalidPodIDFormat  = errors.New("invalid global pod ID format; expected 'backendID:localPodID'")
	ErrInvalidPodID        = errors.New("pod ID is invalid or empty")
	ErrDeploymentFailed    = errors.New("pod deployment failed")
	ErrResourceUnavailable = errors.New("requested resources are unavailable on the node")
)

// NodeProvider defines the interface for a backend that can supply node and pod information.
// Implementations of this interface handle the specifics of interacting with an
// underlying cluster system (e.g., Kubernetes API, a static configuration, cloud provider APIs).
// All Node IDs handled by a NodeProvider are local to that provider.
// The NodeProvider is responsible for fetching the current state of nodes from its backend.
type NodeProvider interface {
	// GetNodeByID retrieves a specific node by its local ID from the backend.
	// The returned Node should have its ID field set to the localNodeID.
	GetNodeByID(ctx context.Context, localNodeID string) (Node, error)

	// ListNodes returns a slice of nodes currently reported by this provider from the backend.
	// Nodes returned must have their ID field set to their local ID.
	// It accepts a map of labels to filter the nodes based on the backend's capabilities.
	ListNodes(ctx context.Context, labels map[string]string) ([]Node, error)

	// DeployPod instructs the provider to deploy a new pod/runtime instance on a specific node.
	// localNodeID is the provider-specific ID of the node.
	// spec defines the pod to be deployed.
	// The returned Pod should have its ID field set to the localPodID assigned by the provider,
	// and its NodeID field set to the localNodeID it was deployed on.
	DeployPod(ctx context.Context, localNodeID string, spec PodSpecification) (Pod, error)

	// RemovePod instructs the provider to remove/terminate a pod by its local pod ID.
	// localPodID is the provider-specific ID of the pod.
	RemovePod(ctx context.Context, localPodID string) error

	// GetPodByID retrieves a specific pod by its local pod ID from the backend.
	// The returned Pod should have its ID field set to the localPodID.
	// Its NodeID field should be the localNodeID of the node it's running on.
	GetPodByID(ctx context.Context, localPodID string) (Pod, error)

	// ListPods returns a slice of pods currently managed by this provider.
	// Pods returned must have their ID field set to their localPodID and NodeID to localNodeID.
	// It accepts filters; filter.NodeID, if provided, should be a localNodeID.
	ListPods(ctx context.Context, filters ListPodFilters) ([]Pod, error)
}

// PodPort defines a network port for a pod.
type PodPort struct {
	Name          string `json:"name,omitempty"`      // e.g., "api", "metrics"
	ContainerPort int    `json:"container_port"`      // Port inside the pod/container
	HostPort      int    `json:"host_port,omitempty"` // Optional. Port on the host node.
	Protocol      string `json:"protocol,omitempty"`  // e.g., "TCP", "UDP", defaults to "TCP"
}

// PodStatus represents the state of a pod.
type PodStatus string

const (
	PodStatusPending     PodStatus = "Pending"
	PodStatusRunning     PodStatus = "Running"
	PodStatusSucceeded   PodStatus = "Succeeded"
	PodStatusFailed      PodStatus = "Failed"
	PodStatusTerminating PodStatus = "Terminating"
	PodStatusUnknown     PodStatus = "Unknown"
)

// PodSpecification defines the desired state for deploying a new pod.
type PodSpecification struct {
	ModelID              string                            `json:"model_id"`        // Required. ID of the model this pod will serve.
	Image                string                            `json:"image,omitempty"` // Optional. Container image.
	Ports                []PodPort                         `json:"ports,omitempty"`
	EnvVars              map[string]string                 `json:"env_vars,omitempty"`
	Command              []string                          `json:"command,omitempty"`
	Args                 []string                          `json:"args,omitempty"`
	VolumeMounts         []VolumeMount                     `json:"volume_mounts,omitempty"`
	ResourceRequest      modelmanager.ResourceRequirements `json:"resource_request"` // Required. Resources needed.
	Labels               map[string]string                 `json:"labels,omitempty"`
	CustomProviderConfig map[string]any                    `json:"custom_provider_config,omitempty"` // Backend-specific config.
}

// VolumeMount describes a mounting of a volume within a container.
type VolumeMount struct {
	Name      string `json:"name"`                // This must match the Name of a Volume.
	MountPath string `json:"mount_path"`          // Path within the container at which the volume should be mounted.
	ReadOnly  bool   `json:"read_only,omitempty"` // Mounted read-only if true, read-write otherwise (false or unspecified).
}

// Pod represents an instance of an inference runtime.
type Pod struct {
	ID                   string           `json:"id"`      // Globally unique ID (backendID:localPodID) after processing by InventoryManager. Provider returns localPodID.
	NodeID               string           `json:"node_id"` // Global Node ID (backendID:localNodeID) where the pod is. Provider returns localNodeID.
	Specification        PodSpecification `json:"specification"`
	Status               PodStatus        `json:"status"`
	Message              string           `json:"message,omitempty"` // More details about status.
	CreatedAt            time.Time        `json:"created_at"`
	UpdatedAt            time.Time        `json:"updated_at"`
	ActualPorts          []PodPort        `json:"actual_ports,omitempty"`           // Actual ports, including dynamic host ports.
	CustomProviderStatus map[string]any   `json:"custom_provider_status,omitempty"` // Backend-specific status.
}

// ListPodFilters defines criteria for filtering lists of pods.
// When used with InventoryManager, NodeID is a global node ID.
// When used with NodeProvider, NodeID is a local node ID.
type ListPodFilters struct {
	NodeID  string            `json:"node_id,omitempty"`
	ModelID string            `json:"model_id,omitempty"`
	Status  PodStatus         `json:"status,omitempty"`
	Labels  map[string]string `json:"labels,omitempty"`
}

// Accelerator represents a hardware accelerator (e.g., GPU).
type Accelerator struct {
	Type  string `json:"type"`  // e.g., "nvidia-tesla-t4", "nvidia-a100"
	Count int    `json:"count"` // Number of such accelerators
	// Future considerations: MemoryMB int `json:"memory_mb,omitempty"`
}

// NodeStatus represents the operational status of a worker node.
type NodeStatus string

const (
	NodeStatusUnknown  NodeStatus = "Unknown"  // Status is not known.
	NodeStatusPending  NodeStatus = "Pending"  // Node is being provisioned or initialized.
	NodeStatusReady    NodeStatus = "Ready"    // Node is healthy and ready to accept workloads.
	NodeStatusDraining NodeStatus = "Draining" // Node is cordoned, and workloads are being evicted.
	NodeStatusOffline  NodeStatus = "Offline"  // Node is not reachable or powered down.
	NodeStatusError    NodeStatus = "Error"    // Node is in an error state.
)

// NodeResources represents the resources of a node.
type NodeResources struct {
	CPU          string        `json:"cpu"`        // CPU cores, e.g., "4", "8000m" (millicores)
	RAM_MB       int           `json:"ram_mb"`     // RAM in Megabytes
	Storage_GB   int           `json:"storage_gb"` // Ephemeral storage in Gigabytes
	Accelerators []Accelerator `json:"accelerators,omitempty"`
}

// Node represents a compute node in the cluster.
// It's an abstraction that can map to a physical machine, a VM, or a Kubernetes node.
type Node struct {
	ID          string            `json:"id"`             // Unique identifier for the node (e.g., k8s node name, machine-id)
	Name        string            `json:"name,omitempty"` // Optional human-readable name
	Status      NodeStatus        `json:"status"`
	Address     string            `json:"address,omitempty"` // IP address or hostname of the node
	Capacity    NodeResources     `json:"capacity"`          // Total resources available on the node
	Allocatable NodeResources     `json:"allocatable"`       // Resources allocatable for workloads (Capacity - SystemOverhead)
	Labels      map[string]string `json:"labels,omitempty"`  // Key-value pairs for categorization, selection, and scheduling
	Taints      []string          `json:"taints,omitempty"`  // Represents scheduling restrictions (e.g., "key=value:Effect")
	// LastHeartbeatTime time.Time      `json:"last_heartbeat_time,omitempty"` // For tracking node health via heartbeats
	// CustomProperties map[string]interface{} `json:"custom_properties,omitempty"` // For any other specific attributes
}
