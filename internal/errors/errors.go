// Package errors hosts huddle domain failures and JSON-RPC helpers for MCP tools.
package errors

import (
	stderrors "errors"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

// Domain errors returned by store and slack layers.
var (
	ErrKeyInvalid        = stderrors.New("key invalid or revoked")
	ErrHuddleNotFound    = stderrors.New("huddle not found")
	ErrHuddleClosed      = stderrors.New("huddle closed")
	ErrSlackRateLimited  = stderrors.New("slack rate limited")
	ErrSlackMissingScope = stderrors.New("slack missing scope")
	ErrStorageFailure    = stderrors.New("storage failure")
)

// MCPError builds a JSON-RPC fault suitable for tool handlers that should surface protocol errors.
func MCPError(code int64, err error) *jsonrpc.Error {
	if err == nil {
		err = stderrors.New("unspecified error")
	}
	return &jsonrpc.Error{
		Code:    code,
		Message: err.Error(),
	}
}
