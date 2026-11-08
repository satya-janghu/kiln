package steps

import (
	"fmt"
	"strings"

	"github.com/satya-janghu/kiln/internal/run"
)

// doCodecs installs the non-free codecs and the VA-API driver for each GPU (catalog 14).
func doCodecs(c *Ctx) error {
	pkgs := []string{"libavcodec-extra"}
	if c.F.GNOME {
		// The GStreamer plugins serve desktop apps.
		pkgs = append(pkgs, "gstreamer1.0-plugins-good", "gstreamer1.0-plugins-bad", "gstreamer1.0-plugins-ugly", "gstreamer1.0-libav")
	}
	if len(c.F.GPUs) > 0 {
		pkgs = append(pkgs, "vainfo")
	}
	if c.F.HasGPU("1002") {
		pkgs = append(pkgs, "mesa-va-drivers")
	}
	if c.F.HasGPU("8086") {
		// Conflicts with main's intel-media-va-driver, so apt swaps it if GNOME pulled it in.
		pkgs = append(pkgs, "intel-media-va-driver-non-free")
		c.Note("Intel video acceleration uses intel-media-va-driver-non-free, which covers Gen8 (Broadwell) and newer graphics; older Intel graphics aren't covered.")
	}
	if c.Picked("nvidia") && nvidiaSource(c.F) != "" {
		// VA-API on top of NVDEC, for the NVIDIA driver the NVIDIA item installs.
		pkgs = append(pkgs, "nvidia-vaapi-driver")
	}
	if !run.Installed(c.S, "libavcodec-extra") {
		c.Note("Codecs: libavcodec-extra replaces libavcodec61 with libavcodec-extra61 (expected; it provides libavcodec61, so nothing breaks).")
	}
	if err := c.Install(pkgs...); err != nil {
		return err
	}
	var missing []string
	for _, p := range pkgs {
		if !run.Installed(c.S, p) {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("these packages aren't installed after apt finished: %s; check %s and run kiln again", strings.Join(missing, " "), run.LogPath)
	}
	return nil
}
