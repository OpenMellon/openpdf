// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package ocr

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/OpenMellon/openpdf/model"
)

// Service returns the underlying OCR service.
func (c *Client) Service() OCRService { return c.service }

// NewClient creates a new OCR client with the given service.
func NewClient(service OCRService) *Client { return &Client{service: service} }

// NewHTTPOCRService creates a new HTTP-based OCR service.
func NewHTTPOCRService(options OCROptions) *HTTPOCRService {
	if options.Method == "" {
		options.Method = "POST"
	}
	if options.FileFieldName == "" {
		options.FileFieldName = "file"
	}
	if options.TimeoutSeconds == 0 {
		options.TimeoutSeconds = 30
	}
	if options.Headers == nil {
		options.Headers = make(map[string]string)
	}
	if options.FormFields == nil {
		options.FormFields = make(map[string]string)
	}
	return &HTTPOCRService{options: options}
}

// OCRService defines the interface for OCR services.
type OCRService interface {
	// ExtractText extracts text from an image reader.
	// The filename parameter is used to set the filename in the multipart form data.
	// If filename is empty, a default name based on content type will be used.
	//
	// Parameters:
	//   - reader: Image data reader (e.g., file, buffer)
	//   - filename: Optional filename for the uploaded file (used in multipart form)
	ExtractText(ctx context.Context, reader io.Reader, filename string) ([]byte, error)

	// CallEndpoint executes an HTTP request using the service's configured URL and options.
	CallEndpoint(ctx context.Context, body io.Reader) ([]byte, error)
}

// ExtractTextFromImage extracts text from a OpenPDF image
func (c *Client) ExtractTextFromImage(ctx context.Context, image *model.Image) ([]byte, error) {
	if image == nil {
		return nil, fmt.Errorf("image cannot be nil")
	}
	if len(image.Data) == 0 {
		return nil, fmt.Errorf("image data is empty")
	}
	reader := bytes.NewReader(image.Data)
	filename := "image.jpg"
	return c.service.ExtractText(ctx, reader, filename)
}

// Client provides a high-level interface for OCR operations.
type Client struct{ service OCRService }

// CallEndpoint executes an HTTP request to the configured URL.
// This provides a general method for users to execute HTTP requests using
// the service's configured options (URL, method, headers, timeout, etc.).
//
// The URL, method, and headers are taken from the OCROptions used to create this service.
// To call different endpoints, create separate service instances with different URLs.
//
// Parameters:
//   - body: Request body (can be nil for GET requests or when no body is needed)
func (s *HTTPOCRService) CallEndpoint(ctx context.Context, body io.Reader) ([]byte, error) {
	if s.options.Url == "" {
		return nil, fmt.Errorf("no URL configured in OCROptions")
	}
	method := s.options.Method
	if method == "" {
		method = "GET"
	}
	req, err := http.NewRequestWithContext(ctx, method, s.options.Url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	for key, value := range s.options.Headers {
		req.Header.Set(key, value)
	}
	if s.options.RequestModifier != nil {
		if err := s.options.RequestModifier(req); err != nil {
			return nil, fmt.Errorf("request modifier failed: %w", err)
		}
	}
	return s.executeRequestWithRetry(ctx, req)
}

// WithService returns a new client with the given service.
func (c *Client) WithService(service OCRService) *Client { return &Client{service: service} }

// HTTPOCRService implements OCRService using HTTP requests.
//
// This service is mainly designed to work with an OCR server
// but can be adapted to other HTTP-based OCR services with similar APIs.
type HTTPOCRService struct{ options OCROptions }

// BatchProcessFiles processes multiple image files concurrently from a list of file paths.
func (c *Client) BatchProcessFiles(ctx context.Context, filePaths []string) ([][]byte, []error) {
	if len(filePaths) == 0 {
		return nil, nil
	}
	texts := make([][]byte, len(filePaths))
	errs := make([]error, len(filePaths))
	sem := make(chan struct{}, 10)
	type result struct {
		index int
		text  []byte
		err   error
	}
	results := make(chan result, len(filePaths))
	for i, path := range filePaths {
		go func(idx int, path string) {
			sem <- struct{}{}
			defer func() { <-sem }()
			text, err := c.ExtractTextFromFile(ctx, path)
			results <- result{index: idx, text: text, err: err}
		}(i, path)
	}
	for n := 0; n < len(filePaths); n++ {
		res := <-results
		texts[res.index] = res.text
		errs[res.index] = res.err
	}
	return texts, errs
}

// ExtractText extracts text from an image reader.
// The filename parameter is used to set the filename in the multipart form data.
// If filename is empty, a default name based on content type will be used.
//
// Parameters:
//   - reader: Image data reader (e.g., file, buffer)
//   - filename: Optional filename for the uploaded file (used in multipart form)
func (s *HTTPOCRService) ExtractText(ctx context.Context, reader io.Reader, filename string) ([]byte, error) {
	if s.options.Url == "" {
		return nil, fmt.Errorf("no OCR service URL provided")
	}
	if reader == nil {
		return nil, fmt.Errorf("reader cannot be nil")
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if filename == "" {
		buf := make([]byte, 512)
		n, err := reader.Read(buf)
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("failed to read from reader: %w", err)
		}
		contentType := http.DetectContentType(buf[:n])
		switch contentType {
		case "image/jpeg":
			filename = "image.jpg"
		case "image/png":
			filename = "image.png"
		case "image/gif":
			filename = "image.gif"
		default:
			filename = "image.jpg"
		}
		reader = io.MultiReader(bytes.NewReader(buf[:n]), reader)
	}
	part, err := writer.CreateFormFile(s.options.FileFieldName, filename)
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}
	_, err = io.Copy(part, reader)
	if err != nil {
		return nil, fmt.Errorf("failed to copy data: %w", err)
	}
	for name, value := range s.options.FormFields {
		err = writer.WriteField(name, value)
		if err != nil {
			return nil, fmt.Errorf("failed to add form field %s: %w", name, err)
		}
	}
	err = writer.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, s.options.Method, s.options.Url, &body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	for key, value := range s.options.Headers {
		req.Header.Set(key, value)
	}
	if s.options.RequestModifier != nil {
		if err := s.options.RequestModifier(req); err != nil {
			return nil, fmt.Errorf("request modifier failed: %w", err)
		}
	}
	return s.executeRequestWithRetry(ctx, req)
}

// OCROptions provides configuration for HTTP-based OCR services.
type OCROptions struct {
	// URL for the OCR service.
	Url string

	// HTTP method to use for the OCR request (default: POST).
	Method string

	// Custom headers to add to the request.
	Headers map[string]string

	// Form field name for the file upload (default: "file").
	FileFieldName string

	// Additional form fields to send with the request.
	FormFields map[string]string

	// HTTP client timeout in seconds (default: 30).
	TimeoutSeconds int

	// Maximum number of retry attempts on failure (default: 0, no retry).
	MaxRetries int

	// Custom HTTP client (optional - if provided, TimeoutSeconds is ignored).
	Client *http.Client

	// Custom request modifier function (called after request is created).
	RequestModifier func(*http.Request) error
}

// NewOCRHTTPClient creates a new OCR client using HTTP service with the given options.
func NewOCRHTTPClient(options OCROptions) *Client {
	return &Client{service: NewHTTPOCRService(options)}
}

// ExtractTextFromFile extracts text from an image file.
func (c *Client) ExtractTextFromFile(ctx context.Context, filePath string) ([]byte, error) {
	if filePath == "" {
		return nil, fmt.Errorf("file path cannot be empty")
	}
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("error opening file %s: %w", filePath, err)
	}
	defer file.Close()
	name := filepath.Base(filePath)
	return c.service.ExtractText(ctx, file, name)
}

// ExtractText extracts text from an image reader
func (c *Client) ExtractText(ctx context.Context, reader io.Reader, filename string) ([]byte, error) {
	if reader == nil {
		return nil, fmt.Errorf("reader cannot be nil")
	}
	return c.service.ExtractText(ctx, reader, filename)
}

func (s *HTTPOCRService) executeRequestWithRetry(ctx context.Context, req *http.Request) ([]byte, error) {
	client := s.options.Client
	if client == nil {
		client = &http.Client{Timeout: time.Duration(s.options.TimeoutSeconds) * time.Second}
	}
	attempts := s.options.MaxRetries + 1
	var (
		lastErr   error
		bodyBytes []byte
		err       error
	)
	if req.Body != nil {
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read request body: %w", err)
		}
		req.Body.Close()
	}
	for attempt := 0; attempt < attempts; attempt++ {
		attemptReq := req.Clone(ctx)
		if bodyBytes != nil {
			attemptReq.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}
		resp, err := client.Do(attemptReq)
		if err != nil {
			lastErr = fmt.Errorf("failed to execute request (attempt %d/%d): %w", attempt+1, attempts, err)
			if attempt < attempts-1 {
				time.Sleep(time.Duration(attempt+1) * time.Second)
				continue
			}
			break
		}
		defer resp.Body.Close()
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			lastErr = fmt.Errorf("failed to read response (attempt %d/%d): %w", attempt+1, attempts, err)
			if attempt < attempts-1 {
				continue
			}
			break
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("request failed with status %s (attempt %d/%d): %s", resp.Status, attempt+1, attempts, string(respBody))
			if attempt < attempts-1 {
				continue
			}
			break
		}
		return respBody, nil
	}
	return nil, lastErr
}

// BatchProcess processes multiple images concurrently.
func (c *Client) BatchProcess(ctx context.Context, images []*model.Image) ([][]byte, []error) {
	if len(images) == 0 {
		return nil, nil
	}
	texts := make([][]byte, len(images))
	errs := make([]error, len(images))
	sem := make(chan struct{}, 10)
	type result struct {
		index int
		text  []byte
		err   error
	}
	results := make(chan result, len(images))
	for i, image := range images {
		go func(idx int, img *model.Image) {
			sem <- struct{}{}
			defer func() { <-sem }()
			text, err := c.ExtractTextFromImage(ctx, img)
			results <- result{index: idx, text: text, err: err}
		}(i, image)
	}
	for n := 0; n < len(images); n++ {
		res := <-results
		texts[res.index] = res.text
		errs[res.index] = res.err
	}
	return texts, errs
}

// CallEndpoint executes an HTTP request using the underlying service's configured options.
// The URL, method, headers, and other settings are taken from the OCROptions
// used to create the service.
//
// To call different endpoints, create separate service instances with different URLs:
//
//	statusService := NewHTTPOCRService(OCROptions{Url: "http://localhost:8080/status", Method: "GET"})
//	ocrService := NewHTTPOCRService(OCROptions{Url: "http://localhost:8080/ocr", Method: "POST"})
//
// Parameters:
//   - body: Request body (can be nil for GET requests or when no body is needed)
//
// Example usage:
//
//	result, err := client.CallEndpoint(ctx, nil)              // GET request
//	result, err := client.CallEndpoint(ctx, requestBody)      // POST request with body
func (c *Client) CallEndpoint(ctx context.Context, body io.Reader) ([]byte, error) {
	return c.service.CallEndpoint(ctx, body)
}
