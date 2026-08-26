package server

import (
	"fmt"
	"net/http"
)

// ErrorCode is a stable machine-readable identifier a panel can switch on.
// The HTTP status is the network-level signal; the code survives as the API
// evolves, so client logic need not re-derive intent from the status alone.
type ErrorCode string

const (
	ErrBadRequest       ErrorCode = "bad_request"
	ErrUnauthorized     ErrorCode = "unauthorized"
	ErrForbidden        ErrorCode = "forbidden"
	ErrNotFound         ErrorCode = "not_found"
	ErrConflict         ErrorCode = "conflict"
	ErrUnprocessable    ErrorCode = "unprocessable"
	ErrMethodNotAllowed ErrorCode = "method_not_allowed"
	ErrNotImplemented   ErrorCode = "not_implemented"
	ErrUpstream         ErrorCode = "upstream_error"
	ErrInternal         ErrorCode = "internal"
)

// apiError is the single error shape returned by every /v1 endpoint. A panel
// reads `error.code` to branch and `error.message` to surface to a human.
type apiError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
	Status  int       `json:"-"` // HTTP status, not serialised
}

// writeErr writes a well-formed error envelope with the given status + code.
func writeErr(w http.ResponseWriter, status int, code ErrorCode, format string, args ...interface{}) {
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	writeJSON(w, status, map[string]interface{}{
		"error": apiError{Code: code, Message: msg, Status: status},
	})
}

// writeError keeps the short (status + message) signature as a compatible
// bridge for existing call sites; the message is a literal value, never a
// printf format string, so callers passing err.Error() are safe.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeErr(w, status, codeForStatus(status), "%s", msg)
}

// writeUpstreamError is used when the local cardinal node (or a remote node)
// fails; it maps common daemon errors onto apt statuses.
func writeUpstreamError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	writeErr(w, http.StatusBadGateway, ErrUpstream, "%s", err.Error())
}

func codeForStatus(status int) ErrorCode {
	switch status {
	case http.StatusBadRequest:
		return ErrBadRequest
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusForbidden:
		return ErrForbidden
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusConflict:
		return ErrConflict
	case http.StatusUnprocessableEntity:
		return ErrUnprocessable
	case http.StatusMethodNotAllowed:
		return ErrMethodNotAllowed
	case http.StatusNotImplemented:
		return ErrNotImplemented
	case http.StatusBadGateway:
		return ErrUpstream
	default:
		return ErrInternal
	}
}
