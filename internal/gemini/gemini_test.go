package gemini

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

func TestClamp(t *testing.T) {
	tests := []struct {
		val, min, max float64
		expected      float64
	}{
		{0.5, 0.0, 1.0, 0.5},
		{-0.2, 0.0, 1.0, 0.0},
		{1.5, 0.0, 1.0, 1.0},
		{0.0, 0.0, 1.0, 0.0},
		{1.0, 0.0, 1.0, 1.0},
	}

	for _, tt := range tests {
		got := clamp(tt.val, tt.min, tt.max)
		if got != tt.expected {
			t.Errorf("clamp(%f, %f, %f) = %f; expected %f", tt.val, tt.min, tt.max, got, tt.expected)
		}
	}
}

func TestAnalysisResultParsing(t *testing.T) {
	rawJSON := `{
		"patch_name": "Test Sine Bass",
		"osc1_waveform": "Sine",
		"filter1_cutoff": 0.654,
		"filter1_resonance": 0.21,
		"env1_attack": 0.001,
		"env1_decay": 0.32,
		"env1_sustain": 0.7,
		"env1_release": 0.15,
		"additional_parameters": [
			{
				"id": "Engine2_Type",
				"value": "1",
				"description": "Sample engine enabled"
			},
			{
				"id": "Fx_BusA_Reverb_Wet",
				"value": "0.350000",
				"description": "Add space"
			}
		]
	}`

	var res AnalysisResult
	err := json.Unmarshal([]byte(rawJSON), &res)
	if err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if res.PatchName != "Test Sine Bass" {
		t.Errorf("expected PatchName 'Test Sine Bass', got '%s'", res.PatchName)
	}
	if res.Osc1Waveform != "Sine" {
		t.Errorf("expected Osc1Waveform 'Sine', got '%s'", res.Osc1Waveform)
	}
	if res.Filter1Cutoff != 0.654 {
		t.Errorf("expected Filter1Cutoff 0.654, got %f", res.Filter1Cutoff)
	}
	if len(res.AdditionalParameters) != 2 {
		t.Errorf("expected 2 additional parameters, got %d", len(res.AdditionalParameters))
	}
	if res.AdditionalParameters[0].ID != "Engine2_Type" || res.AdditionalParameters[0].Value != "1" {
		t.Errorf("unexpected first additional param: %+v", res.AdditionalParameters[0])
	}
}

func TestAnalyzeAudioMissingApiKey(t *testing.T) {
	// Temporarily unset API key
	origKey := os.Getenv("GEMINI_API_KEY")
	os.Unsetenv("GEMINI_API_KEY")
	defer func() {
		if origKey != "" {
			os.Setenv("GEMINI_API_KEY", origKey)
		}
	}()

	_, err := AnalyzeAudio(context.Background(), "dummy.wav", "audio/wav", "")
	if err == nil {
		t.Error("expected error due to missing API key, got nil")
	}
}
