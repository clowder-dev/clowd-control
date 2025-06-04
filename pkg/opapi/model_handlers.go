package opapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/aifoundry-org/clowd-control/pkg/modelmanager"
	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
)

// ModelManagerInterface defines the operations required by ModelHandlers.
// This allows for easier testing by mocking the ModelManager.
type ModelManagerInterface interface {
	AddModel(model modelmanager.ModelMetadata) error
	GetModelByID(id string) (modelmanager.ModelMetadata, error)
	ListModels() ([]modelmanager.ModelMetadata, error)
	RemoveModel(id string) error
}

// ModelHandlers provides HTTP handlers for model operations.
type ModelHandlers struct {
	mm     ModelManagerInterface
	logger *logrus.Entry
}

// NewModelHandlers creates a new ModelHandlers instance.
func NewModelHandlers(mm ModelManagerInterface, parentLogger *logrus.Entry) *ModelHandlers {
	return &ModelHandlers{
		mm:     mm,
		logger: parentLogger.WithField("sub_component", "model_handlers"),
	}
}

// RegisterRoutes registers the model management API endpoints on the given router.
// All routes will be prefixed by the router's existing path prefix.
// For example, if the router is for "/api/v1", these routes will become:
// GET /api/v1/models
// POST /api/v1/models
// GET /api/v1/models/{id}
// DELETE /api/v1/models/{id}
func (mh *ModelHandlers) RegisterRoutes(router *mux.Router) {
	modelsRouter := router.PathPrefix("/models").Subrouter()

	modelsRouter.HandleFunc("", mh.ListModels).Methods(http.MethodGet)
	modelsRouter.HandleFunc("", mh.AddModel).Methods(http.MethodPost)
	modelsRouter.HandleFunc("/{id}", mh.GetModel).Methods(http.MethodGet)
	modelsRouter.HandleFunc("/{id}", mh.RemoveModel).Methods(http.MethodDelete)
	mh.logger.Info("Registered model management endpoints under /models prefix")
}

// ErrorResponse is a generic structure for JSON error responses.
type ErrorResponse struct {
	Error   string `json:"error"`
	Details string `json:"details,omitempty"`
}

func respondWithJSON(w http.ResponseWriter, code int, payload any, logger *logrus.Entry) {
	response, err := json.Marshal(payload)
	if err != nil {
		logger.WithError(err).Error("Failed to marshal JSON response")
		// Fallback to a generic error response if marshalling fails
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		// Attempt to write a simple error message, ignoring further errors here
		_, _ = w.Write([]byte(`{"error":"failed to marshal response"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, err = w.Write(response)
	if err != nil {
		// Log error, but response header might have already been sent.
		logger.WithError(err).Error("Failed to write JSON response")
	}
}

func respondWithError(w http.ResponseWriter, code int, message string, details string, logger *logrus.Entry) {
	logger.WithFields(logrus.Fields{
		"status_code": code,
		"details":     details,
	}).Error(message)
	respondWithJSON(w, code, ErrorResponse{Error: message, Details: details}, logger)
}

// ListModels handles GET requests to list all models.
func (mh *ModelHandlers) ListModels(w http.ResponseWriter, r *http.Request) {
	models, err := mh.mm.ListModels()
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to list models", err.Error(), mh.logger)
		return
	}
	if models == nil { // Ensure we return an empty array instead of null for an empty list
		models = []modelmanager.ModelMetadata{}
	}
	respondWithJSON(w, http.StatusOK, models, mh.logger)
	mh.logger.Debug("Successfully listed models")
}

// AddModel handles POST requests to add a new model.
func (mh *ModelHandlers) AddModel(w http.ResponseWriter, r *http.Request) {
	var model modelmanager.ModelMetadata
	if err := json.NewDecoder(r.Body).Decode(&model); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request payload", err.Error(), mh.logger)
		return
	}
	defer r.Body.Close()

	// Basic validation: ID should not be empty
	if model.ID == "" {
		respondWithError(w, http.StatusBadRequest, "Model ID cannot be empty", "", mh.logger)
		return
	}

	err := mh.mm.AddModel(model)
	if err != nil {
		if errors.Is(err, modelmanager.ErrModelExists) {
			respondWithError(w, http.StatusConflict, "Model already exists", err.Error(), mh.logger)
		} else {
			respondWithError(w, http.StatusInternalServerError, "Failed to add model", err.Error(), mh.logger)
		}
		return
	}
	respondWithJSON(w, http.StatusCreated, model, mh.logger)
	mh.logger.Infof("Successfully added model with ID: %s", model.ID)
}

// GetModel handles GET requests to retrieve a model by its ID.
func (mh *ModelHandlers) GetModel(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, ok := vars["id"]
	if !ok || id == "" {
		respondWithError(w, http.StatusBadRequest, "Model ID not provided in path", "", mh.logger)
		return
	}

	model, err := mh.mm.GetModelByID(id)
	if err != nil {
		if errors.Is(err, modelmanager.ErrModelNotFound) {
			respondWithError(w, http.StatusNotFound, "Model not found", err.Error(), mh.logger)
		} else {
			respondWithError(w, http.StatusInternalServerError, "Failed to get model", err.Error(), mh.logger)
		}
		return
	}
	respondWithJSON(w, http.StatusOK, model, mh.logger)
	mh.logger.Debugf("Successfully retrieved model with ID: %s", id)
}

// RemoveModel handles DELETE requests to remove a model by its ID.
func (mh *ModelHandlers) RemoveModel(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, ok := vars["id"]
	if !ok || id == "" {
		respondWithError(w, http.StatusBadRequest, "Model ID not provided in path", "", mh.logger)
		return
	}

	err := mh.mm.RemoveModel(id)
	if err != nil {
		if errors.Is(err, modelmanager.ErrModelNotFound) {
			respondWithError(w, http.StatusNotFound, "Model not found", err.Error(), mh.logger)
		} else {
			respondWithError(w, http.StatusInternalServerError, "Failed to remove model", err.Error(), mh.logger)
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
	mh.logger.Infof("Successfully removed model with ID: %s", id)
}
