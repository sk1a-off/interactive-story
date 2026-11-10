package aiconfig

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestGenerationProfileDefaultsAndValidation(t *testing.T) {
	for _, profile := range []GenerationProfile{"", GenerationQuality, GenerationBalance, GenerationSpeed, "unknown"} {
		t.Run(string(profile), func(t *testing.T) {
			settings := PublicSettings{StoryLLMContext: 16384, GenerationProfile: profile}
			err := settings.Validate()
			valid := profile == "" || profile == GenerationQuality || profile == GenerationBalance
			if valid && err != nil || !valid && !errors.Is(err, ErrInvalidPublicSettings) {
				t.Fatalf("unexpected profile validation: %v", err)
			}
			var roundtrip PublicSettings
			if err := json.Unmarshal(settings.JSON(), &roundtrip); err != nil {
				t.Fatal(err)
			}
			want := profile
			if want == "" {
				want = GenerationQuality
			}
			if roundtrip.GenerationProfile != want {
				t.Fatalf("profile lost in settings: %q", roundtrip.GenerationProfile)
			}
		})
	}
}
