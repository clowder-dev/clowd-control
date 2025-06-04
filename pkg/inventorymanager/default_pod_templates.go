package inventorymanager

import "github.com/aifoundry-org/clowd-control/pkg/modelmanager"

var (
	// Helper for defining default resource values in templates if needed.
	// Example: defaultRAMForTemplate = 1024 (MB)
	// However, for llama.cpp, RAM is a required parameter.
	// We use a zero value pointer for RAM in the template,
	// which will be filled by the "ram_mb_request" parameter.
	// If a parameter is not required and has no default, its corresponding
	// field in PodSpecification might remain zero/nil if not set by template logic.
	emptyRamForTemplate *int // This will be nil, Render logic handles it.
)

// DefaultPodTemplates holds the built-in pod template definitions.
// These are data structures that define how a PodSpecification should be rendered.
var DefaultPodTemplates = []PodTemplateDefinition{
	{
		IDValue:          "llama-cpp-server",
		DescriptionValue: "Deploys a llama.cpp OpenAI-compatible server. Mounts a model file from a volume.",
		ParametersValue: []PodTemplateParameter{
			{Name: "model_id", Description: "The model ID this pod will serve.", Type: ParameterTypeString, Required: true},
			{Name: "model_file_name", Description: "Filename of the GGUF model (e.g., 'Llama-3-8B-Instruct-Q4_K_M.gguf').", Type: ParameterTypeString, Required: true},
			{Name: "model_volume_name", Description: "Name of the Kubernetes Volume containing the models.", Type: ParameterTypeString, DefaultValue: "model-storage"},
			{Name: "model_volume_mount_path", Description: "Mount path inside the container for the models volume.", Type: ParameterTypeString, DefaultValue: "/models"},
			{Name: "n_gpu_layers", Description: "Number of layers to offload to GPU. Use -1 for all available.", Type: ParameterTypeInt, DefaultValue: 0},
			{Name: "port", Description: "Container port for the llama.cpp server API.", Type: ParameterTypeInt, DefaultValue: 8080},
			{Name: "host_port", Description: "Host port to map to the container port. 0 for dynamic/none (K8s default).", Type: ParameterTypeInt, DefaultValue: 0},
			{Name: "image", Description: "Container image for llama.cpp server.", Type: ParameterTypeString, DefaultValue: "ghcr.io/ggerganov/llama.cpp:server"},
			{Name: "ram_mb_request", Description: "RAM request for the pod in MB.", Type: ParameterTypeInt, Required: true},
			{Name: "num_threads", Description: "Number of threads for llama.cpp processing.", Type: ParameterTypeInt, DefaultValue: 4},
		},
		SpecTemplate: PodSpecification{
			ModelID: "{{ .model_id }}", // Will be filled by the "model_id" parameter
			Image:   "{{ .image }}",    // Will be filled by the "image" parameter
			Ports: []PodPort{
				{
					Name:          "http-api",
					ContainerPort: 0, // Will be overridden by "port" parameter in Render logic
					HostPort:      0, // Will be overridden by "host_port" parameter in Render logic
					Protocol:      "TCP",
				},
			},
			VolumeMounts: []VolumeMount{
				{
					Name:      "{{ .model_volume_name }}",       // Parameter 'model_volume_name'
					MountPath: "{{ .model_volume_mount_path }}", // Parameter 'model_volume_mount_path'
					ReadOnly:  true,                             // Default, can be overridden if a param is introduced
				},
			},
			Args: []string{
				"--model", "{{ .model_volume_mount_path }}/{{ .model_file_name }}",
				"--port", "{{ .port }}", // text/template handles int to string conversion
				"--n-gpu-layers", "{{ .n_gpu_layers }}",
				"--n-threads", "{{ .num_threads }}",
				"--host", "0.0.0.0",
				// Example for a templated alias: "--alias", "{{ .model_id }}"
			},
			ResourceRequest: modelmanager.ResourceRequirements{
				RAM: emptyRamForTemplate, // Will be set by "ram_mb_request" parameter in Render logic
			},
			Labels: map[string]string{
				// "clowder.io/runtime-template" is added by Render logic.
				"clowder.io/model-id": "{{ .model_id }}", // Will be filled by "model_id" parameter
				// Add other static labels for this template if needed
			},
			// CustomProviderConfig can be defined here if parts of it are static for this template.
			// Example:
			// CustomProviderConfig: map[string]interface{}{
			//  "kubernetes": map[string]interface{}{
			//    "some_static_k8s_setting": "value",
			//  },
			// },
		},
	},
	// Future default templates can be added here.
	// Example:
	// {
	//  IDValue: "another-template",
	//  DescriptionValue: "Description for another template.",
	//  ParametersValue: []PodTemplateParameter{...},
	//  SpecTemplate: PodSpecification{...},
	// },
}
