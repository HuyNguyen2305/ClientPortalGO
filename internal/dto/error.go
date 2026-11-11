// Package dto holds the JSON request and response shapes.
package dto

// ErrorBody is the detail of a failed request.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ErrorResponse is the body of every non-2xx response.
type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}
