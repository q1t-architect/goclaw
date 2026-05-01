package providers

// KimiCLIUserAgent is the User-Agent value expected by the Kimi Code API
// (api.kimi.com/coding/v1). The endpoint allowlists official client UAs and
// returns 403 for non-whitelisted values (incl. Go default Go-http-client/*).
//
// Pinning to a specific version provides a single source of truth so any
// future bump (KimiCLI/1.5 -> KimiCLI/1.6) is one-line.
const KimiCLIUserAgent = "KimiCLI/1.5"
