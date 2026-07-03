package ext

// LicenseChecker is the optional edition/license extension point used by API
// feature gates. Community builds normally leave it nil and rely on the static
// feature map. Enterprise builds can derive capabilities from license/tier
// state without exposing license internals through the public /api/features
// response.
type LicenseChecker interface {
	FeatureEnabled(feature string) bool
}
