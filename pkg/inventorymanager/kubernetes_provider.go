package inventorymanager

import (
	"context"
	"errors" // Standard library errors package for errors.Is
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	k8sAPIErrors "k8s.io/apimachinery/pkg/api/errors" // Aliased to avoid conflict
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields" // Added for field selector
	k8sLabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"maps"
)

const (
	// clowderNamespaceLabelKey is the label key used to scope nodes to a specific namespace
	// for the purpose of this provider. Nodes must have this label matching the provider's targetNamespace.
	clowderNamespaceLabelKey = "clowder.io/namespace"

	// Common GPU vendor resource name prefixes
	nvidiaGpuResourcePrefix = "nvidia.com/gpu"
	amdGpuResourcePrefix    = "amd.com/gpu"        // Or specific like "amd.com/mi250", "amd.com/mi100"
	intelGpuResourcePrefix  = "gpu.intel.com/i915" // Or other specific types like "gpu.intel.com/sriov"
)

// KubernetesNodeProvider implements the NodeProvider interface for Kubernetes.
// It fetches node information from a Kubernetes cluster and is scoped to a targetNamespace
// by checking for a specific label on the nodes.
type KubernetesNodeProvider struct {
	clientset       kubernetes.Interface
	targetNamespace string
}

// NewKubernetesNodeProvider creates a new KubernetesNodeProvider.
// If clientset is nil, it attempts to create one using in-cluster config,
// then falls back to the default kubeconfig.
// targetNamespace is the namespace this provider is scoped to; nodes must have
// the 'clowder.io/namespace: <targetNamespace>' label to be included.
func NewKubernetesNodeProvider(cs kubernetes.Interface, targetNamespace string) (*KubernetesNodeProvider, error) {
	if targetNamespace == "" {
		return nil, fmt.Errorf("targetNamespace cannot be empty")
	}

	var err error
	if cs == nil {
		config, errInCluster := rest.InClusterConfig()
		if errInCluster != nil {
			// Not in cluster, try kubeconfig from default location
			loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
			kubeConfigOverrides := &clientcmd.ConfigOverrides{}
			kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, kubeConfigOverrides)
			config, err = kubeConfig.ClientConfig()
			if err != nil {
				return nil, fmt.Errorf("failed to create k8s config (in-cluster err: %v): %w", errInCluster, err)
			}
		}
		cs, err = kubernetes.NewForConfig(config)
		if err != nil {
			return nil, fmt.Errorf("failed to create k8s clientset: %w", err)
		}
	}

	return &KubernetesNodeProvider{
		clientset:       cs,
		targetNamespace: targetNamespace,
	}, nil
}

// GetNodeByID retrieves a specific node by its local ID (Kubernetes node name).
// The node must have the 'clowder.io/namespace' label matching the provider's targetNamespace.
func (p *KubernetesNodeProvider) GetNodeByID(ctx context.Context, localNodeID string) (Node, error) {
	k8sNode, err := p.clientset.CoreV1().Nodes().Get(ctx, localNodeID, metav1.GetOptions{})
	if err != nil {
		if k8sAPIErrors.IsNotFound(err) { // Use the aliased k8sAPIErrors package
			return Node{}, fmt.Errorf("%w: Kubernetes node '%s' not found or not accessible", ErrNodeNotFound, localNodeID)
		}
		return Node{}, fmt.Errorf("failed to get Kubernetes node '%s': %w", localNodeID, err)
	}

	if nsLabel, ok := k8sNode.Labels[clowderNamespaceLabelKey]; !ok || nsLabel != p.targetNamespace {
		return Node{}, fmt.Errorf("%w: Kubernetes node '%s' does not belong to target namespace '%s' (missing or mismatched label '%s')", ErrNodeNotFound, localNodeID, p.targetNamespace, clowderNamespaceLabelKey)
	}

	return p.k8sNodeToInventoryNode(k8sNode), nil
}

// ListNodes returns a slice of nodes from Kubernetes.
// Nodes are filtered by the 'clowder.io/namespace' label matching the provider's targetNamespace,
// and then by any additional labels specified in the 'filterLabels' argument.
func (p *KubernetesNodeProvider) ListNodes(ctx context.Context, filterLabels map[string]string) ([]Node, error) {
	selectorSet := make(map[string]string)
	// Start with the mandatory namespace scoping label
	selectorSet[clowderNamespaceLabelKey] = p.targetNamespace

	// Add user-provided filter labels, ensuring not to override the namespace key if present
	for k, v := range filterLabels {
		if k == clowderNamespaceLabelKey && v != p.targetNamespace {
			// If user tries to filter by a different namespace, it will result in no nodes,
			// which is correct behavior as our provider is scoped.
			// Or, we could return an error, but this seems more consistent with label filtering.
			return []Node{}, nil // Effectively an impossible filter for this provider instance
		}
		if k != clowderNamespaceLabelKey { // Avoid double-adding if user redundantly specifies it
			selectorSet[k] = v
		}
	}

	labelSelector := k8sLabels.SelectorFromSet(selectorSet).String()

	k8sNodeList, err := p.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		return nil, fmt.Errorf("failed to list Kubernetes nodes with selector '%s': %w", labelSelector, err)
	}

	var inventoryNodes []Node
	for i := range k8sNodeList.Items {
		k8sNode := k8sNodeList.Items[i] // Important to use a new variable in the loop for the pointer
		// The label selector should have already filtered by namespace,
		// but this check is a safeguard.
		if nsLabel, ok := k8sNode.Labels[clowderNamespaceLabelKey]; !ok || nsLabel != p.targetNamespace {
			continue // Should not happen if selector works as expected
		}
		inventoryNodes = append(inventoryNodes, p.k8sNodeToInventoryNode(&k8sNode))
	}

	return inventoryNodes, nil
}

func (p *KubernetesNodeProvider) k8sNodeToInventoryNode(k8sNode *corev1.Node) Node {
	status := NodeStatusUnknown
	if k8sNode.Spec.Unschedulable {
		status = NodeStatusDraining
	} else {
		for _, cond := range k8sNode.Status.Conditions {
			if cond.Type == corev1.NodeReady {
				switch cond.Status {
				case corev1.ConditionTrue:
					status = NodeStatusReady
				case corev1.ConditionFalse:
					// Could be NodeStatusError or more specific based on other conditions
					status = NodeStatusError // Defaulting to Error if not Ready
				case corev1.ConditionUnknown:
					status = NodeStatusUnknown
				}
				break
			}
		}
	}

	address := ""
	for _, addr := range k8sNode.Status.Addresses {
		if addr.Type == corev1.NodeInternalIP {
			address = addr.Address
			break
		}
	}
	if address == "" { // Fallback to Hostname if InternalIP is not found
		for _, addr := range k8sNode.Status.Addresses {
			if addr.Type == corev1.NodeHostName {
				address = addr.Address
				break
			}
		}
	}
	// Could add ExternalIP as another fallback if needed

	var taints []string
	for _, taint := range k8sNode.Spec.Taints {
		taints = append(taints, fmt.Sprintf("%s=%s:%s", taint.Key, taint.Value, taint.Effect))
	}

	return Node{
		ID:          k8sNode.Name, // Local ID for the provider
		Name:        k8sNode.Name,
		Status:      status,
		Address:     address,
		Capacity:    p.extractNodeResources(k8sNode.Status.Capacity),
		Allocatable: p.extractNodeResources(k8sNode.Status.Allocatable),
		Labels:      k8sNode.Labels, // Kubernetes labels are directly compatible
		Taints:      taints,
	}
}

func (p *KubernetesNodeProvider) extractNodeResources(k8sResources corev1.ResourceList) NodeResources {
	var resources NodeResources
	if cpu, ok := k8sResources[corev1.ResourceCPU]; ok {
		resources.CPU = cpu.String()
	}
	if mem, ok := k8sResources[corev1.ResourceMemory]; ok {
		resources.RAM_MB = int(mem.Value() / (1024 * 1024)) // Bytes to MB
	}
	if storage, ok := k8sResources[corev1.ResourceEphemeralStorage]; ok {
		resources.Storage_GB = int(storage.Value() / (1024 * 1024 * 1024)) // Bytes to GB
	}

	var accelerators []Accelerator
	for resName, quantity := range k8sResources {
		nameStr := string(resName)
		var accType string
		isAccelerator := false

		// Check for known GPU resource prefixes
		if strings.HasPrefix(nameStr, nvidiaGpuResourcePrefix) {
			accType = strings.TrimPrefix(nameStr, nvidiaGpuResourcePrefix)
			if accType == "" { // Case like "nvidia.com/gpu"
				accType = "gpu" // Generic NVIDIA GPU
			}
			isAccelerator = true
		} else if strings.HasPrefix(nameStr, amdGpuResourcePrefix) {
			accType = strings.TrimPrefix(nameStr, amdGpuResourcePrefix)
			if accType == "" {
				accType = "gpu" // Generic AMD GPU
			}
			isAccelerator = true
		} else if strings.HasPrefix(nameStr, intelGpuResourcePrefix) {
			accType = strings.TrimPrefix(nameStr, intelGpuResourcePrefix)
			if accType == "" {
				accType = "gpu" // Generic Intel GPU
			}
			isAccelerator = true
		}
		// Add more vendor domains/prefixes if needed, e.g., specific Intel device types

		if isAccelerator {
			accelerators = append(accelerators, Accelerator{
				Type:  fmt.Sprintf("%s%s", strings.SplitN(nameStr, "/", 2)[0], accType), // e.g. nvidia.com/tesla-t4
				Count: int(quantity.Value()),
			})
		}
	}
	resources.Accelerators = accelerators
	return resources
}

// k8sPodPhaseToInventoryStatus converts a Kubernetes PodPhase to an inventorymanager.PodStatus.
func k8sPodPhaseToInventoryStatus(phase corev1.PodPhase) PodStatus {
	switch phase {
	case corev1.PodPending:
		return PodStatusPending
	case corev1.PodRunning:
		return PodStatusRunning
	case corev1.PodSucceeded:
		return PodStatusSucceeded
	case corev1.PodFailed:
		return PodStatusFailed
	case corev1.PodUnknown:
		return PodStatusUnknown
	default:
		return PodStatusUnknown
	}
}

// k8sPodToInventoryPod converts a Kubernetes Pod object into an inventorymanager.Pod object.
// If originalSpec is provided, it's used directly. Otherwise, the specification is reconstructed
// from the k8sPod object itself, which might be an approximation of the original.
// The NodeID in the returned Pod is the localNodeID.
func (p *KubernetesNodeProvider) k8sPodToInventoryPod(k8sPod *corev1.Pod, originalSpec *PodSpecification) Pod {
	var specToUse PodSpecification
	if originalSpec != nil {
		specToUse = *originalSpec
	} else {
		// Reconstruct PodSpecification from k8sPod
		specToUse.ModelID = k8sPod.Labels["clowder.io/model-id"] // Assuming this label is set
		specToUse.Labels = make(map[string]string)
		for k, v := range k8sPod.Labels {
			// Filter out clowder internal labels from the spec's labels
			if !strings.HasPrefix(k, "clowder.io/") && k != "app.kubernetes.io/name" && k != "app.kubernetes.io/instance" { // Add more common k8s labels if needed
				specToUse.Labels[k] = v
			}
		}

		if len(k8sPod.Spec.Containers) > 0 {
			mainContainer := k8sPod.Spec.Containers[0]
			specToUse.Image = mainContainer.Image
			specToUse.Command = mainContainer.Command
			specToUse.Args = mainContainer.Args

			for _, k8sPort := range mainContainer.Ports {
				specToUse.Ports = append(specToUse.Ports, PodPort{
					Name:          k8sPort.Name,
					ContainerPort: int(k8sPort.ContainerPort),
					HostPort:      int(k8sPort.HostPort),
					Protocol:      string(k8sPort.Protocol),
				})
			}
			for _, envVar := range mainContainer.Env {
				if specToUse.EnvVars == nil {
					specToUse.EnvVars = make(map[string]string)
				}
				specToUse.EnvVars[envVar.Name] = envVar.Value
				// Note: ValueFrom (e.g. secretKeyRef, configMapKeyRef) is not handled here
			}

			// Reconstruct VolumeMounts
			for _, k8sVm := range mainContainer.VolumeMounts {
				specToUse.VolumeMounts = append(specToUse.VolumeMounts, VolumeMount{
					Name:      k8sVm.Name,
					MountPath: k8sVm.MountPath,
					ReadOnly:  k8sVm.ReadOnly,
				})
			}

			// Reconstruct ResourceRequest (RAM only for now, as per current PodSpecification)
			if memReq, ok := mainContainer.Resources.Requests[corev1.ResourceMemory]; ok {
				ramMB := int(memReq.Value() / (1024 * 1024))
				specToUse.ResourceRequest.RAM = &ramMB
			}
			// Storage is not directly part of k8s container resources, usually handled by volumes.
		}
	}

	var actualPorts []PodPort
	if len(k8sPod.Spec.Containers) > 0 {
		for _, k8sPort := range k8sPod.Spec.Containers[0].Ports {
			actualPorts = append(actualPorts, PodPort{
				Name:          k8sPort.Name,
				ContainerPort: int(k8sPort.ContainerPort),
				HostPort:      int(k8sPort.HostPort), // May be 0 if not specified or dynamically assigned
				Protocol:      string(k8sPort.Protocol),
			})
		}
	}

	return Pod{
		ID:            k8sPod.Name,          // Local Pod ID
		NodeID:        k8sPod.Spec.NodeName, // Local Node ID
		Specification: specToUse,
		Status:        k8sPodPhaseToInventoryStatus(k8sPod.Status.Phase),
		Message:       k8sPod.Status.Message,
		CreatedAt:     k8sPod.CreationTimestamp.Time,
		UpdatedAt:     time.Now(), // Or derive from conditions if more accuracy is needed
		ActualPorts:   actualPorts,
		CustomProviderStatus: map[string]any{
			"podIP": k8sPod.Status.PodIP,
			"phase": string(k8sPod.Status.Phase),
			// Consider adding more details like conditions or container statuses
		},
	}
}

// DeployPod creates a new pod on the specified Kubernetes node.
func (p *KubernetesNodeProvider) DeployPod(ctx context.Context, localNodeID string, spec PodSpecification) (Pod, error) {
	// 1. Validate the target node
	node, err := p.GetNodeByID(ctx, localNodeID)
	if err != nil {
		return Pod{}, fmt.Errorf("failed to validate target node '%s': %w", localNodeID, err)
	}
	if node.Status != NodeStatusReady && node.Status != NodeStatusDraining { // Draining nodes might still accept critical pods or have specific taints
		// For general purpose pods, we usually want Ready nodes.
		// If node is Draining, it implies it's being phased out.
		// This check can be more sophisticated based on taints/tolerations in the future.
		// For now, let's be conservative and prefer Ready nodes.
		// However, K8s scheduler itself will make the final call based on NodeName and taints/tolerations.
		// The primary role of GetNodeByID here is to confirm it's a known and managed node.
		// Let's ensure it's not in a definitive non-operational state like Offline or Error.
		if node.Status == NodeStatusOffline || node.Status == NodeStatusError || node.Status == NodeStatusUnknown {
			return Pod{}, fmt.Errorf("%w: target node '%s' is not in a schedulable state (status: %s)", ErrResourceUnavailable, localNodeID, node.Status)
		}
	}

	// 2. Generate Pod Name and basic ObjectMeta
	podName := fmt.Sprintf("model-%s-%s", strings.ToLower(spec.ModelID), uuid.New().String()[:8])
	podLabels := map[string]string{
		"clowder.io/managed-by": "clowd-control",
		"clowder.io/model-id":   spec.ModelID,
		"clowder.io/namespace":  p.targetNamespace, // For discoverability, even though pod is in the namespace
	}
	maps.Copy(podLabels, spec.Labels)

	// 3. Construct Kubernetes Pod object
	k8sPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: p.targetNamespace,
			Labels:    podLabels,
			// Annotations can be added from spec.CustomProviderConfig if needed
		},
		Spec: corev1.PodSpec{
			NodeName:      localNodeID, // Schedule on the specific node
			RestartPolicy: corev1.RestartPolicyOnFailure,
			Containers:    []corev1.Container{},
		},
	}

	// 4. Define the container
	if spec.Image == "" {
		return Pod{}, fmt.Errorf("%w: image must be specified in PodSpecification", ErrDeploymentFailed)
	}
	containerName := "inference-container" // Or derive from spec.ModelID
	if spec.ModelID != "" {
		containerName = strings.ToLower(spec.ModelID) + "-container"
	}

	k8sContainer := corev1.Container{
		Name:         containerName,
		Image:        spec.Image,
		Command:      spec.Command,
		Args:         spec.Args,
		Ports:        []corev1.ContainerPort{},
		Env:          []corev1.EnvVar{},
		VolumeMounts: []corev1.VolumeMount{},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{},
			Limits:   corev1.ResourceList{},
		},
	}

	for _, pPort := range spec.Ports {
		k8sContainer.Ports = append(k8sContainer.Ports, corev1.ContainerPort{
			Name:          pPort.Name,
			ContainerPort: int32(pPort.ContainerPort),
			HostPort:      int32(pPort.HostPort), // If 0, K8s might assign dynamically or not expose on host
			Protocol:      corev1.Protocol(pPort.Protocol),
		})
	}
	if len(k8sContainer.Ports) == 0 { // Ensure at least TCP protocol if none specified for a port
		// This block might be too presumptive. If Ports are empty, they are empty.
		// If a port is defined without protocol, K8s defaults to TCP.
	}

	for k, v := range spec.EnvVars {
		k8sContainer.Env = append(k8sContainer.Env, corev1.EnvVar{Name: k, Value: v})
	}

	// Map ResourceRequirements
	// Currently, modelmanager.ResourceRequirements only has RAM and Storage.
	// CPU and Accelerators would need to be added to that struct or passed via CustomProviderConfig.
	if spec.ResourceRequest.RAM != nil && *spec.ResourceRequest.RAM > 0 {
		ramQuantity := resource.MustParse(fmt.Sprintf("%dMi", *spec.ResourceRequest.RAM))
		k8sContainer.Resources.Requests[corev1.ResourceMemory] = ramQuantity
		k8sContainer.Resources.Limits[corev1.ResourceMemory] = ramQuantity // Typically set limits same as requests for critical workloads
	}
	// TODO: Map CPU requests/limits when available in spec.ResourceRequest
	// Example: if spec.ResourceRequest.CPU != "" {
	// 	cpuQuantity := resource.MustParse(spec.ResourceRequest.CPU)
	// 	k8sContainer.Resources.Requests[corev1.ResourceCPU] = cpuQuantity
	// 	k8sContainer.Resources.Limits[corev1.ResourceCPU] = cpuQuantity
	// }

	// TODO: Map Accelerator requests/limits when available in spec.ResourceRequest
	// Example: for _, accel := range spec.ResourceRequest.Accelerators {
	// 	accelQuantity := resource.MustParse(fmt.Sprintf("%d", accel.Count))
	// 	k8sContainer.Resources.Requests[corev1.ResourceName(accel.Type)] = accelQuantity
	// 	k8sContainer.Resources.Limits[corev1.ResourceName(accel.Type)] = accelQuantity
	// }

	// Map VolumeMounts
	for _, vm := range spec.VolumeMounts {
		k8sContainer.VolumeMounts = append(k8sContainer.VolumeMounts, corev1.VolumeMount{
			Name:      vm.Name,
			MountPath: vm.MountPath,
			ReadOnly:  vm.ReadOnly,
		})
	}

	k8sPod.Spec.Containers = append(k8sPod.Spec.Containers, k8sContainer)

	// 5. Create the Pod using Kubernetes API
	createdK8sPod, err := p.clientset.CoreV1().Pods(p.targetNamespace).Create(ctx, k8sPod, metav1.CreateOptions{})
	if err != nil {
		if k8sAPIErrors.IsAlreadyExists(err) {
			return Pod{}, fmt.Errorf("%w: pod '%s' already exists in namespace '%s': %v", ErrPodAlreadyExists, podName, p.targetNamespace, err)
		}
		return Pod{}, fmt.Errorf("%w: failed to create Kubernetes pod '%s': %v", ErrDeploymentFailed, podName, err)
	}

	// 6. Convert created Kubernetes Pod to inventorymanager.Pod
	return p.k8sPodToInventoryPod(createdK8sPod, &spec), nil
}

// RemovePod is a stub implementation.
func (p *KubernetesNodeProvider) RemovePod(ctx context.Context, localPodID string) error {
	// First, verify the pod exists and is managed by this provider to avoid deleting unrelated pods.
	_, err := p.GetPodByID(ctx, localPodID) // GetPodByID includes the managed-by check
	if err != nil {
		if errors.Is(err, ErrPodNotFound) { // Uses standard library errors.Is
			return fmt.Errorf("%w: cannot remove pod '%s', as it's not found or not managed by this provider", ErrPodNotFound, localPodID)
		}
		return fmt.Errorf("failed to verify pod '%s' before removal: %w", localPodID, err)
	}

	deletePolicy := metav1.DeletePropagationForeground // Or Background, Orphan
	err = p.clientset.CoreV1().Pods(p.targetNamespace).Delete(ctx, localPodID, metav1.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	})
	if err != nil {
		if k8sAPIErrors.IsNotFound(err) { // Uses k8s.io/apimachinery/pkg/api/errors
			// If GetPodByID found it, but Delete now says not found, it might have been deleted concurrently.
			return fmt.Errorf("%w: pod '%s' was not found during delete operation (possibly deleted concurrently)", ErrPodNotFound, localPodID)
		}
		return fmt.Errorf("failed to delete Kubernetes pod '%s': %w", localPodID, err)
	}
	return nil
}

// GetPodByID retrieves a specific pod by its local pod ID (Kubernetes pod name).
// The pod must be in the provider's targetNamespace and have the clowder management labels.
func (p *KubernetesNodeProvider) GetPodByID(ctx context.Context, localPodID string) (Pod, error) {
	k8sPod, err := p.clientset.CoreV1().Pods(p.targetNamespace).Get(ctx, localPodID, metav1.GetOptions{})
	if err != nil {
		if k8sAPIErrors.IsNotFound(err) { // Uses k8s.io/apimachinery/pkg/api/errors
			return Pod{}, fmt.Errorf("%w: Kubernetes pod '%s' not found in namespace '%s'", ErrPodNotFound, localPodID, p.targetNamespace)
		}
		return Pod{}, fmt.Errorf("failed to get Kubernetes pod '%s': %w", localPodID, err)
	}

	// Verify it's a pod managed by this controller and namespace
	if managedBy, ok := k8sPod.Labels["clowder.io/managed-by"]; !ok || managedBy != "clowd-control" {
		return Pod{}, fmt.Errorf("%w: pod '%s' is not managed by clowd-control", ErrPodNotFound, localPodID)
	}
	if nsLabel, ok := k8sPod.Labels[clowderNamespaceLabelKey]; !ok || nsLabel != p.targetNamespace {
		return Pod{}, fmt.Errorf("%w: pod '%s' does not belong to target namespace '%s' (mismatched label '%s')", ErrPodNotFound, localPodID, p.targetNamespace, clowderNamespaceLabelKey)
	}

	return p.k8sPodToInventoryPod(k8sPod, nil), nil
}

// ListPods returns a slice of pods from Kubernetes, filtered according to ListPodFilters.
// All pods must be in the provider's targetNamespace and have clowder management labels.
func (p *KubernetesNodeProvider) ListPods(ctx context.Context, filters ListPodFilters) ([]Pod, error) {
	listOptions := metav1.ListOptions{}
	labelSet := map[string]string{
		"clowder.io/managed-by":  "clowd-control",
		clowderNamespaceLabelKey: p.targetNamespace, // Ensure we only list pods from our designated scope
	}

	if filters.ModelID != "" {
		labelSet["clowder.io/model-id"] = filters.ModelID
	}
	for k, v := range filters.Labels {
		// User-provided labels should not override internal ones
		if _, isInternalKey := labelSet[k]; !isInternalKey {
			labelSet[k] = v
		}
	}
	listOptions.LabelSelector = k8sLabels.SelectorFromSet(labelSet).String()

	if filters.NodeID != "" { // NodeID in filters is localNodeID for the provider
		listOptions.FieldSelector = fields.OneTermEqualSelector("spec.nodeName", filters.NodeID).String()
	}

	k8sPodList, err := p.clientset.CoreV1().Pods(p.targetNamespace).List(ctx, listOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to list Kubernetes pods with selector '%s' and field selector '%s': %w", listOptions.LabelSelector, listOptions.FieldSelector, err)
	}

	var inventoryPods []Pod
	for i := range k8sPodList.Items {
		k8sPod := &k8sPodList.Items[i] // Use pointer to item
		invPod := p.k8sPodToInventoryPod(k8sPod, nil)

		if filters.Status != "" && invPod.Status != filters.Status {
			continue
		}
		inventoryPods = append(inventoryPods, invPod)
	}

	return inventoryPods, nil
}
