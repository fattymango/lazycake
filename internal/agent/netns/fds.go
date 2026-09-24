//go:build linux

package netns

import (
	"fmt"
	"net"
	"os"

	"golang.org/x/sys/unix"
)

// sendFDs sends every file in files as ancillary data (SCM_RIGHTS) over
// conn, one at a time. A bound socket's fd keeps working regardless of
// which network namespace the receiving process is in - only the bind()
// call that created it was namespace-sensitive - which is the whole
// reason this package hands fds across a process boundary instead of
// trying to setns() from within the agent itself.
func sendFDs(conn *net.UnixConn, files []*os.File) error {
	for _, f := range files {
		rights := unix.UnixRights(int(f.Fd()))
		if _, _, err := conn.WriteMsgUnix([]byte{0}, rights, nil); err != nil {
			return fmt.Errorf("sending fd for %s: %w", f.Name(), err)
		}
	}
	return nil
}

// socketpairFDs returns a connected pair of unix domain socket fds, for
// the parent/child control channel fd-passing rides on.
func socketpairFDs() ([2]int, error) {
	return unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
}

// recvFDs receives n file descriptors sent by sendFDs, in order.
func recvFDs(conn *net.UnixConn, n int) ([]*os.File, error) {
	files := make([]*os.File, 0, n)
	for i := 0; i < n; i++ {
		buf := make([]byte, 1)
		oob := make([]byte, unix.CmsgSpace(4))
		_, oobn, _, _, err := conn.ReadMsgUnix(buf, oob)
		if err != nil {
			return nil, fmt.Errorf("receiving fd %d/%d: %w", i+1, n, err)
		}
		cmsgs, err := unix.ParseSocketControlMessage(oob[:oobn])
		if err != nil {
			return nil, fmt.Errorf("parsing control message for fd %d/%d: %w", i+1, n, err)
		}
		if len(cmsgs) == 0 {
			return nil, fmt.Errorf("no control message for fd %d/%d", i+1, n)
		}
		fds, err := unix.ParseUnixRights(&cmsgs[0])
		if err != nil {
			return nil, fmt.Errorf("parsing rights for fd %d/%d: %w", i+1, n, err)
		}
		if len(fds) != 1 {
			return nil, fmt.Errorf("expected exactly 1 fd, got %d", len(fds))
		}
		files = append(files, os.NewFile(uintptr(fds[0]), fmt.Sprintf("recv-fd-%d", i)))
	}
	return files, nil
}
