package modelmanager

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

var ( // Made hfAPIBaseURL a var for testing purposes
	hfAPIBaseURL          = "https://huggingface.co/api/models"
	hfFileDownloadBaseURL = "https://huggingface.co" // Base for file downloads
)

const (
	hfScheme       = "hf"
	defaultTimeout = 30 * time.Second
	ggufPrefixSize = 100 * 1024 * 1024 // 100MB
	// bytesInMB is the number of bytes in a megabyte.
	bytesInMB = 1024 * 1024
)

// hfSibling represents a file in a Hugging Face model repository.
type hfSibling struct {
	Rfilename string `json:"rfilename"`
	Size      *int64 `json:"size"` // Pointer to handle null size
	BlobID    string `json:"blobId"`
	// Lfs *struct { Oid string `json:"oid"`; Size int64 `json:"size" } `json:"lfs"` // For LFS files, size might be here
}

// hfModelInfoResponse is a simplified struct to decode the relevant parts of the HF API response.
type hfModelInfoResponse struct {
	ModelID     string      `json:"modelId"`
	Sha         string      `json:"sha"` // Commit SHA
	Siblings    []hfSibling `json:"siblings"`
	PipelineTag string      `json:"pipeline_tag"` // Note: API uses snake_case
	Tags        []string    `json:"tags"`
	// Add other fields if needed, e.g., private, gated, etc.
}

// FetchMetadataFromHuggingFace fetches model metadata from Hugging Face Hub
// based on a URI like "hf:///unsloth/SmolLM2-135M-Instruct-GGUF/SmolLM2-135M-Instruct-Q4_K_M.gguf"
// and an HF API token.
func FetchMetadataFromHuggingFace(uriStr string, hfToken string) (*ModelMetadata, error) {
	if hfToken == "" {
		return nil, fmt.Errorf("Hugging Face API token (hfToken) must be provided")
	}

	parsedURL, err := url.Parse(uriStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URI %s: %w", uriStr, err)
	}

	if parsedURL.Scheme != hfScheme {
		return nil, fmt.Errorf("invalid URI scheme: expected '%s', got '%s'", hfScheme, parsedURL.Scheme)
	}

	// The path part of hf:///org/repo/file.gguf will be /org/repo/file.gguf
	// We need to split this into repoID (org/repo) and filename (file.gguf)
	// Host is empty for this scheme.
	uriPath := strings.TrimPrefix(parsedURL.Path, "/")
	if uriPath == "" {
		return nil, fmt.Errorf("URI path is empty, expected format like /<org_or_user>/<repo_name>/<filename>")
	}

	parts := strings.SplitN(uriPath, "/", 3)
	if len(parts) < 3 { // e.g. "user/repo/file.gguf" -> ["user", "repo", "file.gguf"]
		return nil, fmt.Errorf("invalid URI path format: expected /<org_or_user>/<repo_name>/<filename>, got %s", parsedURL.Path)
	}

	repoID := parts[0] + "/" + parts[1]
	fileName := parts[2]

	if repoID == "" || fileName == "" {
		return nil, fmt.Errorf("invalid URI path: repoID ('%s') or fileName ('%s') is empty", repoID, fileName)
	}

	// Construct API URL
	apiURL := fmt.Sprintf("%s/%s", hfAPIBaseURL, repoID)

	// Create HTTP client and request
	httpClient := &http.Client{Timeout: defaultTimeout}
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request for %s: %w", apiURL, err)
	}
	req.Header.Set("Authorization", "Bearer "+hfToken)
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request to %s: %w", apiURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Attempt to read body for more error details, but don't fail if it's unreadable
		var bodyStr string
		bodyBytes, readErr := io.ReadAll(resp.Body) // Use io.ReadAll directly
		if readErr == nil {
			bodyStr = string(bodyBytes)
		}
		return nil, fmt.Errorf("Hugging Face API request for %s failed with status %s: %s", apiURL, resp.Status, bodyStr)
	}

	var modelInfo hfModelInfoResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelInfo); err != nil {
		return nil, fmt.Errorf("failed to decode JSON response from %s: %w", apiURL, err)
	}

	var targetFile *hfSibling
	for i := range modelInfo.Siblings { // Iterate by index to get a pointer to the element
		if modelInfo.Siblings[i].Rfilename == fileName {
			targetFile = &modelInfo.Siblings[i]
			break
		}
	}

	if targetFile == nil {
		return nil, fmt.Errorf("file '%s' not found in Hugging Face repo '%s'", fileName, repoID)
	}

	// Infer format from filename extension
	fileExt := strings.TrimPrefix(path.Ext(fileName), ".")
	if fileExt == "" {
		fileExt = "unknown" // Default if no extension
	}

	// Calculate storage in MB
	storageMB := 0
	if targetFile.Size != nil {
		storageMB = int(*targetFile.Size / bytesInMB)
		if *targetFile.Size > 0 && storageMB == 0 { // Ensure small files are at least 1MB if not 0
			storageMB = 1
		}
	}
	storageMBPtr := &storageMB

	// Construct ModelMetadata
	// The ID for our system should be unique. Combining repoID and filename is a good start.
	// Or, the user might want to specify this ID separately when adding.
	// For now, let's use a combination.
	internalModelID := fmt.Sprintf("%s-%s", strings.ReplaceAll(repoID, "/", "-"), strings.ReplaceAll(fileName, ".", "-"))

	metadata := &ModelMetadata{
		ID:        internalModelID, // This ID is for ClowdControl, not necessarily the HF repoID directly
		Name:      repoID,          // Use repoID as the default name
		Version:   modelInfo.Sha,   // Use commit SHA as a version indicator
		SourceURI: uriStr,
		Format:    fileExt,
		Type:      "", // Type is hard to infer reliably, could be part of model tags or config.
		Resources: ResourceRequirements{
			Storage: storageMBPtr,
			// RAM is not directly available from file info.
		},
		Licensing: "", // Licensing info might be in model card, not easily parsable here.
		CustomProperties: map[string]string{
			"hf_repo_id":  repoID,
			"hf_filename": fileName,
			"hf_etag":     modelInfo.Sha, // Or targetFile.BlobID if more specific ETag is needed
			// Add more HF specific info if needed
		},
	}
	if modelInfo.PipelineTag != "" {
		metadata.Type = modelInfo.PipelineTag
	}
	if len(modelInfo.Tags) > 0 {
		metadata.CustomProperties["hf_tags"] = strings.Join(modelInfo.Tags, ", ")
	}

	// Download GGUF prefix if applicable
	if strings.HasSuffix(strings.ToLower(fileName), ".gguf") {
		// Construct file download URL: https://huggingface.co/{repo_id}/resolve/{commit_sha}/{filename}
		fileDownloadURL := fmt.Sprintf("%s/%s/resolve/%s/%s", hfFileDownloadBaseURL, repoID, modelInfo.Sha, fileName)
		prefixReq, err := http.NewRequest(http.MethodGet, fileDownloadURL, nil)
		if err != nil {
			// Using fmt.Printf for now as per user's "don't bother with it for now" for error handling details.
			// A proper logging mechanism should be used in a production environment.
			fmt.Printf("Warning: Failed to create request for GGUF prefix %s: %v\n", fileDownloadURL, err)
		} else {
			prefixReq.Header.Set("Range", fmt.Sprintf("bytes=0-%d", ggufPrefixSize-1))
			// Re-use the httpClient established earlier for the API call
			prefixResp, err := httpClient.Do(prefixReq)
			if err != nil {
				fmt.Printf("Warning: Failed to download GGUF prefix from %s: %v\n", fileDownloadURL, err)
			} else {
				defer prefixResp.Body.Close()
				// HF returns 200 OK for full file if Range is not satisfiable or ignored,
				// and 206 Partial Content if Range is satisfied.
				if prefixResp.StatusCode == http.StatusOK || prefixResp.StatusCode == http.StatusPartialContent {
					// Limit reading to ggufPrefixSize to avoid consuming too much memory if server sends full file
					limitedReader := io.LimitReader(prefixResp.Body, int64(ggufPrefixSize))
					prefixBytes, readErr := io.ReadAll(limitedReader)
					if readErr != nil {
						fmt.Printf("Warning: Failed to read GGUF prefix from %s: %v\n", fileDownloadURL, readErr)
					} else {
						// Attempt to parse the GGUF prefix
						if len(prefixBytes) > 0 {
							parsedGGUF, parseErr := ParseGGUFMetaData(prefixBytes)
							if parseErr != nil {
								// Log warning but don't fail the entire fetch operation
								fmt.Printf("Warning: Failed to parse GGUF prefix for %s: %v\n", fileDownloadURL, parseErr)
							} else {
								filteredGGUFMeta := make(map[string]any)
								for key, value := range parsedGGUF.Metadata {
									if !strings.HasPrefix(key, "tokenizer.ggml.") {
										filteredGGUFMeta[key] = value
									}
								}
								if len(filteredGGUFMeta) > 0 {
									metadata.GGUFMeta = filteredGGUFMeta
								} else {
									metadata.GGUFMeta = nil // Ensure consistency if all keys are filtered
								}
							}
						}
					}
				} else {
					var bodyStr string
					bodyBytes, readErr := io.ReadAll(prefixResp.Body) // Read body for error details
					if readErr == nil {
						bodyStr = string(bodyBytes)
					}
					fmt.Printf("Warning: GGUF prefix download from %s failed with status %s: %s\n", fileDownloadURL, prefixResp.Status, bodyStr)
				}
			}
		}
	}

	return metadata, nil
}
