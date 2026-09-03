// Package httperror provides constants for RFC 9457 Problem Details error responses.
package httperror

const (
	InternalTitle  = "Internal Server Error"
	InternalDetail = "an unexpected error occurred"

	InvalidArgumentTitle       = "Invalid argument"
	InvalidArgumentMultiDetail = "multiple validation errors, see the errors array for details"
	NotFoundTitle              = "Not found"
	AlreadyExistsTitle         = "Already exists"
)
