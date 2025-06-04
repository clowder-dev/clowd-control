package modelmanager

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"strings"
	"sync"
)

// ErrModelNotFound is returned when a model with the given ID is not found.
var ErrModelNotFound = errors.New("model not found")

// ErrModelExists is returned when a model with the given ID already exists.
var ErrModelExists = errors.New("model already exists")

// ErrModelInvalid is returned when model metadata is invalid (e.g., missing ID).
var ErrModelInvalid = errors.New("model metadata is invalid")

// ModelManager manages the collection of model metadata.
// It is responsible for storing, retrieving, and managing models.
// All operations are thread-safe.
type ModelManager struct {
	mu      sync.RWMutex
	models  map[string]ModelMetadata // models stores ModelMetadata keyed by model ID
	hfToken string                   // Hugging Face API token passed from config
}

// NewModelManager creates and returns a new ModelManager instance.
// It accepts an hfToken which can be sourced from configuration.
func NewModelManager(hfTokenFromConfig string) *ModelManager {
	return &ModelManager{
		models:  make(map[string]ModelMetadata),
		hfToken: hfTokenFromConfig,
	}
}

// AddModel adds a new model to the manager.
// It returns ErrModelExists if a model with the same ID already exists.
func (mm *ModelManager) AddModel(model ModelMetadata) error {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	// Augment and validate the model metadata before adding.
	// We pass a pointer to allow modification of the model.
	if err := mm.augmentAndValidateModel(&model); err != nil {
		return err
	}

	if _, exists := mm.models[model.ID]; exists {
		return fmt.Errorf("%w: id %s", ErrModelExists, model.ID)
	}

	mm.models[model.ID] = model
	return nil
}

// augmentAndValidateModel performs validation and augmentation of model metadata.
// Currently, it ensures ID is present. If the SourceURI is an "hf://" URI,
// it attempts to fetch metadata from Hugging Face to augment the model.
// Otherwise, it infers Name from ID if Name is empty.
func (mm *ModelManager) augmentAndValidateModel(model *ModelMetadata) error {
	userInputModel := *model // Keep a copy of the original user input for merging

	if strings.HasPrefix(userInputModel.SourceURI, hfScheme+":///") {
		tokenToUse := mm.hfToken
		if tokenToUse == "" {
			tokenToUse = os.Getenv("HF_TOKEN")
		}

		if tokenToUse == "" {
			return fmt.Errorf("%w: Hugging Face token not provided (checked config and HF_TOKEN env var), required for hf:/// URIs", ErrModelInvalid)
		}

		fetchedMetadata, err := FetchMetadataFromHuggingFace(userInputModel.SourceURI, tokenToUse)
		if err != nil {
			return fmt.Errorf("failed to fetch metadata from Hugging Face for URI %s: %w", userInputModel.SourceURI, err)
		}

		// Start with fetched metadata as the base
		finalModel := *fetchedMetadata

		// Override with user-provided values if they are set (non-empty or non-nil)
		if strings.TrimSpace(userInputModel.ID) != "" {
			finalModel.ID = strings.TrimSpace(userInputModel.ID)
		} else if strings.TrimSpace(finalModel.ID) == "" { // If HF didn't provide an ID and user didn't
			return fmt.Errorf("%w: model ID could not be determined from HF URI and was not provided by user", ErrModelInvalid)
		}

		if strings.TrimSpace(userInputModel.Name) != "" {
			finalModel.Name = userInputModel.Name
		}

		if strings.TrimSpace(userInputModel.Version) != "" {
			finalModel.Version = userInputModel.Version
		}

		if strings.TrimSpace(userInputModel.Format) != "" {
			finalModel.Format = userInputModel.Format
		}

		if strings.TrimSpace(userInputModel.Type) != "" {
			finalModel.Type = userInputModel.Type
		}

		if strings.TrimSpace(userInputModel.Licensing) != "" {
			finalModel.Licensing = userInputModel.Licensing
		}

		// Resources: User values override fetched ones if provided
		if userInputModel.Resources.RAM != nil {
			finalModel.Resources.RAM = userInputModel.Resources.RAM
		}
		if userInputModel.Resources.Storage != nil {
			finalModel.Resources.Storage = userInputModel.Resources.Storage
		}

		// CustomProperties: Merge, user's values take precedence
		mergedCustomProps := make(map[string]string)
		// Start with fetched custom properties
		if fetchedMetadata.CustomProperties != nil {
			maps.Copy(mergedCustomProps, fetchedMetadata.CustomProperties)
		}
		// User's custom properties override or add to the fetched ones
		if userInputModel.CustomProperties != nil {
			maps.Copy(mergedCustomProps, userInputModel.CustomProperties)
		}
		if len(mergedCustomProps) > 0 {
			finalModel.CustomProperties = mergedCustomProps
		} else {
			finalModel.CustomProperties = nil // Ensure it's nil if empty, not an empty map
		}

		// Preserve GGUFMeta from fetched data, user cannot override this directly
		finalModel.GGUFMeta = fetchedMetadata.GGUFMeta

		*model = finalModel // Update the original model pointer with the merged data

	} else {
		// Non-HF URI logic
		// For non-HF URIs, GGUFPrefix and GGUFMeta will remain nil/empty as they are not fetched.
		if strings.TrimSpace(userInputModel.ID) == "" {
			return fmt.Errorf("%w: model ID cannot be empty for non-hf URI", ErrModelInvalid)
		}
		model.ID = strings.TrimSpace(userInputModel.ID) // Ensure it's the trimmed version from user input

		// Augment: If Name is empty from user input, infer it from ID.
		if strings.TrimSpace(userInputModel.Name) == "" {
			model.Name = model.ID
		} else {
			model.Name = userInputModel.Name
		}
		// For non-HF, other fields are taken as is from userInputModel
	}

	// Final validation: ID must not be empty at this point.
	if strings.TrimSpace(model.ID) == "" {
		return fmt.Errorf("%w: model ID cannot be empty after augmentation", ErrModelInvalid)
	}
	// Ensure Name is set if ID is set (if not set by user or HF, defaults to ID)
	if strings.TrimSpace(model.Name) == "" {
		model.Name = model.ID
	}
	// Future: Add more common validation rules here (e.g., SourceURI format for non-HF, etc.)
	return nil
}

// GetModelByID retrieves a model by its ID.
// It returns ErrModelNotFound if the model is not found.
func (mm *ModelManager) GetModelByID(id string) (ModelMetadata, error) {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	model, exists := mm.models[id]
	if !exists {
		return ModelMetadata{}, fmt.Errorf("%w: id %s", ErrModelNotFound, id)
	}
	return model, nil
}

// ListModels returns a slice of all models currently managed.
// The returned slice is a copy and can be modified by the caller without affecting the manager.
func (mm *ModelManager) ListModels() ([]ModelMetadata, error) {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	// Placeholder implementation:
	modelList := make([]ModelMetadata, 0, len(mm.models))
	for _, model := range mm.models {
		modelList = append(modelList, model)
	}
	return modelList, nil
}

// RemoveModel removes a model from the manager by its ID.
// It returns ErrModelNotFound if the model with the given ID does not exist.
func (mm *ModelManager) RemoveModel(id string) error {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	if _, exists := mm.models[id]; !exists {
		return fmt.Errorf("%w: id %s", ErrModelNotFound, id)
	}
	// Placeholder implementation:
	delete(mm.models, id)
	return nil
}
