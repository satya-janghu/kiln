package mirrors

// regions is the small built-in neighbor table (catalog step 3, D21): the
// UN M49 sub-regions, as ISO 3166 codes. A country's neighbors are the other
// countries in its sub-region.
var regions = [][]string{
	// Europe
	{"DK", "EE", "FI", "FO", "GB", "IE", "IM", "IS", "LT", "LV", "NO", "SE", "AX", "GG", "JE", "SJ"}, // Northern Europe
	{"AT", "BE", "CH", "DE", "FR", "LI", "LU", "MC", "NL"},                                           // Western Europe
	{"AD", "AL", "BA", "ES", "GI", "GR", "HR", "IT", "ME", "MK", "MT", "PT", "RS", "SI", "SM", "VA"}, // Southern Europe
	{"BG", "BY", "CZ", "HU", "MD", "PL", "RO", "RU", "SK", "UA"},                                     // Eastern Europe
	// Asia
	{"AE", "AM", "AZ", "BH", "CY", "GE", "IL", "IQ", "JO", "KW", "LB", "OM", "PS", "QA", "SA", "SY", "TR", "YE"}, // Western Asia
	{"KG", "KZ", "TJ", "TM", "UZ"},                                     // Central Asia
	{"AF", "BD", "BT", "IN", "IR", "LK", "MV", "NP", "PK"},             // Southern Asia
	{"CN", "HK", "JP", "KP", "KR", "MN", "MO", "TW"},                   // Eastern Asia
	{"BN", "ID", "KH", "LA", "MM", "MY", "PH", "SG", "TH", "TL", "VN"}, // South-eastern Asia
	// Americas
	{"BM", "CA", "GL", "PM", "US"},                                                                   // Northern America
	{"BZ", "CR", "GT", "HN", "MX", "NI", "PA", "SV"},                                                 // Central America
	{"AR", "BO", "BR", "CL", "CO", "EC", "GF", "GY", "PE", "PY", "SR", "UY", "VE"},                   // South America
	{"AG", "AW", "BB", "BS", "CU", "CW", "DM", "DO", "GD", "HT", "JM", "KN", "LC", "PR", "TT", "VC"}, // Caribbean
	// Africa
	{"DZ", "EG", "LY", "MA", "SD", "TN"}, // Northern Africa
	{"BF", "BJ", "CI", "CV", "GH", "GM", "GN", "GW", "LR", "ML", "MR", "NE", "NG", "SL", "SN", "TG"},                         // Western Africa
	{"BI", "DJ", "ER", "ET", "KE", "KM", "MG", "MU", "MW", "MZ", "RE", "RW", "SC", "SO", "SS", "TZ", "UG", "YT", "ZM", "ZW"}, // Eastern Africa
	{"AO", "CD", "CF", "CG", "CM", "GA", "GQ", "ST", "TD"},                                                                   // Middle Africa
	{"BW", "LS", "NA", "SZ", "ZA"}, // Southern Africa
	// Oceania
	{"AU", "NF", "NZ"},                               // Australia and New Zealand
	{"FJ", "NC", "PG", "SB", "VU"},                   // Melanesia
	{"FM", "GU", "KI", "MH", "MP", "NR", "PW"},       // Micronesia
	{"AS", "CK", "NU", "PF", "TO", "TV", "WF", "WS"}, // Polynesia
}

// neighbors returns the other countries in cc's sub-region.
func neighbors(cc string) []string {
	for _, r := range regions {
		for _, c := range r {
			if c != cc {
				continue
			}
			out := make([]string, 0, len(r)-1)
			for _, o := range r {
				if o != cc {
					out = append(out, o)
				}
			}
			return out
		}
	}
	return nil
}
