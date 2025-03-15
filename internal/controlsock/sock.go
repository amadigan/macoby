package controlsock

import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/amadigan/macoby/internal/applog"
	"github.com/amadigan/macoby/internal/host/config"
	"github.com/mitchellh/go-ps"
)

var log *applog.Logger = applog.New("controlsock")

type ExistingLockError struct {
	PID       int
	ParentPID int
	Path      string
}

func (e ExistingLockError) Error() string {
	return fmt.Sprintf("existing lock found at %s with PID %d and parent PID %d", e.Path, e.PID, e.ParentPID)
}

type ExistingSocketError struct {
	Path string
}

func (e ExistingSocketError) Error() string {
	return fmt.Sprintf("existing socket found at %s", e.Path)
}

func socketPath(home string) string {
	return fmt.Sprintf("%s/run/%s.sock", home, config.Name)
}

func LockSocket(home string) error {
	contents := fmt.Appendf(nil, "%d\t%d", os.Getpid(), os.Getppid())
	sockpath := socketPath(home)

	f, err := os.OpenFile(sockpath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		stat, err := os.Stat(sockpath)
		if err == nil && stat.Mode()&os.ModeSocket != 0 && checkSocket(sockpath) {
			return &ExistingSocketError{Path: sockpath}
		} else if err := checkLock(sockpath); err != nil {
			return err
		}

		_ = os.Remove(sockpath)

		f, err = os.OpenFile(sockpath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
	}

	defer f.Close()

	_, err = f.Write(contents)

	return err
}

func checkLock(sockpath string) (err error) {
	lockValid := true

	defer func() {
		if !lockValid {
			if err := os.Remove(sockpath); err != nil {
				log.Warnf("failed to remove lock %s: %v", sockpath, err)
			}
		}
	}()

	if bs, err := os.ReadFile(sockpath); err == nil {
		var pid, ppid int

		if _, err := fmt.Sscanf(string(bs), "%d\t%d", &pid, &ppid); err != nil {
			lockValid = false

			return nil
		}

		if ps, err := ps.FindProcess(pid); err != nil || ps.PPid() != ppid {
			lockValid = false

			return nil
		}

		return &ExistingLockError{PID: pid, ParentPID: ppid, Path: sockpath}
	} else {
		lockValid = false
	}

	return nil
}

func checkSocket(sockpath string) bool {
	log.Infof("checking socket %s", sockpath)
	conn, err := net.DialTimeout("unix", sockpath, 1*time.Second)
	if err != nil {
		log.Infof("socket %s is not listening", sockpath)
		if err := os.Remove(sockpath); err != nil {
			log.Warnf("failed to remove socket %s: %v", sockpath, err)
		}

		return false
	}

	_ = conn.Close()
	return true
}

func ListenSocket(home string) (*net.UnixListener, error) {
	sockpath := socketPath(home)

	sock, err := net.ListenUnix("unix", &net.UnixAddr{Name: sockpath, Net: "unix"})
	if err != nil {
		var stat os.FileInfo
		stat, err = os.Stat(sockpath)

		if err == nil {
			if stat.Mode()&os.ModeSocket != 0 {
				if checkSocket(sockpath) {
					return nil, &ExistingSocketError{Path: sockpath}
				}
			} else if err := checkLock(sockpath); err != nil {
				return nil, err
			}
		}

		_ = os.Remove(sockpath)

		sock, err = net.ListenUnix("unix", &net.UnixAddr{Name: sockpath, Net: "unix"})
	}

	return sock, err
}

func DialSocket(home string) (*net.UnixConn, error) {
	return net.DialUnix("unix", nil, &net.UnixAddr{Name: socketPath(home), Net: "unix"})
}
