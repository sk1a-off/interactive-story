package aiconfig

type GenerationProfile string

const (
	GenerationQuality GenerationProfile = "quality"
	GenerationBalance GenerationProfile = "balance"
	// Reserved for a future durable finalization protocol; not selectable yet.
	GenerationSpeed GenerationProfile = "speed"
)

func (p GenerationProfile) Effective() GenerationProfile {
	if p == "" {
		return GenerationQuality
	}
	return p
}

func (p GenerationProfile) Available() bool {
	return p.Effective() == GenerationQuality || p == GenerationBalance
}

func (s PublicSettings) WithGenerationDefaults() PublicSettings {
	s.GenerationProfile = s.GenerationProfile.Effective()
	return s
}
