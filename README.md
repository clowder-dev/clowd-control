```

## API Reference

The ClowdControl controller exposes an Operational API for management and monitoring.
All API endpoints are prefixed with `/api/v1`.

### Health Check

*   **Endpoint:** `GET /health`
*   **Description:** Checks the health status of the API server.
*   **Success Response (200 OK):**
    ```json
    {
      "status": "ok"
    }
    ```

### Model Management

These endpoints allow for the management of model metadata within the controller.

*   **Endpoint:** `GET /models`
    *   **Description:** Lists all available models.
    *   **Success Response (200 OK):**
        ```json
        [
          {
            "id": "model-1",
            "name": "Llama-2-7b",
            "version": "1.0",
            "source_uri": "meta-llama/Llama-2-7b-chat-hf",
            "format": "gguf",
            "type": "language_model",
            "resources": {
              "ram_mb": 8192,
              "storage_mb": 7000
            },
            "licensing": "Llama 2 Community License",
            "custom_properties": {
              "quantization": "Q4_K_M"
            }
          }
        ]
        ```
        Returns an empty array `[]` if no models are present.

*   **Endpoint:** `POST /models`
    *   **Description:** Adds a new model.
        The request body should be a `ModelMetadata` JSON object.
        If the `source_uri` uses the Hugging Face scheme (`hf:///`), the controller will attempt to automatically fetch and populate most metadata fields (like `name`, `format`, `type`, `resources`, `licensing`, `tags`) directly from Hugging Face. For this to work, the `HF_TOKEN` environment variable must be set with a valid Hugging Face API token on the controller.
        When using an `hf:///` URI, you only need to provide the `id` and `source_uri`. Other fields are optional and, if provided, may override the values fetched from Hugging Face.
    *   **Request Body Examples:**
        *   Minimal request for a Hugging Face model (metadata will be auto-populated):
            ```json
            {
              "id": "my-smollm-model",
              "source_uri": "hf:///unsloth/SmolLM2-135M-Instruct-GGUF/SmolLM2-135M-Instruct-Q4_K_M.gguf"
            }
            ```
        *   Request for a non-Hugging Face model or to manually specify all details:
            ```json
            {
              "id": "model-2",
              "name": "Mistral-7B-Instruct",
              "version": "0.2",
              "source_uri": "s3://my-bucket/mistralai/Mistral-7B-Instruct-v0.2",
              "format": "gguf",
              "type": "language_model",
              "resources": {
                "ram_mb": 8000,
                "storage_mb": 7500
              }
            }
            ```
    *   **Success Response (201 Created):** The created `ModelMetadata` object, including any auto-populated fields if an `hf:///` URI was used.
    *   **Error Responses:**
        *   `400 Bad Request`: If the request payload is invalid (e.g., missing ID, malformed JSON, missing `HF_TOKEN` when `hf:///` URI is used and auto-population fails).
        *   `409 Conflict`: If a model with the same ID already exists.
        *   `500 Internal Server Error`: For other server-side errors.

*   **Endpoint:** `GET /models/{id}`
    *   **Description:** Retrieves a specific model by its ID.
    *   **Path Parameter:** `id` (string) - The unique identifier of the model.
    *   **Success Response (200 OK):** The `ModelMetadata` object.
    *   **Error Responses:**
        *   `400 Bad Request`: If the model ID is not provided in the path.
        *   `404 Not Found`: If the model with the specified ID does not exist.
        *   `500 Internal Server Error`: For other server-side errors.

*   **Endpoint:** `DELETE /models/{id}`
    *   **Description:** Removes a specific model by its ID.
    *   **Path Parameter:** `id` (string) - The unique identifier of the model.
    *   **Success Response (204 No Content):** An empty response.
    *   **Error Responses:**
        *   `400 Bad Request`: If the model ID is not provided in the path.
        *   `404 Not Found`: If the model with the specified ID does not exist.
        *   `500 Internal Server Error`: For other server-side errors.

### Inventory Management

These endpoints allow for querying node information from the Inventory Manager. Node IDs are global, typically in the format `backendID:localNodeID`.

*   **Endpoint:** `GET /nodes`
    *   **Description:** Lists all nodes registered with the Inventory Manager. Supports filtering by labels using query parameters. For example, `?label.region=us-east-1&label.instance-type=gpu` would filter for nodes with both labels. If a key is provided without `label.` prefix (e.g. `?region=us-east-1`), it will also be treated as a label filter.
    *   **Success Response (200 OK):**
        ```json
        [
          {
            "id": "kubernetes-prod:node-1-worker",
            "name": "k8s-worker-node-1",
            "status": "Ready",
            "address": "192.168.1.101",
            "capacity": {
              "cpu": "8",
              "ram_mb": 32768,
              "storage_gb": 500,
              "accelerators": [
                {
                  "type": "nvidia-tesla-t4",
                  "count": 1
                }
              ]
            },
            "allocatable": {
              "cpu": "7500m",
              "ram_mb": 30720,
              "storage_gb": 480
            },
            "labels": {
              "region": "us-east-1",
              "instance-type": "gpu-standard",
              "clowder.io/namespace": "production"
            },
            "taints": ["gpu=true:NoSchedule"]
          }
        ]
        ```
        Returns an empty array `[]` if no nodes are found or match the filter.
    *   **Error Responses:**
        *   `500 Internal Server Error`: If there's an issue fetching nodes from one or more providers.

*   **Endpoint:** `GET /nodes/{id}`
    *   **Description:** Retrieves a specific node by its global ID.
    *   **Path Parameter:** `id` (string) - The global unique identifier of the node (e.g., `kubernetes-prod:node-1-worker`).
    *   **Success Response (200 OK):** A single `Node` object.
        ```json
        {
          "id": "kubernetes-prod:node-1-worker",
          "name": "k8s-worker-node-1",
          "status": "Ready",
          "address": "192.168.1.101",
          "capacity": {
            "cpu": "8",
            "ram_mb": 32768,
            "storage_gb": 500,
            "accelerators": [
              {
                "type": "nvidia-tesla-t4",
                "count": 1
              }
            ]
          },
          "allocatable": {
            "cpu": "7500m",
            "ram_mb": 30720,
            "storage_gb": 480
          },
          "labels": {
            "region": "us-east-1",
            "instance-type": "gpu-standard",
            "clowder.io/namespace": "production"
          },
          "taints": ["gpu=true:NoSchedule"]
        }
        ```
    *   **Error Responses:**
        *   `400 Bad Request`: If the node ID format is invalid.
        *   `404 Not Found`: If the node with the specified ID (or its backend provider) does not exist.
        *   `500 Internal Server Error`: For other server-side errors.

*   **Endpoint:** `POST /nodes/{node_id}/pods`
    *   **Description:** Deploys a new pod (inference runtime) on a specific node.
        The request body can either be a full `PodSpecification` object or specify a `template_id` along with `template_params` to use a predefined pod template.
    *   **Path Parameter:** `node_id` (string) - The global unique identifier of the node (e.g., `kubernetes-prod:node-1-worker`).
    *   **Request Body Structure:**
        ```json
        {
          "specification": { /* PodSpecification object, OR */ },
          "template_id": "template-name",
          "template_params": { /* map of parameters for the template */ }
        }
        ```
    *   **Request Body Example (Direct Specification):**
        ```json
        {
          "specification": {
            "model_id": "my-smollm-model",
            "image": "unsloth/llama-3-8b-instruct-gguf:latest",
            "resource_request": {
              "ram_mb": 4096
            },
            "ports": [
              {
                "name": "http-api",
                "container_port": 8080
              }
            ],
            "volume_mounts": [
              {
                "name": "model-storage",
                "mount_path": "/models",
                "read_only": true
              },
              {
                "name": "hf-cache",
                "mount_path": "/root/.cache/huggingface"
              }
            ],
            "labels": {
              "service": "inference",
              "environment": "staging"
            },
            "custom_provider_config": {
              "kubernetes": {
                "pod_spec_volumes": [
                  {
                    "name": "model-storage",
                    "hostPath": {
                      "path": "/mnt/shared/models",
                      "type": "Directory"
                    }
                  },
                  {
                    "name": "hf-cache",
                    "emptyDir": {}
                  }
                ]
              }
            }
          }
        }
        ```
    *   **Request Body Example (Template-based - `llama-cpp-server`):**
        Available `llama-cpp-server` template parameters:
        *   `model_id` (string, required): model ID.
        *   `model_file_name` (string, required): GGUF model filename (e.g., "Llama-3-8B-Instruct-Q4_K_M.gguf").
        *   `ram_mb_request` (int, required): RAM request in MB.
        *   `model_volume_name` (string, default: "model-storage"): Kubernetes Volume name for models.
        *   `model_volume_mount_path` (string, default: "/models"): Container mount path for models.
        *   `n_gpu_layers` (int, default: 0): GPU layers for llama.cpp.
        *   `port` (int, default: 8080): Container port for llama.cpp API.
        *   `host_port` (int, default: 0): Host port to map.
        *   `image` (string, default: "ghcr.io/ggerganov/llama.cpp:server"): Container image.
        *   `num_threads` (int, default: 4): Threads for llama.cpp.
        ```json
        {
          "template_id": "llama-cpp-server",
          "template_params": {
            "model_id": "my-llama3-8b",
            "model_file_name": "Meta-Llama-3-8B-Instruct.Q4_K_M.gguf",
            "ram_mb_request": 8192,
            "n_gpu_layers": -1,
            "port": 8081
          }
        }
        ```
        **Note on Volumes:** The `volume_mounts` field (in direct specification) or parameters like `model_volume_name` (in templates) specify how volumes are mounted into the container. The actual definition of these volumes (e.g., `hostPath`, `persistentVolumeClaim`, `emptyDir`) typically needs to be provided through the `custom_provider_config` for the specific backend (like Kubernetes `PodSpec.Volumes`), or the volumes must be pre-configured on the node if the provider supports direct mounting of assumed paths. The `llama-cpp-server` template assumes the volume named by `model_volume_name` is already available to the Kubernetes node and does not define it via `custom_provider_config` by default.
    *   **Success Response (201 Created):** The created `Pod` object.
    *   **Error Responses:**
        *   `400 Bad Request`: If the node ID is invalid, or the request payload is invalid (e.g., missing required fields/parameters, providing both specification and template, template rendering fails).
        *   `404 Not Found`: If the specified node or pod template does not exist.
        *   `500 Internal Server Error`: For other server-side errors (e.g., pod deployment failed on the backend).

## Development
          "image": "unsloth/llama-3-8b-instruct-gguf:latest",
          "resource_request": {
            "ram_mb": 4096
          },
          "ports": [
            {
              "name": "http-api",
              "container_port": 8080
            }
          ],
          "volume_mounts": [
            {
              "name": "model-storage",
              "mount_path": "/models",
              "read_only": true
            },
            {
              "name": "hf-cache",
              "mount_path": "/root/.cache/huggingface"
            }
          ],
          "labels": {
            "service": "inference",
            "environment": "staging"
          },
          "custom_provider_config": {
            "kubernetes": {
              "pod_spec_volumes": [
                {
                  "name": "model-storage",
                  "hostPath": {
                    "path": "/mnt/shared/models",
                    "type": "Directory"
                  }
                },
                {
                  "name": "hf-cache",
                  "emptyDir": {}
                }
              ]
            }
          }
        }
        ```
        **Note on Volumes:** The `volume_mounts` field specifies how volumes are mounted into the container. The actual definition of these volumes (e.g., `hostPath`, `persistentVolumeClaim`, `emptyDir`) typically needs to be provided through the `custom_provider_config` for the specific backend (like Kubernetes `PodSpec.Volumes`), or the volumes must be pre-configured on the node if the provider supports direct mounting of assumed paths.
    *   **Success Response (201 Created):** The created `Pod` object.
    *   **Error Responses:**
        *   `400 Bad Request`: If the node ID is invalid, or the request payload is invalid (e.g., missing `model_id`, `image`, or `resource_request.ram_mb`).
        *   `404 Not Found`: If the specified node does not exist.
        *   `500 Internal Server Error`: For other server-side errors (e.g., pod deployment failed on the backend).

## Development

This section provides guidance for setting up a local development environment.

### Prerequisites

*   **Go:** Version 1.21 or later.
*   **Docker:** For building container images (optional, if you plan to run the controller in a container).
*   **Minikube:** For running a local Kubernetes cluster. Install from [Minikube's official documentation](https://minikube.sigs.k8s.io/docs/start/).
*   **kubectl:** For interacting with the Kubernetes cluster. Install from [Kubernetes official documentation](https://kubernetes.io/docs/tasks/tools/install-kubectl/).

### Local Kubernetes Cluster (Minikube)

Helper scripts are provided in the `scripts/` directory to manage a Minikube cluster for development.

*   **Setting up the cluster:**
    ```bash
    bash scripts/minikube_setup.sh
    ```
    This script will:
    1.  Start a Minikube cluster with the profile name `clowd-control-dev`.
    2.  Enable the Minikube registry addon (useful for local image development).
    3.  Create a Kubernetes namespace named `clowd-control-dev-ns`.
    4.  Label the Minikube node(s) with `clowder.io/namespace=clowd-control-dev-ns`. This label is used by the `KubernetesNodeProvider` to discover and manage nodes within this namespace.
    5.  Output instructions to point your local Docker client to Minikube's Docker daemon, which is useful if you build container images locally and want them to be available within Minikube without pushing to an external registry.

    When configuring the `KubernetesNodeProvider` for local development, ensure its `targetNamespace` parameter is set to `clowd-control-dev-ns`.

*   **Destroying the cluster:**
    ```bash
    bash scripts/minikube_destroy.sh
    ```
    This script will:
    1.  Stop the Minikube cluster associated with the `clowd-control-dev` profile.
    2.  Delete the Minikube cluster.

### Building and Running

(Instructions for building and running the controller will be added here once the main application entry point is defined.)
