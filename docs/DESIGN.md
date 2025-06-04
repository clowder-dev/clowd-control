# ClowdControl Design Notes

ClowdControl controller is a software used to control Clowder
GenAI distributed inference cluster, from managing models,
to making privisioning and load scheduling decisions.
Effectively it is a control plane part of the cluster.

## Architecture

Clowder cluster is a system comprised of several parts.

There are several layers to it:
- physical, representing individual computers (can be VMs with dedicated
  resources) with specific hardware configurations: RAM, CPU, accelerators etc.
- cluster (k8s), mapping to a hierarcies of nodes/pods/namespaces etc.
  We will use k8s for this. Each node is likely to match physical machine,
  but there can be exceptios. Some nodes, specifically the workers dedicated
  to inference workloads have resources (disk/cpu/ram/etc) accounted for.
- logical, represents parts of the system responsible for specific areas,
  eg. load balancer, scheduler, inference runtime etc. These can have complex
  assignment to cluster resources.

### Logical parts

1. Web UI service: expose op APIs and dashboards to the user.
2. Load balancer: accepts requests, calls scheduler. Primarily responsible for L4/L7 request distribution and invoking the scheduler.
3. Reverse proxy: Routes requests to the specific backend selected by the scheduler.
4. Analytics middleware (monitors traffic and pick usage, timing and other
   metrics).
5. Scheduler (evaluates request according to system state and decides on the
   optimal route). Pluggable (add adapters to support llm-d filters and
   sorters).
6. Inventory manager: keeps state of all available resources.
7. Model manager: manages collection of model metadata.
8. Scaler: analyzes demand (from analytics) and modifies cluster accordingly.
   Can call provisioning manager.
9. API service: exposes operational APIs to internal components (e.g., Scaler, Model Manager, Inventory Manager) and for direct system manipulation by administrators.
10. Storage manager - fetches and stores model data.
11. Runtime engine(-s): performs inference using local model and exposes
    standard inference API (eg OpenAI API compatible chat completions).
12. Aggregated API service: provides a user-facing facade for standard APIs (eg /v1/models) by aggregating data from the whole cluster, offering cluster-wide views.

Standard utils:
- ingress
- identity manager
- prometheus/grafana
- playground (Open Web UI)
- provisioning manager - platform specific implementation of
  Cluster API or analogous mechanism to provision/remove
  k8s nodes.

This project specifically implements the following parts:
- load balancer
- reverse proxy
- analytics middleware
- scheduler (framework and default policy)
- inventory manager
- model manager
- scaler (framework and default policy)
- operational API service
- aggregated API service

### Model Manager

The Model Manager is a core component responsible for the lifecycle and metadata
management of machine learning models within the Clowder cluster. It serves as
a central registry, providing other components with necessary information about
available models.

**Key Responsibilities:**

*   **Metadata Registry:** Stores, retrieves, and manages comprehensive metadata
    for each model. This includes, but is not limited to:
    *   Model name and unique identifier.
    *   Version information.
    *   Source URI (e.g., Hugging Face model ID, S3 path).
    *   Model format and type.
    *   Resource requirements (CPU, RAM, accelerator type/count).
    *   Input/output schemas.
    *   Licensing information.
*   **Model Discovery:** Enables other services (like the Operational API,
    Scaler, and Scheduler) to query and discover available models and their
    properties.
*   **Version Control:** Supports multiple versions of the same model, allowing
    for controlled rollouts and rollbacks.
*   **Interface for Administration:** Provides APIs (exposed via the Operational
    API Service) for administrators to add new models, update existing ones, or
    remove models from the cluster.
*   **Requirement Provisioning:** Supplies model requirement details (e.g.,
    hardware needs, container image) to components like the Scaler to facilitate
    resource allocation and deployment.

The Model Manager itself does not handle the physical storage or download of
model artifacts; this responsibility lies with the Storage Manager. However, it
provides the necessary pointers (like URIs) and metadata for the Storage Manager
to perform its tasks.

### Operational API Service

The Operational API Service is the primary entry point for administrators
and other internal components to manage and interact with the ClowdControl's
resources and functionalities. It exposes a RESTful API, typically versioned
(e.g., `/api/v1`), to perform various control plane operations.

**Key Responsibilities:**

*   **Expose Management Endpoints:** Provides HTTP endpoints for managing:
    *   Models (via the Model Manager): Adding, removing, listing, updating models.
    *   Cluster Inventory (via the Inventory Manager): Viewing node status, available resources.
    *   Scaling (via the Scaler): Configuring scaling policies, triggering manual scaling actions.
    *   Other controllable aspects of the system.
*   **Authentication and Authorization:** Integrates with identity management solutions to secure its endpoints, ensuring that only authorized users or services can perform operations.
*   **Request Validation:** Validates incoming API requests for correctness (e.g., proper format, required parameters).
*   **Coordination:** Acts as a facade, translating API calls into operations on the respective backend components (Model Manager, Inventory Manager, Scaler, etc.).
*   **Standardized Interface:** Offers a consistent and well-documented API for programmatic interaction with the ClowdControl.

This service is crucial for the overall manageability and automation of the Clowder cluster. It is distinct from the "Aggregated API Service," which is more user-facing for inference tasks (like listing available models for end-users), whereas the Operational API is for control and administration.

For now let's assume all the data is serializable, but the persistence will
be implemented later.

### Use case: inference workflow

1. User makes inference request to the ingress service.
2. User is authenticated.
3. Authorization layer selects resources available for the request.
4. Request goes to a load balancer/reverse proxy.
5. Load balancer asks scheduler to route the request to the best backend (or
   backends if serving prefill from a different pipeline).
6. Reverse proxy routes request to the selected worker pod.
7. Response is returned/streamed to the user at the same time collecting
   full metrics, from token counts to kv cache prefixes (digests?).

### Use case: Operational APIs

- User makes operation API request to add/remove model (model source URI
  and configuration).
- User sets scaling parameters and/or scaling policy. E.g. sets number of worker
  pods with specific models or enables latency requirements.
- User asks to provision additional physical resources (e.g. additional machine)
- User asks to provision/reassign cluster resources (eg assign more nodes to
  host workers).

### Use case: add new worker pod to serve specific model.

This process is typically orchestrated by the Scaler or initiated via the
Operational API service.
1. The responsible component (e.g., Scaler) gets model data from the Model Manager.
2. The component calls the Storage Manager on a target worker node and instructs
   it to download the model to local storage.
3. The component instantiates a new inference runtime pod on the worker node
   with all configuration applied (interacting with the k8s API).
4. The component notifies the Inventory Manager (which in turn may inform the
   Scheduler/Load Balancer) about changes in the available inference resources.

### Use case: autoscale (cluster level)

1. Autoscaler analyzes historical metrics (demand), available resources
   (capacity) and finds optimal resource allocation to fullfil demand
   according to scaling policy.
2. Difference from existing system state is calculated and converted into
   a list of imperative actions.
3. Actions (eg. instantiate runtime A with model B on a node C) are
   executed.

### Use case: observability

- Prometheus
- Grafana

## Internal APIs

This section outlines key interactions between the logical components
implemented by this project. These are conceptual and subject to detailed
design.

Provisioning (interacts with external provisioning manager):
- `GetHardwareCapacity()`
- `CreateNode(hardware_id, node_config)`
- `ReleaseNode(node_id)`

Scheduler <-> Inventory Manager:
- `InventoryManager.GetAvailableWorkers(model_id, constraints)`
// Note: Worker status updates are observed by the InventoryManager through its providers,
// not directly pushed to it by the Scheduler.

Scaler <-> Model Manager:
- `ModelManager.GetModelRequirements(model_id)`

Scaler <-> Inventory Manager:
- `InventoryManager.GetNodeByID(globalNodeID)`: To get details of a specific node.
- `InventoryManager.ListNodes(filters)`: To find suitable nodes based on labels, capacity, etc.
- `InventoryManager.DeployPod(globalNodeID, PodSpecification)`: Deploys a new inference pod for a given model on the specified node. The `PodSpecification` can be constructed directly or rendered from a `PodTemplateDefinition`. Returns the created `Pod` object. The `InventoryManager` delegates this to the appropriate `NodeProvider`.
- `InventoryManager.RemovePod(globalPodID)`: Removes/terminates an existing inference pod. The `InventoryManager` delegates this to the `NodeProvider` managing the node where the pod resides.
- `InventoryManager.GetPodByID(globalPodID)`: Retrieves the current state and details of a specific pod.
- `InventoryManager.ListPods(filters)`: Lists existing pods, filterable by criteria such as `NodeID`, `ModelID`, `Status`, `Labels`. `ListPodFilters` would be a struct:
    *   `NodeID`: `string` (optional)
    *   `ModelID`: `string` (optional)
    *   `Status`: `PodStatus` (optional)
    *   `Labels`: `map[string]string` (optional)

Operational API Service <-> Model Manager:
- `ModelManager.AddModel(params)`
- `ModelManager.RemoveModel(id)`
- `ModelManager.ListModels()`

Operational API Service <-> Scaler:
- `Scaler.SetScalingPolicy(policy)`
- `Scaler.GetScalingStatus()`

Operational API Service <-> Inventory Manager:
- `GET /api/v1/nodes` -> `InventoryManager.ListNodes(labels)`
- `GET /api/v1/nodes/{id}` -> `InventoryManager.GetNodeByID(node_id)`
// Note: Add/Remove/Update operations for nodes are typically not exposed directly via the InventoryManager's API.
// These actions are usually managed by the underlying infrastructure (e.g., Kubernetes, cloud provider)
// that the NodeProviders connect to. The InventoryManager reflects the state discovered from these backends.

These are hight level and WIP, some are definitelly missing.

## Development Guidelines

Project is developed in go language.

Project is developed under https://github.com/aifoundry-org/clowd-control
namespace.

Individual parts of the project are implemented as packages
under `pkg` folder each in its own subfolder. The main binary is implemented
under `cmd/cli` folder.

There should be minimal amount of external dependencies. Only add new dependencies
where benefits outweigh the cons. Include only high quality dependencies.
External packages should be audited and this cost should not be dismissed.
When possible prefer standard lib packages.

Could should be as decoupled as possible. Use interfaces and dependency-injection
(or analogous methods) to keep individual code modules self-contained. "Module"
here can be package, but also an interface or even a function.

Try to express business logic in functional manner, following
"functional core, imperative shell" pattern. Use idepotency, referential
tranparency and similar practices. Only the "glue" code should be written
in imperative style.

Export only what's neccessary, avoid leaking implementation details.

All the code should be thread-safe. Avoid using goroutines
and channels as public interfaces, prefer functions (similar to how
Erlang/OTP code is usually organized) with "async" parts abstracted away.

Each feature should start from documenting it in the README,
then implementing tests and only then - implementation itself.

## Future Considerations

The following aspects are noted for future expansion or detailed design:

- **High-Level Diagram:** A visual block diagram of the main logical
  components and their primary interactions would be beneficial.
- **Data Models:** Briefly outlining key data entities will be important.
    *   **`ModelMetadata`**: (Already implicitly defined by Model Manager) Stores comprehensive information about a machine learning model, including ID, name, version, source URI, format, type, resource requirements, licensing, etc.
    *   **`Node`**: (Already implicitly defined by Inventory Manager) Represents a compute node in the cluster, including ID, status, capacity, allocatable resources, labels, taints.
    *   **`PodPort`**: Defines a network port for a pod.
        *   `Name`: `string` (e.g., "api", "metrics")
        *   `ContainerPort`: `int` (Port inside the pod/container)
        *   `HostPort`: `int` (Optional. Port on the host node. If not specified, may be dynamically assigned by the backend.)
        *   `Protocol`: `string` (e.g., "TCP", "UDP", defaults to "TCP")
    *   **`VolumeMount`**: Describes a mounting of a volume within a container.
        *   `Name`: `string` (This must match the Name of a Volume in the Pod's spec.)
        *   `MountPath`: `string` (Path within the container at which the volume should be mounted.)
        *   `ReadOnly`: `bool` (Optional. Mounted read-only if true.)
    *   **`PodStatus`**: An enumeration representing the state of a pod.
        *   `Pending`: The pod has been accepted by the system, but one or more of its components has not been created or is not yet running.
        *   `Running`: The pod has been bound to a node, and all of its essential components are running.
        *   `Succeeded`: All components in the pod have terminated successfully.
        *   `Failed`: At least one component in the pod has terminated with a failure.
        *   `Terminating`: The pod is in the process of being removed from the node.
        *   `Unknown`: The state of the pod could not be obtained.
    *   **`PodSpecification`**: Defines the desired state and configuration for deploying a new inference runtime instance (pod). This is the input to the `DeployPod` method.
        *   `ModelID`: `string` (Required. ID of the model this pod will serve, links to `ModelMetadata`)
        *   `Image`: `string` (Optional. Container image to use. If not provided, the system might infer it from the model or use a default runtime image.)
        *   `Ports`: `[]PodPort` (Optional. Network ports to expose.)
        *   `EnvVars`: `map[string]string` (Optional. Environment variables to set for the pod.)
        *   `Command`: `[]string` (Optional. Entrypoint command for the container. Overrides image default.)
        *   `Args`: `[]string` (Optional. Arguments to the command.)
        *   `VolumeMounts`: `[]VolumeMount` (Optional. Describes how volumes should be mounted into the container. The volumes themselves must be defined elsewhere, e.g., via `CustomProviderConfig` for Kubernetes `PodSpec.Volumes` or be pre-existing host paths if the runtime supports direct mounting of assumed paths without explicit pod volume definitions.)
        *   `ResourceRequest`: `ResourceRequirements` (Required. Specifies resources like CPU, RAM, GPU type/count needed by the pod. Typically derived from `ModelMetadata.Resources`.)
        *   `Labels`: `map[string]string` (Optional. Key-value pairs to attach to the pod for organization and selection.)
        *   `CustomProviderConfig`: `map[string]interface{}` (Optional. Backend-specific configuration. For Kubernetes, this could include `PodSpec.Volumes` definitions, annotations, specific volume mounts, security contexts, etc.)
    *   **`Pod`**: Represents an instance of an inference runtime, including its current state and configuration. This is the object returned by `GetPodByID`, `ListPods`, and `DeployPod`.
        *   `ID`: `string` (Globally unique identifier for the pod instance, assigned by the system upon creation. e.g., `backendID:<localPodUUID>` or just `<globalPodUUID>`)
        *   `NodeID`: `string` (Global ID of the node where the pod is deployed/running.)
        *   `Specification`: `PodSpecification` (The original specification used to create this pod.)
        *   `Status`: `PodStatus` (Current state of the pod.)
        *   `Message`: `string` (Optional. Human-readable message providing more details about the current status, especially for `Failed` or `Pending` states.)
        *   `CreatedAt`: `time.Time` (Timestamp of when the pod was created.)
        *   `UpdatedAt`: `time.Time` (Timestamp of the last status update.)
        *   `ActualPorts`: `[]PodPort` (Actual network ports, including host ports if dynamically assigned.)
        *   `CustomProviderStatus`: `map[string]interface{}` (Optional. Backend-specific status details. For Kubernetes, this could include pod IP, conditions, etc.)
    *   **`PodTemplateParameterType`**: An enumeration for the type of a template parameter.
        *   `string`, `int`, `bool`
    *   **`PodTemplateParameter`**: Defines a parameter for a `PodTemplate`.
        *   `Name`: `string` (Name of the parameter)
        *   `Description`: `string` (Human-readable description)
        *   `Type`: `PodTemplateParameterType` (Data type of the parameter)
        *   `DefaultValue`: `interface{}` (Optional. Default value if not provided by the user.)
        *   `Required`: `bool` (Indicates if the parameter must be provided by the user if no default is set.)
    *   **`PodTemplateDefinition`**: (Interface) Represents a predefined, parameterizable template for creating `PodSpecification`s.
        *   `ID()`: `string` (Unique identifier for the template, e.g., "llama-cpp-server")
        *   `Description()`: `string` (Human-readable description of the template)
        *   `Parameters()`: `[]PodTemplateParameter` (List of parameters the template accepts)
        *   `Render(params map[string]interface{})`: `(PodSpecification, error)` (Method to generate a `PodSpecification` using provided parameters)
    *   **`WorkerNodeState`**: (As previously mentioned) Detailed status and capabilities of a worker node. Likely superseded/detailed by the `Node` and `Pod` models.
    *   **`RequestMetrics`**: Data related to inference requests (e.g., latency, token counts, success/error rates).
    *   **`ScalingPolicy`**: Configuration defining how the cluster should scale (e.g., target utilization, min/max instances, model-specific rules).
- **Configuration Management:** Detailing how various components are configured
  (e.g., environment variables, config files, central configuration service).
- **Security Considerations:** Expanding on security aspects beyond initial
  authentication, such as API authorization strategies, securing inter-component
  communication (e.g., mTLS), and data protection.
