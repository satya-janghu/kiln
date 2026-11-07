package battery

// The method ladder as data (catalog step 13, "Method ladder"). Adding a
// vendor is a table row here plus its primary-source citation in
// docs/debian13-notes.md. Sources: kernel ABI sysfs-class-power (v6.12, v7.2),
// drivers/platform/x86/ (v6.12) and the TLP vendor matrix
// (linrunner.de/tlp/settings/bc-vendors.html, verified 2026-11).

// MaxStop is battery.max_stop: a stop value at or above it is never tried or accepted.
const MaxStop = 90

// stopCandidates are the method-1 stop values; candidates() orders them by
// distance from the target, the higher value first on a tie (80 → 80 85 75 70 60 50).
var stopCandidates = []int{85, 80, 75, 70, 60, 50}

// thresholdQuirk describes a method-1 driver that deviates from the kernel ABI.
// Toshiba (end only, 80/100) and MSI (msi_ec, start forced to stop−10) use the
// standard attributes and need no row: the generic read-back rule accepts any
// value that reads back below MaxStop.
type thresholdQuirk struct {
	Name   string
	Module string // matched against /sys/module/<Module>
	Vendor string // DMI sys_vendor substring (case-insensitive); "" = any
	// Clamps: the driver silently clamps instead of returning EINVAL (dell-laptop:
	// start 50–95, end 55–100, keeps a 5-point gap). Whatever reads back < MaxStop is accepted.
	Clamps bool
	// Cached: the read-back is the driver's cached value, not the hardware's
	// (asus-wmi), so the result is recorded as "set, not verifiable".
	Cached bool
	// FailHint is added to the summary when this driver's thresholds can't be set.
	FailHint string
}

var thresholdQuirks = []thresholdQuirk{
	{Name: "dell-laptop", Module: "dell_laptop", Clamps: true,
		FailHint: "On Dell laptops a BIOS admin password blocks charge-limit writes; remove it temporarily and run kiln again."},
	{Name: "asus-wmi", Module: "asus_wmi", Cached: true},
}

// fixedMode is a method-2 vendor fixed mode.
type fixedMode struct {
	Name   string // recorded as "vendor:<Name>"
	Module string // /sys/module/<Module> must exist; "" = no module check
	Vendor string // DMI sys_vendor substring (case-insensitive); "" = any
	// Glob locates the attribute; "{bat}" is replaced by each battery's sysfs path.
	Glob string
	// Values are tried in order; each is written as-is.
	Values []string
	// Selection: the attribute lists choices with the active one in [brackets]
	// (charge_types), and the value must be listed to be tried.
	Selection bool
	// Label describes the accepted mode in the summary and `kiln battery status`.
	Label string
}

var fixedModes = []fixedMode{
	{Name: "ideapad-conservation", Module: "ideapad_laptop", Vendor: "LENOVO",
		Glob:   "/sys/bus/platform/drivers/ideapad_acpi/VPC2004:*/conservation_mode",
		Values: []string{"1"},
		Label:  "IdeaPad conservation mode (caps around 60% or 80% depending on the model; the actual cap can't be read)"},
	{Name: "long-life", Glob: "{bat}/charge_types", Values: []string{"Long_Life"}, Selection: true,
		Label: "charge type Long_Life (a fixed limit set by the firmware; the actual cap can't be read)"},
	{Name: "samsung-extender", Module: "samsung_laptop",
		Glob: "/sys/devices/platform/samsung/battery_life_extender", Values: []string{"1"},
		Label: "Samsung battery life extender (80%)"},
	{Name: "lg-care", Module: "lg_laptop",
		Glob: "/sys/devices/platform/lg-laptop/battery_care_limit", Values: []string{"80"},
		Label: "LG battery care limit (80%)"},
	{Name: "huawei-thresholds", Module: "huawei_wmi",
		Glob: "/sys/devices/platform/huawei-wmi/charge_control_thresholds", Values: []string{"75 80"},
		Label: "Huawei charge thresholds 75–80%"},
	{Name: "sony-care", Module: "sony_laptop",
		Glob: "/sys/devices/platform/sony-laptop/battery_care_limiter", Values: []string{"80", "50"},
		Label: "Sony battery care limiter"},
}

// biosOptions are the firmware settings method 4 points at.
var biosOptions = []string{"Battery Care", "Adaptive Battery Optimizer", "Battery Health Manager", "Primarily AC use", "Charge limit"}
