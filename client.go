package storagesdk

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	apiPathPrefix  = "/api/v1"
	defaultTimeout = 10 * time.Second
)

// Config holds configuration for the storage service client
type Config struct {
	BaseURL string        // Storage service base URL (e.g., "http://localhost:3003")
	Timeout time.Duration // Request timeout (default: 10 seconds)
}

// Client is the storage service HTTP client (plain HTTP).
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// APIError represents an error returned by the storage service API
type APIError struct {
	StatusCode int    // HTTP status code
	Message    string // Error message from the API response
	Body       string // Raw response body (for debugging)
}

// Error implements the error interface
func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("storage service returned status %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("storage service returned status %d: %s", e.StatusCode, e.Body)
}

// IsAPIError checks if an error is an APIError and returns it
func IsAPIError(err error) (*APIError, bool) {
	var apiErr *APIError
	if err != nil && errors.As(err, &apiErr) {
		return apiErr, true
	}
	return nil, false
}

func parseErrorResponse(statusCode int, body []byte) *APIError {
	var errorResp struct {
		Message string `json:"message"`
		Success bool   `json:"success"`
		Status  int    `json:"status"`
		Error   string `json:"error"`
	}

	bodyStr := string(body)
	if err := json.Unmarshal(body, &errorResp); err == nil && (errorResp.Message != "" || errorResp.Error != "") {
		errMessage := errorResp.Error
		if errMessage == "" {
			errMessage = errorResp.Message
		}
		return &APIError{
			StatusCode: statusCode,
			Message:    errMessage,
			Body:       bodyStr,
		}
	}
	return &APIError{
		StatusCode: statusCode,
		Message:    bodyStr,
		Body:       bodyStr,
	}
}

func statusIn(code int, codes []int) bool {
	for _, c := range codes {
		if code == c {
			return true
		}
	}
	return false
}

// do performs a JSON request, checks status, and optionally decodes JSON into result.
func (c *Client) do(method, path string, body interface{}, successStatuses []int, result interface{}, wrapErr string) error {
	resp, err := c.doRequest(method, path, body)
	if err != nil {
		return fmt.Errorf("%s: %w", wrapErr, err)
	}
	defer resp.Body.Close()

	if !statusIn(resp.StatusCode, successStatuses) {
		respBody, _ := io.ReadAll(resp.Body)
		return parseErrorResponse(resp.StatusCode, respBody)
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("%s: %w", wrapErr, err)
		}
	}
	return nil
}

// doRequest performs an HTTP request with optional JSON body.
func (c *Client) doRequest(method, path string, body interface{}) (*http.Response, error) {
	fullURL := c.baseURL + path
	var bodyReader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, fullURL, bodyReader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.httpClient.Do(req)
}

// doMultipart performs a multipart/form-data POST and optionally decodes JSON response.
func (c *Client) doMultipart(path string, formFiles map[string][]string, formValues map[string]string, successStatuses []int, result interface{}, wrapErr string) error {
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)

	for field, paths := range formFiles {
		for _, filePath := range paths {
			f, err := os.Open(filePath)
			if err != nil {
				return fmt.Errorf("%s: open file %s: %w", wrapErr, filePath, err)
			}
			_, name := splitPath(filePath)
			part, err := w.CreateFormFile(field, name)
			if err != nil {
				f.Close()
				return fmt.Errorf("%s: create form file: %w", wrapErr, err)
			}
			if _, err := io.Copy(part, f); err != nil {
				f.Close()
				return fmt.Errorf("%s: copy file: %w", wrapErr, err)
			}
			f.Close()
		}
	}

	for k, v := range formValues {
		if err := w.WriteField(k, v); err != nil {
			return fmt.Errorf("%s: write field: %w", wrapErr, err)
		}
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("%s: close multipart: %w", wrapErr, err)
	}

	fullURL := c.baseURL + path
	req, err := http.NewRequest(http.MethodPost, fullURL, body)
	if err != nil {
		return fmt.Errorf("%s: %w", wrapErr, err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", wrapErr, err)
	}
	defer resp.Body.Close()

	if !statusIn(resp.StatusCode, successStatuses) {
		respBody, _ := io.ReadAll(resp.Body)
		return parseErrorResponse(resp.StatusCode, respBody)
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("%s: %w", wrapErr, err)
		}
	}
	return nil
}

func splitPath(p string) (dir, file string) {
	i := len(p) - 1
	for i >= 0 && p[i] != '/' && p[i] != '\\' {
		i--
	}
	return p[:i+1], p[i+1:]
}

func pathSeg(s string) string { return url.PathEscape(s) }

// NewClient creates a new storage service client (plain HTTP).
func NewClient(config Config) (*Client, error) {
	if config.BaseURL == "" {
		return nil, fmt.Errorf("base URL is required")
	}

	baseURL := strings.TrimRight(config.BaseURL, "/")
	timeout := config.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}

	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: timeout},
	}, nil
}

// FileItem represents a file in API responses (list, get, upload)
type FileItem struct {
	ID           string                 `json:"id"`
	OriginalName string                 `json:"originalName"`
	StoredName   string                 `json:"storedName"`
	FilePath     string                 `json:"filePath"`
	FileSize     int64                  `json:"fileSize"`
	MimeType     string                 `json:"mimeType"`
	Extension    string                 `json:"extension"`
	FileType     string                 `json:"fileType"`
	Hash         string                 `json:"hash"`
	Status       string                 `json:"status"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt    string                 `json:"createdAt"`
	UpdatedAt    string                 `json:"updatedAt"`
}

// Pagination contains pagination metadata
type Pagination struct {
	Page         int   `json:"page"`
	PerPage      int   `json:"perPage"`
	Total        int64 `json:"total"`
	TotalPages   int   `json:"totalPages"`
	HasNext      bool  `json:"hasNext"`
	HasPrevious  bool  `json:"hasPrevious"`
	NextPage     *int  `json:"nextPage,omitempty"`
	PreviousPage *int  `json:"previousPage,omitempty"`
}

// UploadFileResponse represents the response from uploading files
type UploadFileResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Status  int    `json:"status"`
	Data    struct {
		UploadedFiles []FileItem               `json:"uploadedFiles"`
		TotalFiles    int                      `json:"totalFiles"`
		Successful    int                      `json:"successful"`
		Failed        int                      `json:"failed"`
		FailedUploads []map[string]interface{} `json:"failedUploads,omitempty"`
	} `json:"data"`
}

// UploadFile uploads one or more files. filePaths are local paths; metadataJSON is optional JSON object string applied to all files.
func (c *Client) UploadFile(filePaths []string, metadataJSON string) (*UploadFileResponse, error) {
	if len(filePaths) == 0 {
		return nil, fmt.Errorf("at least one file path is required")
	}
	formFiles := map[string][]string{"files": filePaths}
	formValues := make(map[string]string)
	if metadataJSON != "" {
		formValues["metadata"] = metadataJSON
	}
	var result UploadFileResponse
	err := c.doMultipart(apiPathPrefix+"/files/", formFiles, formValues, []int{http.StatusCreated, http.StatusPartialContent}, &result, "failed to upload files")
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// ValidateFileResponse represents the response from validating files
type ValidateFileResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Status  int    `json:"status"`
	Data    struct {
		ValidationResults []ValidationResultItem `json:"validationResults"`
		TotalFiles        int                    `json:"totalFiles"`
	} `json:"data"`
}

// ValidationResultItem represents one file validation result
type ValidationResultItem struct {
	OriginalName     string `json:"originalName"`
	Extension        string `json:"extension"`
	Size             int64  `json:"size"`
	SizeFormatted    string `json:"sizeFormatted"`
	HeaderMimeType   string `json:"headerMimeType"`
	DetectedMimeType string `json:"detectedMimeType"`
	IsAllowed        bool   `json:"isAllowed"`
	Category         string `json:"category"`
	Description      string `json:"description"`
	MaxSize          int64  `json:"maxSize"`
	MaxSizeFormatted string `json:"maxSizeFormatted"`
}

// ValidateFile validates files without uploading. filePaths are local paths.
func (c *Client) ValidateFile(filePaths []string) (*ValidateFileResponse, error) {
	if len(filePaths) == 0 {
		return nil, fmt.Errorf("at least one file path is required")
	}
	formFiles := map[string][]string{"files": filePaths}
	var result ValidateFileResponse
	err := c.doMultipart(apiPathPrefix+"/files/validate", formFiles, nil, []int{http.StatusOK}, &result, "failed to validate files")
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// ListFilesResponse represents the paginated response from listing/searching files
type ListFilesResponse struct {
	Success    bool        `json:"success"`
	Message    string      `json:"message"`
	Status     int         `json:"status"`
	Data       []FileItem  `json:"data"`
	Pagination *Pagination `json:"pagination,omitempty"`
}

// ListFiles lists files with optional query string (page, per_page, filters, e.g. status_eq=active&file_type_eq=jpg).
func (c *Client) ListFiles(queryString string) (*ListFilesResponse, error) {
	path := apiPathPrefix + "/files"
	if queryString != "" {
		path += "?" + queryString
	}
	var result ListFilesResponse
	err := c.do(http.MethodGet, path, nil, []int{http.StatusOK}, &result, "failed to list files")
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// GetFileResponse represents the response from getting a file (metadata)
type GetFileResponse struct {
	Success bool     `json:"success"`
	Message string   `json:"message"`
	Status  int      `json:"status"`
	Data    FileItem `json:"data"`
}

// GetFile retrieves file metadata by ID.
func (c *Client) GetFile(fileID string) (*GetFileResponse, error) {
	if fileID == "" {
		return nil, fmt.Errorf("file ID is required")
	}
	path := apiPathPrefix + "/files/" + pathSeg(fileID)
	var result GetFileResponse
	err := c.do(http.MethodGet, path, nil, []int{http.StatusOK}, &result, "failed to get file")
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// DownloadFile performs GET /files/:id?download=true and returns the HTTP response. Caller must close resp.Body.
// Use resp.Header.Get("Content-Disposition") for suggested filename if needed.
func (c *Client) DownloadFile(fileID string) (*http.Response, error) {
	if fileID == "" {
		return nil, fmt.Errorf("file ID is required")
	}
	path := apiPathPrefix + "/files/" + pathSeg(fileID) + "?download=true"
	resp, err := c.doRequest(http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to download file: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, parseErrorResponse(resp.StatusCode, body)
	}
	return resp, nil
}

// ServeFileContent performs GET /files/:id/content and returns the HTTP response for inline display (e.g. images).
// Response has correct Content-Type, Content-Disposition: inline, and caching headers (ETag, Cache-Control).
// Caller must close resp.Body. Use for <img src> or inline display; use DownloadFile for attachment.
// Returns 200 with body or 304 Not Modified when If-None-Match matches.
func (c *Client) ServeFileContent(fileID string) (*http.Response, error) {
	if fileID == "" {
		return nil, fmt.Errorf("file ID is required")
	}
	path := apiPathPrefix + "/files/" + pathSeg(fileID) + "/content"
	resp, err := c.doRequest(http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to serve file content: %w", err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotModified {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, parseErrorResponse(resp.StatusCode, body)
	}
	return resp, nil
}

// GetFileLimitsResponse represents the response from getting file limits
type GetFileLimitsResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Status  int    `json:"status"`
	Data    struct {
		DefaultMaxSize int64                  `json:"defaultMaxSize"`
		Extensions     map[string]int64       `json:"extensions"`
		UploadLimits   map[string]interface{} `json:"uploadLimits"`
	} `json:"data"`
}

// GetFileLimits returns file size limits and upload limits.
func (c *Client) GetFileLimits() (*GetFileLimitsResponse, error) {
	var result GetFileLimitsResponse
	err := c.do(http.MethodGet, apiPathPrefix+"/files/limits", nil, []int{http.StatusOK}, &result, "failed to get file limits")
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// UpdateFileRequest represents the request body for updating a file
type UpdateFileRequest struct {
	FileName *string                 `json:"fileName,omitempty"`
	Status   *string                 `json:"status,omitempty"` // active, inactive, archived, deleted
	Metadata *map[string]interface{} `json:"metadata,omitempty"`
}

// UpdateFile updates file metadata by ID.
func (c *Client) UpdateFile(fileID string, req UpdateFileRequest) (*GetFileResponse, error) {
	if fileID == "" {
		return nil, fmt.Errorf("file ID is required")
	}
	path := apiPathPrefix + "/files/" + pathSeg(fileID)
	var result GetFileResponse
	err := c.do(http.MethodPut, path, req, []int{http.StatusOK}, &result, "failed to update file")
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteFile deletes a file and its record by ID.
func (c *Client) DeleteFile(fileID string) error {
	if fileID == "" {
		return fmt.Errorf("file ID is required")
	}
	path := apiPathPrefix + "/files/" + pathSeg(fileID)
	return c.do(http.MethodDelete, path, nil, []int{http.StatusOK, http.StatusNoContent}, nil, "failed to delete file")
}
