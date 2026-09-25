package portalapi

import (
	"encoding/json"
	"net/http"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func writeJSON(w http.ResponseWriter, statusCode int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(v)
}

type errorBody struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, statusCode int, message string) {
	writeJSON(w, statusCode, errorBody{Error: message})
}

// decodeJSON decodes the request body into v, or writes a 400 and returns
// false on malformed JSON.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "malformed request body")
		return false
	}
	return true
}

// writeStoreOrRPCError maps an error from api.CustomerServer's shared
// validation logic (a *status.Error, since it's also used by the gRPC
// SubmitTask RPC) to the matching HTTP status - one place that agrees on
// what each failure means regardless of transport.
func writeStoreOrRPCError(w http.ResponseWriter, err error) {
	st, ok := status.FromError(err)
	if !ok {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	code := http.StatusInternalServerError
	switch st.Code() {
	case codes.InvalidArgument:
		code = http.StatusBadRequest
	case codes.FailedPrecondition:
		code = http.StatusUnprocessableEntity
	case codes.AlreadyExists:
		code = http.StatusConflict
	case codes.NotFound:
		code = http.StatusNotFound
	case codes.PermissionDenied:
		code = http.StatusForbidden
	case codes.Unauthenticated:
		code = http.StatusUnauthorized
	}
	writeError(w, code, st.Message())
}
