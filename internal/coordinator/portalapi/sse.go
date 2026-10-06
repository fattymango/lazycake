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

// writeSSEJSONWithID is writeSSEJSON plus an "id:" field. The browser
// remembers the last id it saw and sends it back as Last-Event-ID when it
// reconnects, which is what lets a resumed stream pick up where it left off
// instead of replaying everything.
func writeSSEJSONWithID(w io.Writer, id int64, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "id: %d\ndata: %s\n\n", id, data)
}

// writeSSEEnd writes the terminal "end" event. EventSource treats a closed
// stream as a dropped connection and reconnects, so a server that simply
// hangs up on a finished task gets asked for the whole history again; the
// client closes its EventSource when it sees this event instead.
func writeSSEEnd(w io.Writer) {
	fmt.Fprint(w, "event: end\ndata: {}\n\n")
}
