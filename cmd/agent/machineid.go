package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/fattymango/lazycake/internal/id"
)

// machineIDFile is the file the machine id is kept in.
const machineIDFile = "machine-id"

// loadOrCreateMachineID returns the id that identifies this physical machine across agent restarts, updates and recreated
// containers, creating it on first use. It lives in the host directory the agent already has mounted (hostDir, the same
// bind mount that carries lcinit and survives `podman rm`); an agent running as a plain process, with no mounted host
// directory, keeps it in the user's config directory instead. If neither can be written it returns "": the coordinator
// then falls back to telling agent processes apart, as before this existed, and a restart is a new machine entry.
func loadOrCreateMachineID(hostDir string, log *slog.Logger) string {
	dir := hostDir
	if dir == "" {
		cfg, err := os.UserConfigDir()
		if err != nil {
			log.Warn("no place to keep a machine id; every restart will register as a new machine", "error", err)
			return ""
		}
		dir = filepath.Join(cfg, "lazycake")
	}
	path := filepath.Join(dir, machineIDFile)
	if b, err := os.ReadFile(path); err == nil {
		if v := strings.TrimSpace(string(b)); v != "" {
			return v
		}
	}
	v := id.New("mch")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Warn("couldn't create the machine id directory; every restart will register as a new machine", "dir", dir, "error", err)
		return ""
	}
	// Write-then-rename so a crash can't leave a half-written id that a later start would adopt.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(v+"\n"), 0o644); err != nil {
		log.Warn("couldn't save the machine id; every restart will register as a new machine", "path", path, "error", err)
		return ""
	}
	if err := os.Rename(tmp, path); err != nil {
		log.Warn("couldn't save the machine id; every restart will register as a new machine", "path", path, "error", err)
		return ""
	}
	return v
}
