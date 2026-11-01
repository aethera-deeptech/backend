// Package api holds what every route shares, the error type and its JSON form, the strict JSON reader and the middleware
package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gofiber/fiber/v3"

	"github.com/aethera-deeptech/backend/internal/errorreport"
)

// Error is an error that the service answers with a status and a stable code. The app reads the code and writes its own text.
type Error struct {
	Status            int               `json:"-"`
	Code              string            `json:"code"`
	Message           string            `json:"message"`
	Fields            map[string]string `json:"fields,omitempty"`
	RetryAfterSeconds int               `json:"retryAfterSeconds,omitempty"`
	ExpectedSeq       *int64            `json:"expectedSeq,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// NewError returns an error with a status, a code and a message for a developer
func NewError(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

// Validation returns the 422 answer that names each field that failed with a code such as required, too_short or invalid
func Validation(fields map[string]string) *Error {
	e := NewError(http.StatusUnprocessableEntity, "validation_failed", "A field is not valid")
	e.Fields = fields

	return e
}

// Unauthorized is the answer for a request with no session, or with a session that ended
func Unauthorized() *Error {
	return NewError(http.StatusUnauthorized, "unauthorized", "Sign in first")
}

// NotFound is the answer for an id that does not exist or that belongs to another business, so the two cannot be told apart
func NotFound() *Error {
	return NewError(http.StatusNotFound, "not_found", "Not found")
}

// RateLimited is the answer for a request over a limit. The code is rate_limited unless the caller names another one.
func RateLimited(code string, retryAfter int) *Error {
	if code == "" {
		code = "rate_limited"
	}

	e := NewError(http.StatusTooManyRequests, code, "Too many requests")
	e.RetryAfterSeconds = max(retryAfter, 1)

	return e
}

// ErrorHandler turns every error of a route into the JSON form of the contract. An error that is not an *Error is logged and
// answered as a plain 500 without its text, so no detail of the service leaks.
func ErrorHandler(c fiber.Ctx, err error) error {
	var apiErr *Error

	var fiberErr *fiber.Error

	switch {
	case errors.As(err, &apiErr):
	case errors.As(err, &fiberErr):
		apiErr = fromFiber(fiberErr)
	default:
		slog.Error("a route failed", "method", c.Method(), "path", c.Path(), "error", err)
		errorreport.Report(err, infoOf(c, http.StatusInternalServerError))

		apiErr = NewError(http.StatusInternalServerError, "internal", "Something went wrong")
	}

	if apiErr.RetryAfterSeconds > 0 {
		c.Set(fiber.HeaderRetryAfter, strconv.Itoa(apiErr.RetryAfterSeconds))
	}

	return c.Status(apiErr.Status).JSON(apiErr)
}

func fromFiber(e *fiber.Error) *Error {
	switch e.Code {
	case http.StatusNotFound:
		return NotFound()
	case http.StatusMethodNotAllowed:
		return NewError(e.Code, "method_not_allowed", "This method is not allowed here")
	case http.StatusRequestEntityTooLarge:
		return NewError(e.Code, "payload_too_large", "The request is too large")
	case http.StatusRequestTimeout:
		return NewError(e.Code, "request_timeout", "The request took too long")
	default:
		return NewError(e.Code, "bad_request", http.StatusText(e.Code))
	}
}

// Decode reads the JSON body into dst. It refuses an unknown field, a second JSON value and a body that is not an object, so a
// typo in a field name is an error and not a silent no-op.
func Decode(c fiber.Ctx, dst any) error {
	body := c.Body()
	if len(bytes.TrimSpace(body)) == 0 {
		return NewError(http.StatusBadRequest, "invalid_json", "The body is empty")
	}

	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		return NewError(http.StatusBadRequest, "invalid_json", "The body is not the JSON that this route expects")
	}

	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return NewError(http.StatusBadRequest, "invalid_json", "The body has more than one JSON value")
	}

	return nil
}

// infoOf collects what an error report may hold of a request, the pattern of the route and the status. It never reads the path, the
// query, a header or the body, so what a client typed cannot leave.
func infoOf(c fiber.Ctx, status int) errorreport.Info {
	info := errorreport.Info{Status: status}

	if route := c.Route(); route != nil {
		info.Route = route.Path
	}

	return info
}
