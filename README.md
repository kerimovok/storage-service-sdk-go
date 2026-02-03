# Storage Service SDK

A Go SDK for interacting with the storage-service microservice. This SDK
provides a type-safe client for file upload, validation, metadata, search, and
download.

## Installation

```bash
go get github.com/kerimovok/storage-service-sdk-go
```

## Features

- **Type-safe**: Full type definitions for file items,
  upload/validate/list/get/update responses
- **Error handling**: `APIError` and `IsAPIError()` for API-level errors
- **Upload**: Multipart upload with optional JSON metadata applied to all files
- **Validate**: Validate files without uploading (extension, size, MIME)
- **Search & pagination**: List files with query string (filters, page,
  per_page)
- **Download**: Get file metadata or download file bytes

## Quick Start

```go
package main

import (
	"fmt"
	"io"
	"os"
	"time"

	storagesdk "github.com/kerimovok/storage-service-sdk-go"
)

func main() {
	client, err := storagesdk.NewClient(storagesdk.Config{
		BaseURL: "http://localhost:3003",
		Timeout: 10 * time.Second,
	})
	if err != nil {
		panic(err)
	}

	// Upload files (optional metadata as JSON string)
	paths := []string{"./image.jpg", "./doc.pdf"}
	metadata := `{"source":"web","tags":["photo"]}`
	resp, err := client.UploadFile(paths, metadata)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Uploaded %d files\n", resp.Data.Successful)

	// Get file metadata
	file, err := client.GetFile(resp.Data.UploadedFiles[0].ID)
	if err != nil {
		panic(err)
	}
	fmt.Printf("File: %s\n", file.Data.OriginalName)

	// Download file
	downloadResp, err := client.DownloadFile(file.Data.ID)
	if err != nil {
		panic(err)
	}
	defer downloadResp.Body.Close()
	out, _ := os.Create("downloaded.jpg")
	defer out.Close()
	io.Copy(out, downloadResp.Body)
}
```

## API Reference

### Files

- **UploadFile(filePaths, metadataJSON)** – Upload one or more files from local
  paths; optional metadata JSON string applied to all
- **ValidateFile(filePaths)** – Validate files without uploading (returns
  validation results per file)
- **ListFiles(queryString)** – Paginated list/search; pass query string (e.g.
  `page=1&per_page=20`, `status_eq=active`, `file_type_eq=jpg`)
- **GetFile(fileID)** – Get file metadata by ID
- **DownloadFile(fileID)** – Download file; returns `*http.Response` (caller
  must close `Body`)
- **GetFileLimits()** – Get default max size, per-extension limits, and upload
  limits
- **UpdateFile(fileID, req)** – Update file name, status, or metadata (JSONB)
- **DeleteFile(fileID)** – Delete file and its record

### Types

- **FileItem** – ID, OriginalName, StoredName, FilePath, FileSize, MimeType,
  Extension, FileType, Hash, Status, Metadata, CreatedAt, UpdatedAt
- **UpdateFileRequest** – FileName, Status, Metadata (all optional pointers)
- **Pagination** – Page, PerPage, Total, TotalPages, HasNext, HasPrevious,
  NextPage, PreviousPage

## Configuration

- **BaseURL**: Storage service base URL (e.g. `http://localhost:3003`)
- **Timeout**: Request timeout (optional, default 10s)

## Error Handling

```go
resp, err := client.GetFile(id)
if err != nil {
	if apiErr, ok := storagesdk.IsAPIError(err); ok {
		fmt.Printf("API Error (status %d): %s\n", apiErr.StatusCode, apiErr.Message)
	}
	return err
}
```

## License

MIT
