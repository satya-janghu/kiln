// Derived from NVIDIA/open-gpu-kernel-modules README.md at tag 620.70.06
// ("Compatible GPUs", PCI device IDs; subsystem-qualified rows reduced to their
// device ID). README sha256 adb1d8f1cd024aa36f8b1dc85690b12b7d31d2cf0da87443c61b9966039bf170,
// fetched 2026-11-07. Refresh with each kiln release (catalog step 7).

package multimedia

// openModuleIDs are the PCI device IDs NVIDIA lists as supported by its open
// kernel modules ("any Turing or later GPU"): 299 IDs, 0x1e02 to 0x31c3.
var openModuleIDs = map[string]bool{
	"1e02": true, "1e04": true, "1e07": true, "1e09": true, "1e30": true, "1e36": true, "1e78": true,
	"1e81": true, "1e82": true, "1e84": true, "1e87": true, "1e89": true, "1e90": true, "1e91": true,
	"1e93": true, "1eb0": true, "1eb1": true, "1eb5": true, "1eb6": true, "1eb8": true, "1ec2": true,
	"1ec7": true, "1ed0": true, "1ed1": true, "1ed3": true, "1ef5": true, "1f02": true, "1f03": true,
	"1f06": true, "1f07": true, "1f08": true, "1f0a": true, "1f0b": true, "1f10": true, "1f11": true,
	"1f12": true, "1f14": true, "1f15": true, "1f36": true, "1f42": true, "1f47": true, "1f50": true,
	"1f51": true, "1f54": true, "1f55": true, "1f76": true, "1f82": true, "1f83": true, "1f91": true,
	"1f95": true, "1f96": true, "1f97": true, "1f98": true, "1f99": true, "1f9c": true, "1f9d": true,
	"1f9f": true, "1fa0": true, "1fb0": true, "1fb1": true, "1fb2": true, "1fb6": true, "1fb7": true,
	"1fb8": true, "1fb9": true, "1fba": true, "1fbb": true, "1fbc": true, "1fdd": true, "1ff0": true,
	"1ff2": true, "1ff9": true, "20b0": true, "20b2": true, "20b3": true, "20b5": true, "20b6": true,
	"20b7": true, "20bd": true, "20f1": true, "20f3": true, "20f5": true, "20f6": true, "20fd": true,
	"2182": true, "2184": true, "2187": true, "2188": true, "2189": true, "2191": true, "2192": true,
	"21c4": true, "21d1": true, "2203": true, "2204": true, "2206": true, "2207": true, "2208": true,
	"220a": true, "220d": true, "2216": true, "2230": true, "2231": true, "2232": true, "2233": true,
	"2235": true, "2236": true, "2237": true, "2238": true, "230e": true, "2321": true, "2322": true,
	"2324": true, "2329": true, "232c": true, "2330": true, "2331": true, "2335": true, "2339": true,
	"233a": true, "233b": true, "2342": true, "2348": true, "2414": true, "2420": true, "2438": true,
	"2460": true, "2482": true, "2484": true, "2486": true, "2487": true, "2488": true, "2489": true,
	"248a": true, "249c": true, "249d": true, "24a0": true, "24b0": true, "24b1": true, "24b6": true,
	"24b7": true, "24b8": true, "24b9": true, "24ba": true, "24bb": true, "24c7": true, "24c9": true,
	"24dc": true, "24dd": true, "24e0": true, "24fa": true, "2503": true, "2504": true, "2507": true,
	"2508": true, "2520": true, "2521": true, "2523": true, "2531": true, "2544": true, "2560": true,
	"2563": true, "2571": true, "2582": true, "2584": true, "25a0": true, "25a2": true, "25a5": true,
	"25a6": true, "25a7": true, "25a9": true, "25aa": true, "25ab": true, "25ac": true, "25ad": true,
	"25b0": true, "25b2": true, "25b6": true, "25b8": true, "25b9": true, "25ba": true, "25bb": true,
	"25bc": true, "25bd": true, "25e0": true, "25e2": true, "25e5": true, "25ec": true, "25ed": true,
	"25f9": true, "25fa": true, "25fb": true, "2684": true, "2685": true, "2689": true, "26b1": true,
	"26b2": true, "26b3": true, "26b5": true, "26b9": true, "26ba": true, "2702": true, "2704": true,
	"2705": true, "2709": true, "2717": true, "2730": true, "2757": true, "2770": true, "2782": true,
	"2783": true, "2786": true, "2788": true, "27a0": true, "27b0": true, "27b1": true, "27b2": true,
	"27b6": true, "27b8": true, "27ba": true, "27bb": true, "27e0": true, "27fb": true, "2803": true,
	"2805": true, "2808": true, "2820": true, "2822": true, "2838": true, "2860": true, "2882": true,
	"28a0": true, "28a1": true, "28a3": true, "28b0": true, "28b8": true, "28b9": true, "28ba": true,
	"28bb": true, "28e0": true, "28e1": true, "28e3": true, "28f8": true, "2901": true, "2909": true,
	"2941": true, "29bb": true, "2b85": true, "2b87": true, "2b8c": true, "2bb1": true, "2bb3": true,
	"2bb4": true, "2bb5": true, "2bb9": true, "2bbd": true, "2c02": true, "2c05": true, "2c09": true,
	"2c18": true, "2c19": true, "2c31": true, "2c33": true, "2c34": true, "2c38": true, "2c39": true,
	"2c3a": true, "2c58": true, "2c59": true, "2c77": true, "2c79": true, "2d04": true, "2d05": true,
	"2d18": true, "2d19": true, "2d30": true, "2d39": true, "2d58": true, "2d59": true, "2d79": true,
	"2d83": true, "2d98": true, "2db8": true, "2db9": true, "2dd8": true, "2df9": true, "2e03": true,
	"2e06": true, "2e12": true, "2e13": true, "2f04": true, "2f06": true, "2f18": true, "2f38": true,
	"2f58": true, "3041": true, "3182": true, "31c2": true, "31c3": true,
}
