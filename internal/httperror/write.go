package httperror

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/dcm-project/cnpg-database-service-provider/api/v1alpha1"
	"github.com/dcm-project/cnpg-database-service-provider/internal/util"
)

// WriteResponse writes an RFC 9457 application/problem+json error response.
func WriteResponse(w http.ResponseWriter, logger *slog.Logger, statusCode int,
	errType v1alpha1.ErrorType, title string, detail string, instance *string) {
	resp := v1alpha1.Error{
		Type:     errType,
		Title:    title,
		Status:   util.Ptr(int16(statusCode)),
		Detail:   util.Ptr(detail),
		Instance: instance,
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Error("failed to encode error response", "error", err)
	}
}
