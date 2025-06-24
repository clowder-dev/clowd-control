package modelmanager

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	// "github.com/huggingface/hub-go/client" // No longer needed
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockHFAPIServer creates a mock HTTP server that mimics parts of the Hugging Face API and file downloads.
// It allows testing without actual network calls or a real token.
func MockHFAPIServer(t *testing.T, repoID, fileName string, fileSize int64, modelTags []string, pipelineTag string, commitSHA string, ggufPrefixData []byte) *httptest.Server {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// API model metadata requests still need auth
		if strings.HasPrefix(r.URL.Path, "/api/models/") {
			authHeader := r.Header.Get("Authorization")
			if !strings.HasPrefix(authHeader, "Bearer hf_") {
				t.Logf("Mock server (API): Missing or invalid Bearer token in Authorization header: %s", authHeader)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}

		// Handle /api/models/{repoID}
		// Example path: /api/models/unsloth/SmolLM2-135M-Instruct-GGUF
		if strings.HasPrefix(r.URL.Path, "/api/models/") {
			parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/models/"), "/")
			reqRepoID := strings.Join(parts, "/") // This might need adjustment if repoID has more slashes

			if reqRepoID != repoID {
				t.Logf("Mock server: RepoID mismatch. Expected '%s', got '%s'", repoID, reqRepoID)
				http.Error(w, fmt.Sprintf("Model %s not found", reqRepoID), http.StatusNotFound)
				return
			}

			// Construct a hfModelInfoResponse (defined in hf_importer.go)
			size := fileSize // Local var for pointer
			// Use the hfSibling and hfModelInfoResponse structs from hf_importer.go
			mockResponse := hfModelInfoResponse{ // Using the struct from the main package
				ModelID:     repoID,
				Sha:         commitSHA,
				PipelineTag: pipelineTag, // Ensure field name matches JSON ("pipeline_tag")
				Tags:        modelTags,
				Siblings: []hfSibling{ // Slice of values
					{Rfilename: fileName, Size: &size, BlobID: "someblobid"},
					{Rfilename: "README.md"}, // Other files
				},
			}
			w.Header().Set("Content-Type", "application/json")
			err := json.NewEncoder(w).Encode(mockResponse)
			if err != nil {
				t.Fatalf("Mock server: Failed to encode ModelInfo: %v", err)
			}
			return
		}
		// Handle /api/whoami - for token validation if used
		if r.URL.Path == "/api/whoami" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name": "test-user", "type": "user"}`))
			return
		}

		// Handle file downloads: /{repoID}/resolve/{commitSHA}/{fileName}
		// Example path: /unsloth/SmolLM2-135M-Instruct-GGUF/resolve/abcdef1234567890/SmolLM2-135M-Instruct-Q4_K_M.gguf
		pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
		// Expected: [repoOrg, repoName, "resolve", commit, file]
		if len(pathParts) >= 4 && pathParts[len(pathParts)-3] == "resolve" {
			reqRepoID := pathParts[0] + "/" + pathParts[1]
			reqCommitSHA := pathParts[len(pathParts)-2]
			reqFileName := pathParts[len(pathParts)-1]

			if reqRepoID == repoID && reqCommitSHA == commitSHA && reqFileName == fileName {
				if strings.HasSuffix(strings.ToLower(fileName), ".gguf") && ggufPrefixData != nil {
					rangeHeader := r.Header.Get("Range")
					if strings.HasPrefix(rangeHeader, fmt.Sprintf("bytes=0-%d", ggufPrefixSize-1)) {
						dataToSend := ggufPrefixData
						if len(dataToSend) > ggufPrefixSize { // Should not happen if mock data is prepared well
							dataToSend = dataToSend[:ggufPrefixSize]
						}

						// If actual prefix data is shorter than requested prefix size (e.g. small file)
						// Content-Range should reflect actual bytes sent.
						endByte := len(dataToSend) - 1
						if endByte < 0 { // Empty prefix data
							endByte = 0
						}
						
						// fileSize here is the total size of the file from the main mock setup
						w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", endByte, fileSize))
						w.Header().Set("Content-Length", fmt.Sprintf("%d", len(dataToSend)))
						w.WriteHeader(http.StatusPartialContent)
						_, err := w.Write(dataToSend)
						if err != nil {
							t.Fatalf("Mock server (File): Failed to write GGUF prefix: %v", err)
						}
						return
					}
				}
				// Fallback for non-GGUF, no prefix data, or unexpected range for GGUF
				http.Error(w, "File not found, not GGUF, no prefix data, or invalid range for prefix", http.StatusNotFound)
				return
			}
		}

		t.Logf("Mock server: Unhandled path: %s", r.URL.Path)
		http.NotFound(w, r)
	})
	return httptest.NewServer(handler)
}

func TestFetchMetadataFromHuggingFace(t *testing.T) {
	const testRepoID = "unsloth/SmolLM2-135M-Instruct-GGUF"
	const testFileNameGGUF = "SmolLM2-135M-Instruct-Q4_K_M.gguf"
	const testFileNameNonGGUF = "model.safetensors"
	const testFileSize = 85 * 1024 * 1024 // 85 MB
	const testCommitSHA = "abcdef1234567890"
	testModelTags := []string{"text-generation", "gguf", "unsloth"}
	testPipelineTag := "text-generation"

	// Prepare mock GGUF prefix data (valid GGUF header + one KV pair)
	var validGGUFPrefixBuffer bytes.Buffer
	binary.Write(&validGGUFPrefixBuffer, binary.LittleEndian, ggufMagicLittleEndian) // Magic
	binary.Write(&validGGUFPrefixBuffer, binary.LittleEndian, ggufVersionV3)         // Version
	binary.Write(&validGGUFPrefixBuffer, binary.LittleEndian, uint64(0))             // TensorCount (can be 0 for metadata-only tests)
	binary.Write(&validGGUFPrefixBuffer, binary.LittleEndian, uint64(2))             // MetadataKVCount (one to keep, one to filter)

	// KV Pair 1 (to keep): "test.key" = "test_value" (string)
	validGGUFPrefixBuffer.Write(ggufTestString("test.key"))
	binary.Write(&validGGUFPrefixBuffer, binary.LittleEndian, GGUFMetadataValueTypeString)
	validGGUFPrefixBuffer.Write(ggufTestString("test_value"))

	// KV Pair 2 (to filter): "tokenizer.ggml.eos_token_id" = uint32(123)
	validGGUFPrefixBuffer.Write(ggufTestString("tokenizer.ggml.eos_token_id"))
	binary.Write(&validGGUFPrefixBuffer, binary.LittleEndian, GGUFMetadataValueTypeUint32)
	var tempUint32Bytes bytes.Buffer
	binary.Write(&tempUint32Bytes, binary.LittleEndian, uint32(123))
	validGGUFPrefixBuffer.Write(tempUint32Bytes.Bytes())
	
	mockGGUFPrefixBytes := validGGUFPrefixBuffer.Bytes()
	// Ensure it's not larger than ggufPrefixSize, though for this test it will be much smaller.
	if len(mockGGUFPrefixBytes) > ggufPrefixSize {
		mockGGUFPrefixBytes = mockGGUFPrefixBytes[:ggufPrefixSize]
	}


	// Server for GGUF file with prefix data
	mockServerGGUF := MockHFAPIServer(t, testRepoID, testFileNameGGUF, testFileSize, testModelTags, testPipelineTag, testCommitSHA, mockGGUFPrefixBytes)
	defer mockServerGGUF.Close()

	// Server for Non-GGUF file (no prefix data needed for it)
	mockServerNonGGUF := MockHFAPIServer(t, testRepoID, testFileNameNonGGUF, testFileSize, testModelTags, "text-generation", testCommitSHA, nil)
	defer mockServerNonGGUF.Close()

	// Store original hfAPIBaseURL and hfFileDownloadBaseURL and override them for tests
	// The hfAPIBaseURL is a const in hf_importer.go, so we can't directly change it for tests
	// without making it a variable. For the mock server, FetchMetadataFromHuggingFace will
	// use the const. The mock server URL needs to be structured to match what the
	// real API would look like relative to that const.
	// So, the mockServer.URL will be "http://127.0.0.1:XXXX"
	// and FetchMetadataFromHuggingFace will try to hit "https://huggingface.co/api/models/..."
	// This means we need to adjust how hfAPIBaseURL is used in FetchMetadataFromHuggingFace
	// for testing, or the mock server needs to be more sophisticated (e.g. proxying or DNS override).

	originalHFAPIBaseURL := hfAPIBaseURL
	originalHFFileDownloadBaseURL := hfFileDownloadBaseURL
	defer func() {
		hfAPIBaseURL = originalHFAPIBaseURL
		hfFileDownloadBaseURL = originalHFFileDownloadBaseURL
	}()

	validToken := "hf_mocktoken"

	t.Run("SuccessfulFetch_GGUF_WithPrefix", func(t *testing.T) {
		hfAPIBaseURL = mockServerGGUF.URL + "/api/models"
		hfFileDownloadBaseURL = mockServerGGUF.URL // File downloads are from server root in mock
		validURIGGUF := fmt.Sprintf("hf:///%s/%s", testRepoID, testFileNameGGUF)

		metadata, err := FetchMetadataFromHuggingFace(validURIGGUF, validToken)
		require.NoError(t, err)
		require.NotNil(t, metadata)

		expectedID := "unsloth-SmolLM2-135M-Instruct-GGUF-SmolLM2-135M-Instruct-Q4_K_M-gguf"
		assert.Equal(t, expectedID, metadata.ID)
		assert.Equal(t, testRepoID, metadata.Name)
		assert.Equal(t, validURIGGUF, metadata.SourceURI)
		assert.Equal(t, "gguf", metadata.Format)
		assert.Equal(t, testPipelineTag, metadata.Type)
		assert.Equal(t, testCommitSHA, metadata.Version)

		// Assert GGUFMeta
		require.NotNil(t, metadata.GGUFMeta, "GGUFMeta should be populated for valid GGUF prefix")
		assert.Equal(t, "test_value", metadata.GGUFMeta["test.key"], "Kept GGUF metadata mismatch")
		_, filteredKeyExists := metadata.GGUFMeta["tokenizer.ggml.eos_token_id"]
		assert.False(t, filteredKeyExists, "tokenizer.ggml.eos_token_id should have been filtered out")
		assert.Len(t, metadata.GGUFMeta, 1, "GGUFMeta should only contain one item after filtering")


		require.NotNil(t, metadata.Resources.Storage)
		expectedStorageMB := int(testFileSize / bytesInMB)
		assert.Equal(t, expectedStorageMB, *metadata.Resources.Storage)

		assert.Equal(t, testRepoID, metadata.CustomProperties["hf_repo_id"])
		assert.Equal(t, testFileNameGGUF, metadata.CustomProperties["hf_filename"])
		assert.Equal(t, testCommitSHA, metadata.CustomProperties["hf_etag"])
		assert.Equal(t, strings.Join(testModelTags, ", "), metadata.CustomProperties["hf_tags"])
	})

	t.Run("SuccessfulFetch_NonGGUF_NoPrefix", func(t *testing.T) {
		hfAPIBaseURL = mockServerNonGGUF.URL + "/api/models"
		hfFileDownloadBaseURL = mockServerNonGGUF.URL
		validURINonGGUF := fmt.Sprintf("hf:///%s/%s", testRepoID, testFileNameNonGGUF)

		metadata, err := FetchMetadataFromHuggingFace(validURINonGGUF, validToken)
		require.NoError(t, err)
		require.NotNil(t, metadata)
		assert.Equal(t, "safetensors", metadata.Format) // from filename extension
		assert.Nil(t, metadata.GGUFMeta, "GGUFMeta should be nil for non-GGUF files")
	})

	t.Run("SuccessfulFetch_GGUF_SmallFilePrefix", func(t *testing.T) {
		smallGGUFActualData := []byte("this is a small GGUF file, less than 64KB")
		smallFileSize := int64(len(smallGGUFActualData)) // File is smaller than prefix request size
		// Mock server for small GGUF file, providing its full content as the "prefix"
		mockServerSmallGGUF := MockHFAPIServer(t, testRepoID, testFileNameGGUF, smallFileSize, testModelTags, testPipelineTag, testCommitSHA, smallGGUFActualData)
		defer mockServerSmallGGUF.Close()

		hfAPIBaseURL = mockServerSmallGGUF.URL + "/api/models"
		hfFileDownloadBaseURL = mockServerSmallGGUF.URL
		validURIGGUF := fmt.Sprintf("hf:///%s/%s", testRepoID, testFileNameGGUF)

		metadata, err := FetchMetadataFromHuggingFace(validURIGGUF, validToken)
		require.NoError(t, err)
		require.NotNil(t, metadata)
		// Since smallGGUFActualData is not a valid GGUF header, GGUFMeta should be nil
		assert.Nil(t, metadata.GGUFMeta, "GGUFMeta should be nil if prefix is not a valid GGUF structure")
	})

	t.Run("SuccessfulFetch_GGUF_InvalidPrefixData", func(t *testing.T) {
		invalidGGUFPrefixData := []byte("this is not a valid GGUF file header at all")
		mockServerInvalidGGUF := MockHFAPIServer(t, testRepoID, testFileNameGGUF, testFileSize, testModelTags, testPipelineTag, testCommitSHA, invalidGGUFPrefixData)
		defer mockServerInvalidGGUF.Close()

		hfAPIBaseURL = mockServerInvalidGGUF.URL + "/api/models"
		hfFileDownloadBaseURL = mockServerInvalidGGUF.URL
		validURIGGUF := fmt.Sprintf("hf:///%s/%s", testRepoID, testFileNameGGUF)

		metadata, err := FetchMetadataFromHuggingFace(validURIGGUF, validToken)
		require.NoError(t, err) // Fetch itself should not fail, only GGUF parsing (logged as warning)
		require.NotNil(t, metadata)
		assert.Nil(t, metadata.GGUFMeta, "GGUFMeta should be nil due to parsing error of invalid prefix")
	})


	t.Run("MissingToken", func(t *testing.T) {
		// Need to use a valid URI for a GGUF file to test token logic path,
		// but the call should fail before attempting download.
		// Use mockServerGGUF settings for API base URL.
		hfAPIBaseURL = mockServerGGUF.URL + "/api/models"
		hfFileDownloadBaseURL = mockServerGGUF.URL
		validURIGGUF := fmt.Sprintf("hf:///%s/%s", testRepoID, testFileNameGGUF)
		_, err := FetchMetadataFromHuggingFace(validURIGGUF, "") // Changed validURI to validURIGGUF
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Hugging Face API token (hfToken) must be provided")
	})

	t.Run("InvalidURIScheme", func(t *testing.T) {
		hfAPIBaseURL = mockServerGGUF.URL + "/api/models" // Keep API URL consistent for setup
		hfFileDownloadBaseURL = mockServerGGUF.URL
		_, err := FetchMetadataFromHuggingFace("http:///org/repo/file.gguf", validToken)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid URI scheme")
	})

	t.Run("InvalidURIPathFormat_TooShort", func(t *testing.T) {
		hfAPIBaseURL = mockServerGGUF.URL + "/api/models"
		hfFileDownloadBaseURL = mockServerGGUF.URL
		_, err := FetchMetadataFromHuggingFace("hf:///org/repo_no_file", validToken)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid URI path format")
	})
	t.Run("InvalidURIPathFormat_EmptyRepo", func(t *testing.T) {
		hfAPIBaseURL = mockServerGGUF.URL + "/api/models"
		hfFileDownloadBaseURL = mockServerGGUF.URL
		_, err := FetchMetadataFromHuggingFace("hf:////file.gguf", validToken) // Leads to empty repo part
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid URI path format: expected /<org_or_user>/<repo_name>/<filename>, got //file.gguf")
	})

	t.Run("ModelNotFoundOnHF_APILevel", func(t *testing.T) {
		// Server will return 404 for this repoID for API call
		hfAPIBaseURL = mockServerGGUF.URL + "/api/models" // Use an existing server setup
		hfFileDownloadBaseURL = mockServerGGUF.URL
		uri := "hf:///nonexistent/repo/file.gguf" // This repoID is not handled by mockServerGGUF
		_, err := FetchMetadataFromHuggingFace(uri, validToken)
		assert.Error(t, err)
		// hfAPIBaseURL is mockServerGGUF.URL + "/api/models"
		expectedApiURLForError := fmt.Sprintf("%s/%s", hfAPIBaseURL, "nonexistent/repo")
		assert.Contains(t, err.Error(), fmt.Sprintf("Hugging Face API request for %s failed with status 404 Not Found", expectedApiURLForError))
	})

	t.Run("FileNotFoundInRepo_APILevel", func(t *testing.T) {
		// This tests if the file is not listed in `siblings` by the API
		hfAPIBaseURL = mockServerGGUF.URL + "/api/models" // mockServerGGUF serves testRepoID with testFileNameGGUF
		hfFileDownloadBaseURL = mockServerGGUF.URL
		uriWithMissingFile := fmt.Sprintf("hf:///%s/otherfile.txt", testRepoID) // otherfile.txt is not in siblings
		_, err := FetchMetadataFromHuggingFace(uriWithMissingFile, validToken)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "file 'otherfile.txt' not found")
	})


	t.Run("SmallFileStorageCalculation", func(t *testing.T) {
		smallFileSize := int64(500 * 1024) // 0.5 MB
		// For this test, GGUF prefix is not relevant for the file type "small.bin", so pass nil
		smallFileServer := MockHFAPIServer(t, "org/smallfile-repo", "small.bin", smallFileSize, nil, "", "sha123", nil)
		defer smallFileServer.Close()

		currentTestHFAPIBaseURL := hfAPIBaseURL
		currentTestHFFileDownloadBaseURL := hfFileDownloadBaseURL
		hfAPIBaseURL = smallFileServer.URL + "/api/models"
		hfFileDownloadBaseURL = smallFileServer.URL
		defer func() {
			hfAPIBaseURL = currentTestHFAPIBaseURL
			hfFileDownloadBaseURL = currentTestHFFileDownloadBaseURL
		}()

		metadata, err := FetchMetadataFromHuggingFace("hf:///org/smallfile-repo/small.bin", validToken)
		require.NoError(t, err)

		require.NotNil(t, metadata.Resources.Storage)
		assert.Equal(t, 1, *metadata.Resources.Storage, "Small files should round up to at least 1MB storage if not 0")

		zeroFileSize := int64(0)
		zeroFileServer := MockHFAPIServer(t, "org/zerofile-repo", "zero.bin", zeroFileSize, nil, "", "sha456", nil)
		defer zeroFileServer.Close()

		currentTestHFAPIBaseURLZero := hfAPIBaseURL
		currentTestHFFileDownloadBaseURLZero := hfFileDownloadBaseURL
		hfAPIBaseURL = zeroFileServer.URL + "/api/models"
		hfFileDownloadBaseURL = zeroFileServer.URL
		defer func() {
			hfAPIBaseURL = currentTestHFAPIBaseURLZero
			hfFileDownloadBaseURL = currentTestHFFileDownloadBaseURLZero
		}()


		metadataZero, errZero := FetchMetadataFromHuggingFace("hf:///org/zerofile-repo/zero.bin", validToken)
		require.NoError(t, errZero)
		require.NotNil(t, metadataZero.Resources.Storage)
		assert.Equal(t, 0, *metadataZero.Resources.Storage, "Zero byte files should result in 0MB storage")
	})

	// Test with a real token and model if HF_TOKEN is set
	// This is more of an integration test and can be skipped in CI if tokens are not available.
	hfRealToken := os.Getenv("HF_TOKEN")
	if hfRealToken != "" && os.Getenv("CI") == "" { // Skip in CI or if token not set
		t.Run("RealFetch_OptionalIntegrationTest", func(t *testing.T) {
			t.Skip("Skipping real Hugging Face API test by default. Enable by ensuring HF_TOKEN is set and not in CI.")
			// Restore original base URL for real API call
			currentTestHFAPIBaseURLForReal := hfAPIBaseURL // Could be mock server URL
			hfAPIBaseURL = "https://huggingface.co/api/models" // The actual default
			defer func() { hfAPIBaseURL = currentTestHFAPIBaseURLForReal }() // Point back for other tests

			// Use a known small model file for this test
			// Example: "hf:////ggml-org/models/ggml-tiny.bin" (Note: ggml-org/models is a repo, ggml-tiny.bin is a file)
			// Or find another small, stable GGUF file.
			// For this example, let's assume "hf-internal-testing/tiny-random" exists and has "file.txt"
			// This is a placeholder, replace with a real, small, public model file.
			// realModelURI := "hf:///hf-internal-testing/tiny-random/file.txt"
			// metadata, err := FetchMetadataFromHuggingFace(realModelURI, hfRealToken)

			// For a more stable test, let's use a known public model like a small tokenizer file
			// from a well-known repo.
			// Example: "gpt2/tokenizer.json"
			realModelURI := "hf:///gpt2/tokenizer.json"
			metadata, err := FetchMetadataFromHuggingFace(realModelURI, hfRealToken)

			if err != nil {
				// This can fail due to network, rate limits, or if the model is removed.
				t.Logf("Optional real fetch failed (which can be due to external factors): %v", err)
				t.Skip("Skipping due to real API call failure.")
			}
			require.NoError(t, err)
			require.NotNil(t, metadata)
			assert.NotEmpty(t, metadata.ID)
			assert.Equal(t, "gpt2", metadata.Name) // Or whatever the repo ID is
			assert.Equal(t, realModelURI, metadata.SourceURI)
			assert.Equal(t, "json", metadata.Format) // from tokenizer.json
			assert.NotEmpty(t, metadata.Version)     // Commit SHA
			require.NotNil(t, metadata.Resources.Storage)
			assert.GreaterOrEqual(t, *metadata.Resources.Storage, 0) // Size should be non-negative
			t.Logf("Successfully fetched real metadata for %s: %+v", realModelURI, metadata)
		})
	}
}
