package steps

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/satya-janghu/kiln/internal/run"
)

// dkms's module signing key (dkms 3.2.2) and the temporary hash file.
const (
	mokKey      = "/var/lib/dkms/mok.key"
	mokPub      = "/var/lib/dkms/mok.pub"
	mokHashFile = "/root/.kiln-mok.hash"
)

type mokStatus int

const (
	mokUnknown mokStatus = iota
	mokNotEnrolled
	mokQueued // already enrolled, or in the enrollment request
	mokBlocked
)

// mokState runs `mokutil --test-key`: exit 0 "is not enrolled"; exit 1
// "already enrolled / in the enrollment request" or "blocked" (mokutil 0.7.2).
func mokState(c *Ctx) mokStatus {
	out, err := c.Exec(run.Cmd{Name: "mokutil", Args: []string{"--test-key", mokPub}, Env: []string{"LC_ALL=C"}, Quiet: true})
	var ee *run.ExitError
	switch {
	case err == nil && strings.Contains(out, "is not enrolled"):
		return mokNotEnrolled
	case errors.As(err, &ee) && ee.Code == 1 && strings.Contains(ee.Output, "blocked"):
		return mokBlocked
	case errors.As(err, &ee) && ee.Code == 1 && strings.Contains(ee.Output, "already"):
		return mokQueued
	}
	return mokUnknown
}

// enrollMOK makes sure dkms's signing key exists and is enrolled or queued for
// enrollment (catalog 17, step 1). When it queues the key, the one-time
// password lives only in memory and on the summary screen (SetSecret), and
// reaches mokutil only on stdin. Used by the NVIDIA and battery steps.
func enrollMOK(c *Ctx) error {
	if err := c.Install("mokutil", "openssl", "dkms"); err != nil {
		return err
	}
	if err := c.Run("dkms", "generate_mok"); err != nil {
		return fmt.Errorf("dkms couldn't create its module signing key: %w", err)
	}
	if !run.Exists(mokKey) || !run.Exists(mokPub) {
		return fmt.Errorf("`dkms generate_mok` didn't create %s and %s", mokKey, mokPub)
	}
	switch mokState(c) {
	case mokQueued:
		return nil
	case mokBlocked:
		return fmt.Errorf("the firmware blocks dkms's signing key %s (dbx or MokListX)", mokPub)
	case mokUnknown:
		return fmt.Errorf("`mokutil --test-key %s` gave an unexpected answer (details in %s)", mokPub, run.LogPath)
	}

	otp, err := mokPassword(8)
	if err != nil {
		return err
	}
	c.SetSecret(otp) // redacted from the log from here on
	out, err := c.Exec(run.Cmd{Name: "mokutil", Args: []string{"--generate-hash"}, Stdin: otp + "\n" + otp + "\n", Private: true})
	if err != nil {
		return fmt.Errorf("mokutil couldn't hash the one-time password: %w", err)
	}
	hash := ""
	for _, l := range run.Scan(out) {
		if strings.HasPrefix(l, "$") {
			hash = l
		}
	}
	if hash == "" {
		return errors.New("`mokutil --generate-hash` printed no hash")
	}
	if err := c.Write(mokHashFile, 0o600, hash+"\n"); err != nil {
		return err
	}
	ierr := c.Run("mokutil", "--import", mokPub, "--hash-file", mokHashFile)
	_ = c.Remove(mokHashFile)
	if ierr != nil {
		return fmt.Errorf("mokutil couldn't queue the signing key for enrollment: %w", ierr)
	}
	c.Reboot("enroll the module signing key (MOK) at the boot screen")
	c.Note("Secure Boot: at the next boot choose *Enroll MOK* → *Continue* and enter the one-time password shown on this screen.")
	return nil
}

// mokPassword makes an n-character password that's easy to type at the
// firmware's boot screen (no 0/o or 1/l).
func mokPassword(n int) (string, error) {
	const alphabet = "abcdefghjkmnpqrstuvwxyz23456789"
	b := make([]byte, n)
	for i := range b {
		x, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", fmt.Errorf("couldn't make a one-time password: %w", err)
		}
		b[i] = alphabet[x.Int64()]
	}
	return string(b), nil
}
