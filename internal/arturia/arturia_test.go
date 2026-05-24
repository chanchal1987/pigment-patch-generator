package arturia

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chanchal1987/pigment-patch-generator/internal/gemini"
)

func TestGeneratePGTX(t *testing.T) {
	mockResult := &gemini.AnalysisResult{
		PatchName:        "Test Synth Lead",
		Osc1Waveform:     "Triangle",
		Filter1Cutoff:    0.654321,
		Filter1Resonance: 0.21,
		Env1Attack:       0.001,
		Env1Decay:        0.32,
		Env1Sustain:      0.7,
		Env1Release:      0.15,
		AdditionalParameters: []gemini.AdditionalParam{
			{
				ID:          "Engine2_Type",
				Value:       "Sample",
				Description: "Sample engine for layered texture",
			},
			{
				// Legacy FX bus ID → should auto-activate FX1_ModuleType to Reverb
				ID:          "Fx_BusA_Reverb_Wet",
				Value:       "0.35",
				Description: "Ambience",
			},
			{
				ID:          "Filter1_Cutoff", // Should overwrite basic filter cutoff value
				Value:       "0.500000",
				Description: "Override Cutoff",
			},
		},
	}

	pgtxBytes, err := GeneratePGTX(mockResult)
	if err != nil {
		t.Fatalf("failed to generate PGTX: %v", err)
	}

	// 1. Verify ZIP readability and structure
	zipReader, err := zip.NewReader(bytes.NewReader(pgtxBytes), int64(len(pgtxBytes)))
	if err != nil {
		t.Fatalf("generated data is not a valid zip archive: %v", err)
	}

	var hasPreset, hasPNG bool
	var presetContent []byte

	for _, f := range zipReader.File {
		// Verify system creator and attributes (FAT, 0 attributes, 0 flag bits)
		if f.CreatorVersion>>8 != 0 {
			t.Errorf("expected ZIP creator system to be 0 (FAT), got %d", f.CreatorVersion>>8)
		}
		if f.ExternalAttrs != 0 {
			t.Errorf("expected ExternalAttrs to be 0, got %d", f.ExternalAttrs)
		}
		if len(f.Extra) > 0 {
			t.Errorf("expected no extra headers, got %v", f.Extra)
		}
		if f.Flags != 0 {
			t.Errorf("expected flag bits to be 0 (no data descriptors), got %d", f.Flags)
		}

		if f.Name == "Pigments/User/User/Test Synth Lead" {
			hasPreset = true
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("failed to open preset file in zip: %v", err)
			}
			presetContent, err = io.ReadAll(rc)
			rc.Close()
			if err != nil {
				t.Fatalf("failed to read preset content: %v", err)
			}
		} else if f.Name == "User.png" {
			hasPNG = true
		}
	}

	if !hasPreset {
		t.Error("zip is missing the preset file entry")
	}
	if !hasPNG {
		t.Error("zip is missing User.png")
	}

	// 2. Verify metadata replacements in Boost text archive
	presetStr := string(presetContent)
	if !strings.Contains(presetStr, "7 15 Test Synth Lead 4 User 22 12 Gemini Agent") {
		t.Error("preset metadata header was not correctly replaced")
	}
	if !strings.Contains(presetStr, "18 OriginalPresetName 15 Test Synth Lead") {
		t.Error("OriginalPresetName metadata field was not correctly replaced")
	}

	// 3. Verify parameter replacements with CORRECT fractional type values
	expectedParams := map[string]string{
		// Engine 1: Wavetable = 0.25 (v7 fractional value)
		"Engine1_ModuleType":       "0.25",
		"Engine1_WTOsc_FrameIndex": "0.250000",
		// Filter 1: Multimode = 0.2 (v7 fractional value)
		"Filter1_ModuleType": "0.2",
		"Filter1_Cutoff":     "0.500000", // Overwritten by additional param!
		"Filter1_Resonance":  "0.210000",
		// Envelope
		"Env1_Attack":  "0.001000",
		"Env1_Decay":   "0.320000",
		"Env1_Sustain":  "0.700000",
		"Env1_Release":  "0.150000",
		// Engine 2: Sample = 0.5 (v7 fractional value, translated from "Sample" string)
		"Engine2_ModuleType": "0.5",
		// FX1 Dry_Wet (from legacy Fx_BusA_Reverb_Wet)
		"FX1_Dry_Wet": "0.350000",
		// FX1 ModuleType should be auto-activated to Reverb fraction
		"FX1_ModuleType": "0.61538464",
	}

	for id, val := range expectedParams {
		searchKey := fmt.Sprintf(" %d %s ", len(id), id)
		idx := strings.Index(presetStr, searchKey)
		if idx == -1 {
			t.Errorf("missing parameter %s in generated preset", id)
			continue
		}
		valStart := idx + len(searchKey)
		valEnd := strings.Index(presetStr[valStart:], " ")
		if valEnd == -1 {
			t.Errorf("unterminated value for parameter %s", id)
			continue
		}
		gotVal := presetStr[valStart : valStart+valEnd]
		if gotVal != val {
			t.Errorf("parameter %s: expected %s, got %s", id, val, gotVal)
		}
	}
}

func TestGeneratePGTXWithFXSlots(t *testing.T) {
	// Test the new FX slot-based format (Fx1_Type + Fx1_Amount)
	mockResult := &gemini.AnalysisResult{
		PatchName:        "FX Test Patch",
		Osc1Waveform:     "Saw",
		Filter1Cutoff:    0.8,
		Filter1Resonance: 0.2,
		Env1Attack:       0.01,
		Env1Decay:        0.3,
		Env1Sustain:      0.8,
		Env1Release:      0.2,
		AdditionalParameters: []gemini.AdditionalParam{
			{ID: "Fx1_Type", Value: "Reverb", Description: "Hall reverb"},
			{ID: "Fx1_Amount", Value: "0.65", Description: "Wet reverb mix"},
			{ID: "Fx2_Type", Value: "Delay", Description: "Echo effect"},
			{ID: "Fx2_Amount", Value: "0.40", Description: "Delay mix"},
			{ID: "Fx3_Type", Value: "Chorus", Description: "Widening"},
			{ID: "Fx3_Amount", Value: "0.30", Description: "Chorus mix"},
		},
	}

	pgtxBytes, err := GeneratePGTX(mockResult)
	if err != nil {
		t.Fatalf("failed to generate PGTX: %v", err)
	}

	zipReader, err := zip.NewReader(bytes.NewReader(pgtxBytes), int64(len(pgtxBytes)))
	if err != nil {
		t.Fatalf("invalid zip: %v", err)
	}

	var presetContent []byte
	for _, f := range zipReader.File {
		if strings.HasPrefix(f.Name, "Pigments/User/") {
			rc, _ := f.Open()
			presetContent, _ = io.ReadAll(rc)
			rc.Close()
		}
	}

	presetStr := string(presetContent)

	// Verify FX types are correctly set to fractional values
	fxExpected := map[string]string{
		"FX1_ModuleType": "0.61538464", // Reverb
		"FX1_Dry_Wet":    "0.650000",
		"FX2_ModuleType": "0.46153846", // Delay
		"FX2_Dry_Wet":    "0.400000",
		"FX3_ModuleType": "0.07692308", // Chorus
		"FX3_Dry_Wet":    "0.300000",
	}

	for id, val := range fxExpected {
		searchKey := fmt.Sprintf(" %d %s ", len(id), id)
		idx := strings.Index(presetStr, searchKey)
		if idx == -1 {
			t.Errorf("missing parameter %s in generated preset", id)
			continue
		}
		valStart := idx + len(searchKey)
		valEnd := strings.Index(presetStr[valStart:], " ")
		if valEnd == -1 {
			t.Errorf("unterminated value for parameter %s", id)
			continue
		}
		gotVal := presetStr[valStart : valStart+valEnd]
		if gotVal != val {
			t.Errorf("parameter %s: expected %s, got %s", id, val, gotVal)
		}
	}
}

func TestEngineTypeMapping(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Wavetable", "0.25"},
		{"Analog", "0"},
		{"Sample", "0.5"},
		{"Harmonic", "0.75"},
		// Legacy integer values should also map correctly
		{"0", "0"},
		{"1", "0.25"},
		{"2", "0.5"},
		{"3", "0.75"},
	}

	for _, tt := range tests {
		got := resolveTypeValue("Engine1_ModuleType", tt.input)
		if got != tt.expected {
			t.Errorf("EngineType(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestFXTypeMapping(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Reverb", "0.61538464"},
		{"Delay", "0.46153846"},
		{"Chorus", "0.07692308"},
		{"Distortion", "0.15384616"},
		{"Phaser", "0.38461539"},
		{"Flanger", "0.30769232"},
		{"None", "0"},
	}

	for _, tt := range tests {
		got := resolveTypeValue("FX1_ModuleType", tt.input)
		if got != tt.expected {
			t.Errorf("FXType(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestSavePGTX(t *testing.T) {
	tempDir := "./tmp_test_arturia"
	defer os.RemoveAll(tempDir)

	pgtxData := []byte("fake zip bytes")
	destPath := filepath.Join(tempDir, "test.pgtx")

	err := SavePGTX(pgtxData, destPath)
	if err != nil {
		t.Fatalf("failed to save PGTX: %v", err)
	}

	// Read and verify file contents directly
	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("failed to read saved PGTX file: %v", err)
	}

	if string(data) != string(pgtxData) {
		t.Errorf("expected file content %s, got %s", string(pgtxData), string(data))
	}
}

func TestGenerateMarkdown(t *testing.T) {
	mockResult := &gemini.AnalysisResult{
		PatchName:        "Test Saw Preset",
		Osc1Waveform:     "Saw",
		Filter1Cutoff:    0.8,
		Filter1Resonance: 0.3,
		Env1Attack:       0.0,
		Env1Decay:        0.4,
		Env1Sustain:      1.0,
		Env1Release:      0.2,
		AdditionalParameters: []gemini.AdditionalParam{
			{
				ID:          "Engine2_Type",
				Value:       "Sample",
				Description: "Sample Engine used",
			},
			{
				ID:          "Fx1_Type",
				Value:       "Delay",
				Description: "Delays added",
			},
			{
				ID:          "Fx1_Amount",
				Value:       "0.25",
				Description: "Delay mix",
			},
		},
	}

	mdContent := GenerateMarkdown(mockResult)

	if !strings.Contains(mdContent, "Wavetable") {
		t.Error("expected guide to mention Wavetable engine type")
	}
	if !strings.Contains(mdContent, "Saw") {
		t.Error("expected guide to mention Saw waveform")
	}
	if !strings.Contains(mdContent, "0.800000") || !strings.Contains(mdContent, "80.0%") {
		t.Error("expected cutoff formatting details in guide")
	}
	if !strings.Contains(mdContent, "Test Saw Preset") {
		t.Error("expected guide to contain custom preset name")
	}
	if !strings.Contains(mdContent, "Engine2_Type") || !strings.Contains(mdContent, "Fx1_Type") {
		t.Error("expected guide to list additional parameters")
	}
	if !strings.Contains(mdContent, "25.0%") {
		t.Error("expected percentage calculation in markdown table")
	}
}

func TestRebuildParams(t *testing.T) {
	// Rebuild using empty map shouldn't change the count or order
	rebuilt, err := RebuildParams(defaultTemplate, map[string]string{})
	if err != nil {
		t.Fatalf("failed empty RebuildParams: %v", err)
	}

	// Verify the count in rebuilt bytes matches original count (3335)
	if !bytes.Contains(rebuilt, []byte(" 3335 0 0 0 ")) {
		t.Error("expected count 3335 for empty rebuild")
	}

	// Rebuild with custom parameters and check if count and sorting are correct
	updates := map[string]string{
		"LFO1_Setting": "1",
		"Modulations_F1 Cutoff_LFO 1_Amount": "0.75",
	}

	rebuilt2, err := RebuildParams(defaultTemplate, updates)
	if err != nil {
		t.Fatalf("failed RebuildParams with updates: %v", err)
	}

	// Should have 3336 parameters (LFO1_Setting already exists in default, Modulations_F1 Cutoff_LFO 1_Amount is new)
	if !bytes.Contains(rebuilt2, []byte(" 3336 0 0 0 ")) {
		t.Error("expected count 3336 after adding one new parameter")
	}

	// Verify LFO1_Setting value is updated to 1
	if !bytes.Contains(rebuilt2, []byte(" 12 LFO1_Setting 1 ")) {
		t.Error("expected LFO1_Setting to be updated to 1")
	}

	// Verify Modulations_F1 Cutoff_LFO 1_Amount is correctly inserted
	if !bytes.Contains(rebuilt2, []byte(" 34 Modulations_F1 Cutoff_LFO 1_Amount 0.75 ")) {
		t.Error("expected Modulations_F1 Cutoff_LFO 1_Amount to be inserted with value 0.75")
	}
}
