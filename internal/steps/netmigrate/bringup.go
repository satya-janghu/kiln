package netmigrate

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/satya-janghu/kiln/internal/revert"
	"github.com/satya-janghu/kiln/internal/sys"
)

// ifupdownUp is the registered restore step that puts an interface back on
// ifupdown after a failed migration (catalog step 6.7, D48, D49). It runs
// after the snapshot restore, so it reads the original interfaces file.
const ifupdownUp = "ifupdown-up"

func init() { revert.Register(ifupdownUp, bringUpStep) }

// upRestore is the restore command recorded in state.json for iface.
func upRestore(iface string) []string { return []string{revert.Builtin, ifupdownUp, iface} }

const (
	// supplicantRelease is how long NetworkManager's wpa_supplicant may take to
	// let go of the interface after `nmcli device set … managed no`.
	supplicantRelease = 30 * time.Second
	// ifupDeadline covers ifup@.service's TimeoutStartSec=5min plus margin.
	ifupDeadline = 6 * time.Minute
)

func bringUpStep(ctx context.Context, s sys.System, args []string) error {
	if len(args) != 1 || args[0] == "" {
		return fmt.Errorf("%s needs one interface name", ifupdownUp)
	}
	iface := args[0]
	argv := parse(s).bringUp(iface)

	// NetworkManager releases the interface from its wpa_supplicant
	// asynchronously. ifupdown's wpa_supplicant uses the same control
	// directory and refuses to start while /run/wpa_supplicant/<iface> exists
	// ("ctrl_iface exists and seems to be in use"; seen on the HP test
	// machine, which then stayed offline). Wait for the socket to go away; if
	// it doesn't, go on anyway and let the unit's result say what happened.
	sock := "/run/wpa_supplicant/" + iface
	_ = revert.Poll(ctx, supplicantRelease, 250*time.Millisecond, func() error {
		if _, err := s.Stat(sock); errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("%s still exists", sock)
	})

	if _, err := s.Run(ctx, sys.Cmd{Name: argv[0], Args: argv[1:], Timeout: ifupDeadline}); err != nil {
		return fmt.Errorf("bringing %s back up on ifupdown: %w", iface, err)
	}
	unit, ok := strings.CutPrefix(strings.Join(argv, " "), "systemctl restart ifup@")
	if !ok {
		return nil // networking.service is Type=oneshot and plain ifup runs in the foreground: both finish before returning
	}
	// ifup@.service has no Type=, so it is Type=simple: `systemctl restart`
	// returns once ifup has been started, not when it has finished
	// (systemd.service(5)). With RemainAfterExit=true it ends "active/exited"
	// if ifup succeeded and "failed" if it didn't.
	unit = "ifup@" + unit
	return revert.Poll(ctx, ifupDeadline, time.Second, func() error {
		r, err := s.Query(ctx, sys.Cmd{Name: "systemctl", Args: []string{"show", "-p", "ActiveState", "-p", "SubState", unit}})
		if err != nil {
			return err
		}
		st := r.Stdout
		switch {
		case strings.Contains(st, "ActiveState=active") && strings.Contains(st, "SubState=exited"):
			return nil
		case strings.Contains(st, "ActiveState=failed"):
			return revert.Final(fmt.Errorf("ifup failed for %s; see `journalctl -u %s`", iface, unit))
		}
		return fmt.Errorf("%s is still starting", unit)
	})
}
