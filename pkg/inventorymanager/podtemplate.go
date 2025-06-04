package inventorymanager

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv" // For converting params
	"strings"
	"text/template"
)

// PodTemplateParameterType defines the type of a template parameter.
type PodTemplateParameterType string

const (
	ParameterTypeString PodTemplateParameterType = "string"
	ParameterTypeInt    PodTemplateParameterType = "int"
	ParameterTypeBool   PodTemplateParameterType = "bool"
)

// PodTemplateParameter defines a single parameter for a PodTemplate.
// These fields are typically exported for serialization (e.g., JSON).
type PodTemplateParameter struct {
	Name         string                   `json:"name"`
	Description  string                   `json:"description"`
	Type         PodTemplateParameterType `json:"type"`
	DefaultValue any                      `json:"default_value,omitempty"`
	Required     bool                     `json:"required,omitempty"`
}

// PodTemplateDefinition defines a pod template using data structures.
// It can be serialized to/from JSON or other formats.
type PodTemplateDefinition struct {
	IDValue          string                 `json:"id"`
	DescriptionValue string                 `json:"description"`
	ParametersValue  []PodTemplateParameter `json:"parameters"`
	SpecTemplate     PodSpecification       `json:"spec_template"` // The template for PodSpecification
}

// Render generates a PodSpecification from this template and user-provided parameters.
func (ptd *PodTemplateDefinition) Render(userParams map[string]any) (PodSpecification, error) {
	// 1. Process parameters (validation, defaults)
	processedParams := make(map[string]any)
	var err error
	for _, pDef := range ptd.ParametersValue {
		processedParams[pDef.Name], err = getParamValue(pDef, userParams)
		if err != nil {
			return PodSpecification{}, fmt.Errorf("error processing parameter '%s': %w", pDef.Name, err)
		}
	}

	// 2. Deep copy the SpecTemplate
	var spec PodSpecification
	templateBytes, err := json.Marshal(ptd.SpecTemplate)
	if err != nil {
		return PodSpecification{}, fmt.Errorf("failed to marshal spec template for copying: %w", err)
	}
	if err := json.Unmarshal(templateBytes, &spec); err != nil {
		return PodSpecification{}, fmt.Errorf("failed to unmarshal spec template for copying: %w", err)
	}

	// 3. Apply parameters to the copied spec
	applyStrTpl := func(tplStr string) (string, error) {
		if !strings.Contains(tplStr, "{{") {
			return tplStr, nil
		}
		tmpl, err := template.New(tplStr).Parse(tplStr)
		if err != nil {
			return "", fmt.Errorf("failed to parse template string '%s': %w", tplStr, err)
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, processedParams); err != nil {
			return "", fmt.Errorf("failed to execute template string '%s' with params: %w", tplStr, err)
		}
		return buf.String(), nil
	}

	spec.ModelID, err = applyStrTpl(spec.ModelID)
	if err != nil {
		return PodSpecification{}, fmt.Errorf("failed to render ModelID: %w", err)
	}
	spec.Image, err = applyStrTpl(spec.Image)
	if err != nil {
		return PodSpecification{}, fmt.Errorf("failed to render Image: %w", err)
	}

	for i, argTpl := range spec.Args {
		spec.Args[i], err = applyStrTpl(argTpl)
		if err != nil {
			return PodSpecification{}, fmt.Errorf("failed to render arg template '%s': %w", argTpl, err)
		}
	}

	if spec.EnvVars == nil && len(ptd.SpecTemplate.EnvVars) > 0 {
		spec.EnvVars = make(map[string]string)
	}
	for key, valTpl := range ptd.SpecTemplate.EnvVars { // Iterate original template to get all keys
		spec.EnvVars[key], err = applyStrTpl(valTpl)
		if err != nil {
			return PodSpecification{}, fmt.Errorf("failed to render env var template for key '%s': %w", key, err)
		}
	}

	if spec.Labels == nil && len(ptd.SpecTemplate.Labels) > 0 {
		spec.Labels = make(map[string]string)
	}
	for key, valTpl := range ptd.SpecTemplate.Labels { // Iterate original template to get all keys
		spec.Labels[key], err = applyStrTpl(valTpl)
		if err != nil {
			return PodSpecification{}, fmt.Errorf("failed to render label template for key '%s': %w", key, err)
		}
	}
	// Ensure standard labels
	if spec.Labels == nil {
		spec.Labels = make(map[string]string)
	}
	spec.Labels["clowder.io/runtime-template"] = ptd.IDValue

	for i, vmTpl := range spec.VolumeMounts {
		spec.VolumeMounts[i].Name, err = applyStrTpl(vmTpl.Name)
		if err != nil {
			return PodSpecification{}, fmt.Errorf("failed to render volume mount name template '%s': %w", vmTpl.Name, err)
		}
		spec.VolumeMounts[i].MountPath, err = applyStrTpl(vmTpl.MountPath)
		if err != nil {
			return PodSpecification{}, fmt.Errorf("failed to render volume mount path template '%s': %w", vmTpl.MountPath, err)
		}
		// ReadOnly is bool, typically not templated with string templates.
		// It would be set in SpecTemplate or by a specific boolean parameter if needed.
	}

	for i, portTpl := range spec.Ports {
		spec.Ports[i].Name, err = applyStrTpl(portTpl.Name)
		if err != nil {
			return PodSpecification{}, fmt.Errorf("failed to render port name template '%s': %w", portTpl.Name, err)
		}
		if pVal, ok := processedParams["port"].(int); ok && i == 0 { // Apply to first port
			spec.Ports[i].ContainerPort = pVal
		}
		if hpVal, ok := processedParams["host_port"].(int); ok && i == 0 { // Apply to first port
			spec.Ports[i].HostPort = hpVal
		}
		spec.Ports[i].Protocol, err = applyStrTpl(portTpl.Protocol)
		if err != nil {
			return PodSpecification{}, fmt.Errorf("failed to render port protocol template '%s': %w", portTpl.Protocol, err)
		}
	}

	if ramVal, ok := processedParams["ram_mb_request"].(int); ok {
		if spec.ResourceRequest.RAM == nil {
			spec.ResourceRequest.RAM = new(int)
		}
		*spec.ResourceRequest.RAM = ramVal
	}
	if storageVal, ok := processedParams["storage_mb_request"].(int); ok {
		if spec.ResourceRequest.Storage == nil {
			spec.ResourceRequest.Storage = new(int)
		}
		*spec.ResourceRequest.Storage = storageVal
	}
	// CustomProviderConfig is typically not deeply templated via this simple mechanism.
	// It would be defined in SpecTemplate.

	return spec, nil
}

// Helper to get and type-check parameter value
func getParamValue(paramDef PodTemplateParameter, userParams map[string]any) (any, error) {
	val, userProvided := userParams[paramDef.Name]

	if !userProvided {
		if paramDef.Required {
			return nil, fmt.Errorf("required parameter '%s' not provided", paramDef.Name)
		}
		val = paramDef.DefaultValue
	}

	if val == nil {
		if paramDef.Required {
			return nil, fmt.Errorf("required parameter '%s' resolved to null", paramDef.Name)
		}
		return nil, nil
	}

	switch paramDef.Type {
	case ParameterTypeString:
		s, ok := val.(string)
		if !ok {
			return nil, fmt.Errorf("parameter '%s' must be a string, got %T", paramDef.Name, val)
		}
		return s, nil
	case ParameterTypeInt:
		if fVal, okFloat := val.(float64); okFloat { // JSON numbers are float64
			if fVal != float64(int(fVal)) {
				return nil, fmt.Errorf("parameter '%s' must be a whole number, got %f", paramDef.Name, fVal)
			}
			return int(fVal), nil
		}
		if iVal, okInt := val.(int); okInt {
			return iVal, nil
		}
		if sVal, okStr := val.(string); okStr { // Allow string input for int param
			parsedInt, err := strconv.Atoi(sVal)
			if err != nil {
				return nil, fmt.Errorf("parameter '%s' (string to int conversion) must be an integer, got '%s': %w", paramDef.Name, sVal, err)
			}
			return parsedInt, nil
		}
		return nil, fmt.Errorf("parameter '%s' must be an integer, got %T", paramDef.Name, val)
	case ParameterTypeBool:
		b, ok := val.(bool)
		if !ok {
			return nil, fmt.Errorf("parameter '%s' must be a boolean, got %T", paramDef.Name, val)
		}
		return b, nil
	default:
		return nil, fmt.Errorf("unsupported parameter type '%s' for parameter '%s'", paramDef.Type, paramDef.Name)
	}
}

// --- Template Registry ---

var registeredPodTemplates = make(map[string]PodTemplateDefinition)

func init() {
	// Register default templates from DefaultPodTemplates (defined in default_pod_templates.go)
	for _, tmplDef := range DefaultPodTemplates {
		if err := RegisterPodTemplate(tmplDef); err != nil {
			// This would typically be a panic in init if a core template fails to register
			panic(fmt.Sprintf("Failed to register default pod template '%s': %v", tmplDef.IDValue, err))
		}
	}
}

// RegisterPodTemplate adds a template to the global registry.
func RegisterPodTemplate(template PodTemplateDefinition) error {
	// The 'template' parameter is now a struct, not an interface or pointer.
	// No need to check for nil, as it cannot be a nil struct.
	id := template.IDValue
	if id == "" {
		return fmt.Errorf("pod template ID cannot be empty")
	}
	if _, exists := registeredPodTemplates[id]; exists {
		return fmt.Errorf("pod template with ID '%s' already registered", id)
	}
	registeredPodTemplates[id] = template
	return nil
}

// GetPodTemplateByID retrieves a registered pod template by its ID.
// It returns the struct itself, not a pointer or interface.
func GetPodTemplateByID(id string) (PodTemplateDefinition, bool) {
	tmpl, exists := registeredPodTemplates[id]
	return tmpl, exists
}
