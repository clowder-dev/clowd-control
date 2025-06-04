package modelmanager

// ResourceRequirements specifies the computational resources needed for a model.
type ResourceRequirements struct {
	RAM     *int `json:"ram_mb,omitempty"`     // RAM required in MB, e.g., 4096 (for 4GB)
	Storage *int `json:"storage_mb,omitempty"` // Disk space for the model itself in MB, e.g., 10240 (for 10GB)
}

// ModelMetadata holds all the relevant information about a machine learning model.
type ModelMetadata struct {
	ID               string               `json:"id"`
	Name             string               `json:"name"`
	Version          string               `json:"version"`
	SourceURI        string               `json:"source_uri"`
	Format           string               `json:"format"`
	Type             string               `json:"type"`
	Resources        ResourceRequirements `json:"resources"`
	Licensing        string               `json:"licensing,omitempty"`
	CustomProperties map[string]string    `json:"custom_properties,omitempty"` // Any other custom metadata as key-value pairs
	GGUFMeta         map[string]interface{} `json:"gguf_meta,omitempty"`       // Parsed metadata from GGUF prefix.
}
