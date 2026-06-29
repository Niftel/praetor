package render

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

// GetIDParam parses the "id" URL parameter into an int64.
// It returns 0 if the parameter is missing or invalid.
func GetIDParam(r *http.Request) int64 {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// PaginatedResponse is the standard envelope for list responses.
type PaginatedResponse struct {
	Items  interface{} `json:"items"`
	Total  int64       `json:"total"`
	Limit  int         `json:"limit"`
	Offset int         `json:"offset"`
}

// Render implements the chi.Render interface.
func (resp *PaginatedResponse) Render(w http.ResponseWriter, r *http.Request) error {
	return nil
}

// Render is a wrapper around chi/render.Render
func Render(w http.ResponseWriter, r *http.Request, v render.Renderer) error {
	return render.Render(w, r, v)
}

// PaginationParams holds limit and offset.
type PaginationParams struct {
	Limit  int
	Offset int
}

// ParsePagination extracts limit and offset from the request.
func ParsePagination(r *http.Request) PaginationParams {
	limit := 50
	offset := 0

	if l := r.URL.Query().Get("limit"); l != "" {
		if val, err := strconv.Atoi(l); err == nil && val > 0 {
			limit = val
		}
	}
	if limit > 500 {
		limit = 500
	}

	if o := r.URL.Query().Get("offset"); o != "" {
		if val, err := strconv.Atoi(o); err == nil && val >= 0 {
			offset = val
		}
	}

	return PaginationParams{Limit: limit, Offset: offset}
}

// JSON responds with 200 OK and the payload.
func JSON(w http.ResponseWriter, r *http.Request, v interface{}) {
	render.JSON(w, r, v)
}

// Created responds with 201 Created and the payload.
func Created(w http.ResponseWriter, r *http.Request, v interface{}) {
	render.Status(r, http.StatusCreated)
	render.JSON(w, r, v)
}

// NoContent responds with 204 No Content.
func NoContent(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// ErrorResponse represents a standard error.
type ErrorResponse struct {
	Err            error `json:"-"` // low-level runtime error
	HTTPStatusCode int   `json:"-"` // http response status code

	ErrorText string `json:"error"` // user-facing error message
}

func (e *ErrorResponse) Render(w http.ResponseWriter, r *http.Request) error {
	render.Status(r, e.HTTPStatusCode)
	render.JSON(w, r, e)
	return nil
}

func ErrInvalidRequest(err error) render.Renderer {
	msg := "Invalid request"
	if err != nil {
		msg = err.Error()
	}
	return &ErrorResponse{
		Err:            err,
		HTTPStatusCode: http.StatusBadRequest,
		ErrorText:      msg,
	}
}

func ErrNotFound(err error) render.Renderer {
	return &ErrorResponse{
		Err:            err,
		HTTPStatusCode: http.StatusNotFound,
		ErrorText:      "Resource not found",
	}
}

func ErrInternal(err error) render.Renderer {
	return &ErrorResponse{
		Err:            err,
		HTTPStatusCode: http.StatusInternalServerError,
		ErrorText:      "Internal server error",
	}
}

func ErrUnauthorized(err error) render.Renderer {
	return &ErrorResponse{
		Err:            err,
		HTTPStatusCode: http.StatusUnauthorized,
		ErrorText:      "Unauthorized",
	}
}

func ErrForbidden(err error) render.Renderer {
	return &ErrorResponse{
		Err:            err,
		HTTPStatusCode: http.StatusForbidden,
		ErrorText:      "Permission denied",
	}
}
