package opapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aifoundry-org/clowd-control/pkg/modelmanager"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockModelManager is a mock type for the ModelManagerInterface
type MockModelManager struct {
	mock.Mock
}

func (m *MockModelManager) AddModel(model modelmanager.ModelMetadata) error {
	args := m.Called(model)
	return args.Error(0)
}

func (m *MockModelManager) GetModelByID(id string) (modelmanager.ModelMetadata, error) {
	args := m.Called(id)
	return args.Get(0).(modelmanager.ModelMetadata), args.Error(1)
}

func (m *MockModelManager) ListModels() ([]modelmanager.ModelMetadata, error) {
	args := m.Called()
	return args.Get(0).([]modelmanager.ModelMetadata), args.Error(1)
}

func (m *MockModelManager) RemoveModel(id string) error {
	args := m.Called(id)
	return args.Error(0)
}

func setupTestServerWithModelHandlers(mm *MockModelManager) (*httptest.Server, *ModelHandlers) {
	logger := newTestLogger() // This will now refer to the one in server_test.go
	parentEntry := logger.WithField("component", "test-opapi-server")
	mh := NewModelHandlers(mm, parentEntry)

	router := mux.NewRouter()
	apiV1Router := router.PathPrefix("/api/v1").Subrouter()
	mh.RegisterRoutes(apiV1Router) // ModelHandlers now registers its own routes

	return httptest.NewServer(router), mh
}

func TestModelHandlers_ListModels(t *testing.T) {
	mm := new(MockModelManager)
	server, _ := setupTestServerWithModelHandlers(mm)
	defer server.Close()

	expectedModels := []modelmanager.ModelMetadata{
		{ID: "model1", Name: "Test Model 1"},
		{ID: "model2", Name: "Test Model 2"},
	}
	mm.On("ListModels").Return(expectedModels, nil)

	resp, err := http.Get(server.URL + "/api/v1/models")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var actualModels []modelmanager.ModelMetadata
	err = json.NewDecoder(resp.Body).Decode(&actualModels)
	require.NoError(t, err)
	assert.Equal(t, expectedModels, actualModels)
	mm.AssertExpectations(t)

	// Test empty list
	mm = new(MockModelManager) // New mock for fresh call count
	serverEmpty, _ := setupTestServerWithModelHandlers(mm)
	defer serverEmpty.Close()
	mm.On("ListModels").Return([]modelmanager.ModelMetadata{}, nil)
	respEmpty, errEmpty := http.Get(serverEmpty.URL + "/api/v1/models")
	require.NoError(t, errEmpty)
	defer respEmpty.Body.Close()
	assert.Equal(t, http.StatusOK, respEmpty.StatusCode)
	var actualModelsEmpty []modelmanager.ModelMetadata
	err = json.NewDecoder(respEmpty.Body).Decode(&actualModelsEmpty)
	require.NoError(t, err)
	assert.Len(t, actualModelsEmpty, 0) // Should be an empty array `[]` not `null`
	mm.AssertExpectations(t)

	// Test error
	mm = new(MockModelManager)
	serverErr, _ := setupTestServerWithModelHandlers(mm)
	defer serverErr.Close()
	mm.On("ListModels").Return([]modelmanager.ModelMetadata{}, errors.New("internal error"))
	respErr, _ := http.Get(serverErr.URL + "/api/v1/models")
	assert.Equal(t, http.StatusInternalServerError, respErr.StatusCode)
	mm.AssertExpectations(t)
}

func TestModelHandlers_AddModel(t *testing.T) {
	mm := new(MockModelManager)
	server, _ := setupTestServerWithModelHandlers(mm)
	defer server.Close()

	modelToAdd := modelmanager.ModelMetadata{ID: "model1", Name: "New Model"}
	mm.On("AddModel", modelToAdd).Return(nil)

	body, _ := json.Marshal(modelToAdd)
	resp, err := http.Post(server.URL+"/api/v1/models", "application/json", bytes.NewBuffer(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	var createdModel modelmanager.ModelMetadata
	err = json.NewDecoder(resp.Body).Decode(&createdModel)
	require.NoError(t, err)
	assert.Equal(t, modelToAdd, createdModel)
	mm.AssertExpectations(t)

	// Test model already exists
	mmConflict := new(MockModelManager)
	serverConflict, _ := setupTestServerWithModelHandlers(mmConflict)
	defer serverConflict.Close()
	mmConflict.On("AddModel", modelToAdd).Return(modelmanager.ErrModelExists)
	respConflict, _ := http.Post(serverConflict.URL+"/api/v1/models", "application/json", bytes.NewBuffer(body))
	assert.Equal(t, http.StatusConflict, respConflict.StatusCode)
	mmConflict.AssertExpectations(t)

	// Test bad request (empty ID)
	modelNoID := modelmanager.ModelMetadata{Name: "No ID Model"}
	bodyNoID, _ := json.Marshal(modelNoID)
	respNoID, _ := http.Post(server.URL+"/api/v1/models", "application/json", bytes.NewBuffer(bodyNoID))
	assert.Equal(t, http.StatusBadRequest, respNoID.StatusCode)
	// mm should not have been called for AddModel here
	mm.AssertNumberOfCalls(t, "AddModel", 1) // Only the first successful call

	// Test bad JSON
	respBadJSON, _ := http.Post(server.URL+"/api/v1/models", "application/json", bytes.NewBufferString("{bad json"))
	assert.Equal(t, http.StatusBadRequest, respBadJSON.StatusCode)
}

func TestModelHandlers_GetModel(t *testing.T) {
	mm := new(MockModelManager)
	server, _ := setupTestServerWithModelHandlers(mm)
	defer server.Close()

	expectedModel := modelmanager.ModelMetadata{ID: "model1", Name: "Found Model"}
	mm.On("GetModelByID", "model1").Return(expectedModel, nil)

	resp, err := http.Get(server.URL + "/api/v1/models/model1")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var actualModel modelmanager.ModelMetadata
	err = json.NewDecoder(resp.Body).Decode(&actualModel)
	require.NoError(t, err)
	assert.Equal(t, expectedModel, actualModel)
	mm.AssertExpectations(t)

	// Test model not found
	mmNotFound := new(MockModelManager)
	serverNotFound, _ := setupTestServerWithModelHandlers(mmNotFound)
	defer serverNotFound.Close()
	mmNotFound.On("GetModelByID", "unknown").Return(modelmanager.ModelMetadata{}, modelmanager.ErrModelNotFound)
	respNotFound, _ := http.Get(serverNotFound.URL + "/api/v1/models/unknown")
	assert.Equal(t, http.StatusNotFound, respNotFound.StatusCode)
	mmNotFound.AssertExpectations(t)
}

func TestModelHandlers_RemoveModel(t *testing.T) {
	mm := new(MockModelManager)
	server, _ := setupTestServerWithModelHandlers(mm)
	defer server.Close()

	mm.On("RemoveModel", "model1").Return(nil)

	req, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/v1/models/model1", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	mm.AssertExpectations(t)

	// Test model not found
	mmNotFound := new(MockModelManager)
	serverNotFound, _ := setupTestServerWithModelHandlers(mmNotFound)
	defer serverNotFound.Close()
	mmNotFound.On("RemoveModel", "unknown").Return(modelmanager.ErrModelNotFound)
	reqNotFound, _ := http.NewRequest(http.MethodDelete, serverNotFound.URL+"/api/v1/models/unknown", nil)
	respNotFound, _ := http.DefaultClient.Do(reqNotFound)
	assert.Equal(t, http.StatusNotFound, respNotFound.StatusCode)
	mmNotFound.AssertExpectations(t)
}
