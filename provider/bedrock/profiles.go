package bedrock

// -----------------------------------------------------------------------------
// AWS Region Constants
// -----------------------------------------------------------------------------

// Common AWS regions.
const (
	RegionUSEast1      = "us-east-1"
	RegionUSEast2      = "us-east-2"
	RegionUSWest2      = "us-west-2"
	RegionEUCentral1   = "eu-central-1"
	RegionEUWest1      = "eu-west-1"
	RegionAPNortheast1 = "ap-northeast-1"
)

// DefaultRegion is the default AWS region for Bedrock.
const DefaultRegion = RegionUSEast1

// -----------------------------------------------------------------------------
// Inference Profile Prefix Constants
// -----------------------------------------------------------------------------

// Inference profile prefixes for cross-region inference.
const (
	PrefixEU     = "eu"
	PrefixUS     = "us"
	PrefixAPAC   = "apac"
	PrefixGlobal = "global"
)

// -----------------------------------------------------------------------------
// Inference Profile Types and Data
// -----------------------------------------------------------------------------

// InferenceProfile defines available region prefixes for a model.
type InferenceProfile struct {
	Prefixes []string // Available: PrefixEU, PrefixUS, PrefixAPAC, PrefixGlobal
}

// regionPrefixes maps AWS region prefixes to inference profile prefixes.
var regionPrefixes = map[string]string{
	"eu-": PrefixEU,
	"us-": PrefixUS,
	"ap-": PrefixAPAC,
}

// validPrefixes lists all valid inference profile prefixes with dot suffix.
var validPrefixes = []string{
	PrefixEU + ".",
	PrefixUS + ".",
	PrefixAPAC + ".",
	PrefixGlobal + ".",
}

// -----------------------------------------------------------------------------
// Helper Functions
// -----------------------------------------------------------------------------

// computeRegionPrefix determines the inference profile prefix for an AWS region.
// Examples: "us-east-1" -> "us", "eu-central-1" -> "eu", "ap-northeast-1" -> "apac"
func computeRegionPrefix(region string) string {
	for regionPrefix, profilePrefix := range regionPrefixes {
		if len(region) >= len(regionPrefix) && region[:len(regionPrefix)] == regionPrefix {
			return profilePrefix
		}
	}
	return PrefixGlobal
}

// hasRegionPrefix checks if a model ID already has a region prefix.
func hasRegionPrefix(model string) bool {
	for _, prefix := range validPrefixes {
		if len(model) > len(prefix) && model[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

// containsPrefix checks if a prefix is in the list.
func containsPrefix(prefixes []string, prefix string) bool {
	for _, p := range prefixes {
		if p == prefix {
			return true
		}
	}
	return false
}
