// Package id generates prefixed, sortable, human-readable identifiers:
// tsk_<ulid>, nod_<ulid>, gw_<ulid>, act_<ulid>.
package id

import (
	"crypto/rand"
	"strings"

	"github.com/oklog/ulid/v2"
)

const (
	Task    = "tsk"
	Node    = "nod"
	Gateway = "gw"
	Account = "act"
)

// New generates a new ID with the given prefix, e.g. New(Task) -> "tsk_01H...".
func New(prefix string) string {
	return prefix + "_" + ulid.MustNew(ulid.Now(), rand.Reader).String()
}

// HasPrefix reports whether id was minted with New(prefix).
func HasPrefix(idStr, prefix string) bool {
	return strings.HasPrefix(idStr, prefix+"_")
}
