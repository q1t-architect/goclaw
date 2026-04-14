package tools

import (
	"context"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/audio/elevenlabs"
)

// callElevenLabsSoundEffect delegates to audio/elevenlabs.SFXProvider.
// Phase 1 keeps this thin shim so create_audio.go callers don't change;
// Phase 3 removes the shim and wires audio.Manager.GenerateSFX directly.
func (t *CreateAudioTool) callElevenLabsSoundEffect(ctx context.Context, prompt string, durationSeconds int) ([]byte, error) {
	provider := elevenlabs.NewSFXProvider(elevenlabs.Config{
		APIKey:  t.elevenlabsAPIKey,
		BaseURL: t.elevenlabsBaseURL,
	})
	result, err := provider.GenerateSFX(ctx, audio.SFXOptions{
		Prompt:   prompt,
		Duration: durationSeconds,
	})
	if err != nil {
		return nil, err
	}
	return result.Audio, nil
}
