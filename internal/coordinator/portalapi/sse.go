package portalapi

import (
	"encoding/json"
	"fmt"
	"io"
)

// writeSSEJSON writes v as one "data: <json>\n\n" SSE frame. Errors are
// swallowed - same posture as internal/coordinator/dashboard's handleEvents:
// a write failure here means the client is gone, which the caller's next
// loop iteration (or ctx.Done()) will notice on its own.
func writeSSEJSON(w io.Writer, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
}
