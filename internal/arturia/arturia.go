package arturia

import (
	"bytes"
	"encoding/binary"
	_ "embed"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chanchal1987/pigment-patch-generator/internal/gemini"
)

//go:embed Default
var defaultTemplate []byte

//go:embed User.png
var bankPNG []byte

// WaveformMap translates the text selection from Gemini into the Pigments float position
var WaveformMap = map[string]float64{
	"Sine":     0.000000,
	"Triangle": 0.250000,
	"Square":   0.500000,
	"Saw":      0.750000,
}

// EngineTypeMap maps descriptive engine type strings to the Pigments v7 fractional ModuleType values.
// In Pigments 3.0 (version 7), engine types are encoded as fractions of 1/4 steps.
var EngineTypeMap = map[string]string{
	"Analog":   "0",
	"analog":   "0",
	"Wavetable": "0.25",
	"wavetable": "0.25",
	"Sample":   "0.5",
	"sample":   "0.5",
	"Granular": "0.5",
	"granular": "0.5",
	"Harmonic": "0.75",
	"harmonic": "0.75",
	// Legacy integer mappings from older Gemini prompts → convert to correct fractions
	"0": "0",       // Analog
	"1": "0.25",    // Wavetable  (was WRONG: "1" ≠ Wavetable in v7)
	"2": "0.5",     // Sample
	"3": "0.75",    // Harmonic
}

// FXTypeMap maps descriptive FX type strings to the Pigments fractional ModuleType values.
// These fractions are derived from factory preset analysis (1/13 step pattern in most presets).
var FXTypeMap = map[string]string{
	"Chorus":      "0.07692308",
	"chorus":      "0.07692308",
	"Distortion":  "0.15384616",
	"distortion":  "0.15384616",
	"Compressor":  "0.23076923",
	"compressor":  "0.23076923",
	"Flanger":     "0.30769232",
	"flanger":     "0.30769232",
	"Phaser":      "0.38461539",
	"phaser":      "0.38461539",
	"Delay":       "0.46153846",
	"delay":       "0.46153846",
	"TapeEcho":    "0.53846157",
	"tape_echo":   "0.53846157",
	"Reverb":      "0.61538464",
	"reverb":      "0.61538464",
	"Shimmer":     "0.69230771",
	"shimmer":     "0.69230771",
	"Filter":      "0.76923078",
	"filter":      "0.76923078",
	"SuperUnison": "0.84615386",
	"super_unison":"0.84615386",
	"Erosion":     "0.92307693",
	"erosion":     "0.92307693",
	"BitCrusher":  "1",
	"bitcrusher":  "1",
	"None":        "0",
	"none":        "0",
}

// FilterTypeMap maps descriptive filter type strings to v7 fractional ModuleType values.
var FilterTypeMap = map[string]string{
	"None":      "0",
	"none":      "0",
	"Multimode": "0.2",
	"multimode": "0.2",
	"Classic":   "0.2",   // Classic LP = Multimode
	"classic":   "0.2",
	"Ladder":    "0.14285715",
	"ladder":    "0.14285715",
	"SEM":       "0.2857143",
	"sem":       "0.2857143",
	"Formant":   "0.42857143",
	"formant":   "0.42857143",
	"Comb":      "0.5714286",
	"comb":      "0.5714286",
	"Phaser":    "0.71428573",
	"phaser":    "0.71428573",
}

// LFOWaveformMap maps friendly LFO waveform names to Pigments v7 fractions.
var LFOWaveformMap = map[string]string{
	"Sine":     "0",
	"sine":     "0",
	"Triangle": "0.33333331",
	"triangle": "0.33333331",
	"Saw":      "0.66666669",
	"saw":      "0.66666669",
	"Square":   "1",
	"square":   "1",
}

// fxSlotForLegacyID maps old "Fx_BusA_Reverb_Wet" style IDs to the correct FX slot
// and also returns the FX type to activate.
var fxSlotForLegacyID = map[string]struct {
	slot   int
	fxType string
}{
	"Fx_BusA_Reverb_Wet":     {1, "Reverb"},
	"Fx_BusA_Delay_Wet":      {2, "Delay"},
	"Fx_BusA_Chorus_Wet":     {3, "Chorus"},
	"Fx_Aux_Reverb_Wet":      {1, "Reverb"},
	"Fx_Aux_Delay_Wet":       {2, "Delay"},
	"Fx_BusB_Distortion_Wet": {4, "Distortion"},
	"Fx_BusB_Flanger_Wet":    {5, "Flanger"},
	"Fx_BusB_Phaser_Wet":     {6, "Phaser"},
}

// MapParamID translates internal/simplified parameter IDs to the actual Arturia Pigments parameter IDs
func MapParamID(id string) string {
	switch id {
	case "Engine1_Type":
		return "Engine1_ModuleType"
	case "Engine1_Wvt_Waveform":
		return "Engine1_WTOsc_FrameIndex"
	case "Filter1_Type":
		return "Filter1_ModuleType"
	case "Filter2_Type":
		return "Filter2_ModuleType"
	case "Engine2_Type":
		return "Engine2_ModuleType"
	case "Lfo1_Rate":
		return "LFO1_RateUnSynced"
	case "Lfo2_Rate":
		return "LFO2_RateUnSynced"
	case "Lfo3_Rate":
		return "LFO3_RateUnSynced"
	case "Lfo1_Waveform":
		return "LFO1_Waveform"
	case "Lfo2_Waveform":
		return "LFO2_Waveform"
	case "Lfo3_Waveform":
		return "LFO3_Waveform"
	case "Utility_Noise1_Volume":
		return "FilterMix_UtilityN1Volume"
	case "Utility_Noise2_Volume":
		return "FilterMix_UtilityN2Volume"
	case "Utility_Volume":
		return "FilterMix_UtilitySOVolume"
	case "Mod_Random1_Type":
		return "Random1_ModuleType"
	case "Mod_Random2_Type":
		return "Random2_ModuleType"
	case "Mod_Random3_Type":
		return "Random3_ModuleType"
	case "Mod_Random1_Rate":
		return "Random1_RnH_Distance"
	case "Mod_Random2_Rate":
		return "Random2_RnH_Distance"
	case "Mod_Random3_Rate":
		return "Random3_RnH_Distance"
	case "Function1_Rate":
		return "Function1_RateUnSynced"
	case "Function2_Rate":
		return "Function2_RateUnSynced"
	case "Function3_Rate":
		return "Function3_RateUnSynced"
	// FX slot direct mappings (new format)
	case "Fx1_Amount":
		return "FX1_Dry_Wet"
	case "Fx2_Amount":
		return "FX2_Dry_Wet"
	case "Fx3_Amount":
		return "FX3_Dry_Wet"
	case "Fx4_Amount":
		return "FX4_Dry_Wet"
	case "Fx5_Amount":
		return "FX5_Dry_Wet"
	case "Fx6_Amount":
		return "FX6_Dry_Wet"
	case "Fx1_Type":
		return "FX1_ModuleType"
	case "Fx2_Type":
		return "FX2_ModuleType"
	case "Fx3_Type":
		return "FX3_ModuleType"
	case "Fx4_Type":
		return "FX4_ModuleType"
	case "Fx5_Type":
		return "FX5_ModuleType"
	case "Fx6_Type":
		return "FX6_ModuleType"
	// Legacy FX bus mappings → Dry_Wet only (the FX type activation is handled separately)
	case "Fx_BusA_Reverb_Wet":
		return "FX1_Dry_Wet"
	case "Fx_BusA_Delay_Wet":
		return "FX2_Dry_Wet"
	case "Fx_BusA_Chorus_Wet":
		return "FX3_Dry_Wet"
	case "Fx_Aux_Reverb_Wet":
		return "FX1_Dry_Wet"
	case "Fx_Aux_Delay_Wet":
		return "FX2_Dry_Wet"
	case "Fx_BusB_Distortion_Wet":
		return "FX4_Dry_Wet"
	case "Fx_BusB_Flanger_Wet":
		return "FX5_Dry_Wet"
	case "Fx_BusB_Phaser_Wet":
		return "FX6_Dry_Wet"
	default:
		return id
	}
}

// resolveTypeValue looks up a type value string in the appropriate type map.
// If the value is already a valid float, it passes through.
// If it's a descriptive string like "Wavetable" or "Reverb", it gets mapped to the correct fraction.
func resolveTypeValue(paramID string, rawValue string) string {
	val := strings.TrimSpace(rawValue)

	switch {
	case paramID == "Engine1_ModuleType" || paramID == "Engine2_ModuleType":
		if mapped, ok := EngineTypeMap[val]; ok {
			return mapped
		}
	case strings.HasPrefix(paramID, "FX") && strings.HasSuffix(paramID, "_ModuleType"):
		if mapped, ok := FXTypeMap[val]; ok {
			return mapped
		}
	case paramID == "Filter1_ModuleType" || paramID == "Filter2_ModuleType":
		if mapped, ok := FilterTypeMap[val]; ok {
			return mapped
		}
	case strings.HasPrefix(paramID, "LFO") && strings.HasSuffix(paramID, "_Waveform"):
		if mapped, ok := LFOWaveformMap[val]; ok {
			return mapped
		}
	}
	return val
}

// GeneratePGTX generates the raw .pgtx ZIP archive containing the Boost serialized preset
func GeneratePGTX(res *gemini.AnalysisResult) ([]byte, error) {
	name := res.PatchName
	if name == "" {
		name = "Unnamed Patch"
	}
	bank := "User"
	author := "Gemini Agent"

	// 1. Copy the embedded template bytes to modify
	templateBytes := make([]byte, len(defaultTemplate))
	copy(templateBytes, defaultTemplate)

	// 2. Metadata replacements in bytes
	// Header: "7 7 Default 7 Factory 22 7 Arturia"
	oldHeader := []byte("7 7 Default 7 Factory 22 7 Arturia")
	newHeader := []byte(fmt.Sprintf("7 %d %s %d %s 22 %d %s", len(name), name, len(bank), bank, len(author), author))
	if !bytes.Contains(templateBytes, oldHeader) {
		return nil, fmt.Errorf("embedded template header not found")
	}
	templateBytes = bytes.Replace(templateBytes, oldHeader, newHeader, 1)

	// OriginalPresetName: "18 OriginalPresetName 18 Default Pigments 7"
	oldOrigName := []byte("18 OriginalPresetName 18 Default Pigments 7")
	newOrigName := []byte(fmt.Sprintf("18 OriginalPresetName %d %s", len(name), name))
	if !bytes.Contains(templateBytes, oldOrigName) {
		return nil, fmt.Errorf("embedded template original preset name field not found")
	}
	templateBytes = bytes.Replace(templateBytes, oldOrigName, newOrigName, 1)

	// 64-character padded blocks (FX1-FX6 Corrosion)
	oldPadded := make([]byte, 64)
	copy(oldPadded, []byte("Default")) // Trailing bytes are automatically 0

	newPadded := make([]byte, 64)
	copy(newPadded, []byte(name))

	templateBytes = bytes.Replace(templateBytes, oldPadded, newPadded, -1)

	// 4. Macro name replacements (custom labels on UI)
	oldMacro1 := []byte(" 11 Macro1_Name 16 Macro 1\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	newMacro1 := []byte(" 11 Macro1_Name 16 Bright\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	templateBytes = bytes.Replace(templateBytes, oldMacro1, newMacro1, 1)

	oldMacro2 := []byte(" 11 Macro2_Name 16 Macro 2\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	newMacro2 := []byte(" 11 Macro2_Name 16 Timbre\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	templateBytes = bytes.Replace(templateBytes, oldMacro2, newMacro2, 1)

	oldMacro3 := []byte(" 11 Macro3_Name 16 Macro 3\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	newMacro3 := []byte(" 11 Macro3_Name 16 Time\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	templateBytes = bytes.Replace(templateBytes, oldMacro3, newMacro3, 1)

	oldMacro4 := []byte(" 11 Macro4_Name 16 Macro 4\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	newMacro4 := []byte(" 11 Macro4_Name 16 Movement\x00\x00\x00\x00\x00\x00\x00\x00")
	templateBytes = bytes.Replace(templateBytes, oldMacro4, newMacro4, 1)

	// 3. Parameter compilation
	wvtVal, exists := WaveformMap[res.Osc1Waveform]
	if !exists {
		wvtVal = 0.250000
	}

	paramsMap := map[string]string{
		// Engine 1: Wavetable = 0.25 (correct v7 fractional value)
		"Engine1_ModuleType":       "0.25",
		"Engine1_WTOsc_FrameIndex": fmt.Sprintf("%.6f", wvtVal),
		// Filter 1: Multimode = 0.2 (correct v7 fractional value)
		"Filter1_ModuleType": "0.2",
		"Filter1_Cutoff":     fmt.Sprintf("%.6f", res.Filter1Cutoff),
		"Filter1_Resonance":  fmt.Sprintf("%.6f", res.Filter1Resonance),
		// Envelope 1
		"Env1_Attack":  fmt.Sprintf("%.6f", res.Env1Attack),
		"Env1_Decay":   fmt.Sprintf("%.6f", res.Env1Decay),
		"Env1_Sustain":  fmt.Sprintf("%.6f", res.Env1Sustain),
		"Env1_Release":  fmt.Sprintf("%.6f", res.Env1Release),
	}

	// Track which FX slots need their ModuleType activated
	fxTypeActivations := map[string]string{} // e.g. "FX1_ModuleType" -> "0.61538464"

	for _, p := range res.AdditionalParameters {
		id := strings.TrimSpace(p.ID)
		if id == "" {
			continue
		}

		// Check if this is a legacy FX bus parameter that needs automatic FX type activation
		if slot, ok := fxSlotForLegacyID[id]; ok {
			fxModuleTypeKey := fmt.Sprintf("FX%d_ModuleType", slot.slot)
			if fxTypeVal, ok2 := FXTypeMap[slot.fxType]; ok2 {
				fxTypeActivations[fxModuleTypeKey] = fxTypeVal
			}
		}

		// Check if this is a new-style Fx_Type parameter
		if strings.HasSuffix(id, "_Type") && strings.HasPrefix(id, "Fx") {
			mappedID := MapParamID(id)
			val := resolveTypeValue(mappedID, p.Value)
			paramsMap[mappedID] = val
			continue
		}

		mappedID := MapParamID(id)

		// Resolve type values through the type maps
		val := strings.TrimSpace(p.Value)
		if strings.HasSuffix(mappedID, "_ModuleType") {
			val = resolveTypeValue(mappedID, val)
		} else {
			// Standard float/int value formatting
			var valFloat float64
			if _, err := fmt.Sscanf(val, "%f", &valFloat); err == nil {
				if strings.HasSuffix(mappedID, "_ModuleType") {
					val = fmt.Sprintf("%.0f", valFloat)
				} else {
					val = fmt.Sprintf("%.6f", valFloat)
				}
			}
		}

		paramsMap[mappedID] = val
	}

	// Apply automatic FX type activations (only if not already explicitly set)
	for fxKey, fxVal := range fxTypeActivations {
		if _, alreadySet := paramsMap[fxKey]; !alreadySet {
			paramsMap[fxKey] = fxVal
		}
	}

	// Auto-enable LFO settings if they are used/modulated
	for k := range paramsMap {
		if strings.Contains(k, "LFO1") || strings.Contains(k, "LFO 1") {
			paramsMap["LFO1_Setting"] = "1"
		}
		if strings.Contains(k, "LFO2") || strings.Contains(k, "LFO 2") {
			paramsMap["LFO2_Setting"] = "1"
		}
		if strings.Contains(k, "LFO3") || strings.Contains(k, "LFO 3") {
			paramsMap["LFO3_Setting"] = "1"
		}
	}

	// Rebuild the template parameters segment dynamically
	rebuiltTemplate, err := RebuildParams(templateBytes, paramsMap)
	if err != nil {
		return nil, fmt.Errorf("failed to rebuild preset parameters: %w", err)
	}
	templateBytes = rebuiltTemplate


	// 4. Create manual ZIP archive
	var zipBuf bytes.Buffer

	// File entries
	innerFilePath := fmt.Sprintf("Pigments/User/%s/%s", bank, name)
	files := []struct {
		name string
		data []byte
	}{
		{name: innerFilePath, data: templateBytes},
		{name: fmt.Sprintf("%s.png", bank), data: bankPNG},
	}

	offsets := make([]int, len(files))
	crcs := make([]uint32, len(files))

	// Write Local File Headers and file data
	for i, f := range files {
		offsets[i] = zipBuf.Len()
		crcs[i] = crc32.ChecksumIEEE(f.data)

		// LFH Signature
		zipBuf.Write([]byte{0x50, 0x4b, 0x03, 0x04})
		// Version needed to extract (2.0 = 20)
		binary.Write(&zipBuf, binary.LittleEndian, uint16(20))
		// General purpose bit flag (0)
		binary.Write(&zipBuf, binary.LittleEndian, uint16(0))
		// Compression method (0 = Store)
		binary.Write(&zipBuf, binary.LittleEndian, uint16(0))
		// Last mod file time and date (0)
		binary.Write(&zipBuf, binary.LittleEndian, uint16(0))
		binary.Write(&zipBuf, binary.LittleEndian, uint16(0))
		// CRC-32
		binary.Write(&zipBuf, binary.LittleEndian, crcs[i])
		// Compressed size
		binary.Write(&zipBuf, binary.LittleEndian, uint32(len(f.data)))
		// Uncompressed size
		binary.Write(&zipBuf, binary.LittleEndian, uint32(len(f.data)))
		// File name length
		binary.Write(&zipBuf, binary.LittleEndian, uint16(len(f.name)))
		// Extra field length (0)
		binary.Write(&zipBuf, binary.LittleEndian, uint16(0))
		// File name
		zipBuf.Write([]byte(f.name))
		// File data
		zipBuf.Write(f.data)
	}

	cdStart := zipBuf.Len()

	// Write Central Directory Headers
	for i, f := range files {
		// CDFH Signature
		zipBuf.Write([]byte{0x50, 0x4b, 0x01, 0x02})
		// Version made by: OS system 0 (FAT), Version 2.0 = 20 (0x0014)
		binary.Write(&zipBuf, binary.LittleEndian, uint16(20))
		// Version needed to extract (2.0 = 20)
		binary.Write(&zipBuf, binary.LittleEndian, uint16(20))
		// General purpose bit flag (0)
		binary.Write(&zipBuf, binary.LittleEndian, uint16(0))
		// Compression method (0 = Store)
		binary.Write(&zipBuf, binary.LittleEndian, uint16(0))
		// Last mod file time and date (0)
		binary.Write(&zipBuf, binary.LittleEndian, uint16(0))
		binary.Write(&zipBuf, binary.LittleEndian, uint16(0))
		// CRC-32
		binary.Write(&zipBuf, binary.LittleEndian, crcs[i])
		// Compressed size
		binary.Write(&zipBuf, binary.LittleEndian, uint32(len(f.data)))
		// Uncompressed size
		binary.Write(&zipBuf, binary.LittleEndian, uint32(len(f.data)))
		// File name length
		binary.Write(&zipBuf, binary.LittleEndian, uint16(len(f.name)))
		// Extra field length (0)
		binary.Write(&zipBuf, binary.LittleEndian, uint16(0))
		// File comment length (0)
		binary.Write(&zipBuf, binary.LittleEndian, uint16(0))
		// Disk number start (0)
		binary.Write(&zipBuf, binary.LittleEndian, uint16(0))
		// Internal file attributes (0)
		binary.Write(&zipBuf, binary.LittleEndian, uint16(0))
		// External file attributes (0)
		binary.Write(&zipBuf, binary.LittleEndian, uint32(0))
		// Local header offset
		binary.Write(&zipBuf, binary.LittleEndian, uint32(offsets[i]))
		// File name
		zipBuf.Write([]byte(f.name))
	}

	cdEnd := zipBuf.Len()
	cdSize := cdEnd - cdStart

	// Write End of Central Directory (EOCD)
	zipBuf.Write([]byte{0x50, 0x4b, 0x05, 0x06})
	// Number of this disk (0)
	binary.Write(&zipBuf, binary.LittleEndian, uint16(0))
	// Disk where CD starts (0)
	binary.Write(&zipBuf, binary.LittleEndian, uint16(0))
	// Number of CD records on this disk
	binary.Write(&zipBuf, binary.LittleEndian, uint16(len(files)))
	// Total number of CD records
	binary.Write(&zipBuf, binary.LittleEndian, uint16(len(files)))
	// Size of Central Directory
	binary.Write(&zipBuf, binary.LittleEndian, uint32(cdSize))
	// Offset of CD start
	binary.Write(&zipBuf, binary.LittleEndian, uint32(cdStart))
	// Comment length (0)
	binary.Write(&zipBuf, binary.LittleEndian, uint16(0))

	return zipBuf.Bytes(), nil
}

// SavePGTX writes the raw ZIP archive bytes to the .pgtx file
func SavePGTX(pgtxData []byte, destPath string) error {
	// Create destination directory if needed
	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory for PGTX: %w", err)
	}

	return os.WriteFile(destPath, pgtxData, 0644)
}


// formatMarkdownParam formats value and percentage for display
func formatMarkdownParam(id string, valStr string) (string, string) {
	var valFloat float64
	if _, err := fmt.Sscanf(valStr, "%f", &valFloat); err == nil {
		if strings.HasSuffix(id, "_Type") || id == "Engine1_Type" || id == "Engine2_Type" {
			return fmt.Sprintf("%.0f", valFloat), "N/A"
		}
		if valFloat >= 0.0 && valFloat <= 1.0 {
			return fmt.Sprintf("%.6f", valFloat), fmt.Sprintf("%.1f%%", valFloat*100.0)
		}
		return valStr, "N/A"
	}
	return valStr, "N/A"
}

// GenerateMarkdown creates the human-readable manual recreation guide
func GenerateMarkdown(res *gemini.AnalysisResult) string {
	wvtVal, exists := WaveformMap[res.Osc1Waveform]
	if !exists {
		wvtVal = 0.250000
	}

	md := fmt.Sprintf(`# Arturia Pigments Manual Recreation Guide - %s

This guide provides the exact parameter values to recreate the **%s** patch manually in Arturia Pigments. Use this as a backup if you lose your '.pgtx' preset file.

---

## 1. Oscillator Engine Settings
*   **Engine 1 Type**: **Wavetable** (Value: '0.25')
*   **Wavetable Waveform**: **%s** (Value: '%.6f' / **%.1f%%** of range)
    *   *Reference Positions:*
        *   Sine: '0.000000' (0%%)
        *   Triangle: '0.250000' (25%%)
        *   Square: '0.500000' (50%%)
        *   Saw: '0.750000' (75%%)

## 2. Filter 1 Settings
*   **Filter 1 Type**: **Multimode** (Value: '0.2')
*   **Filter 1 Cutoff**: '%.6f' (**%.1f%%**)
*   **Filter 1 Resonance**: '%.6f' (**%.1f%%**)

## 3. Envelope 1 Settings (Amplitude)
*   **Env 1 Attack**: '%.6f' (**%.1f%%**)
*   **Env 1 Decay**: '%.6f' (**%.1f%%**)
*   **Env 1 Sustain**: '%.6f' (**%.1f%%**)
*   **Env 1 Release**: '%.6f' (**%.1f%%**)
`,
		res.PatchName,
		res.PatchName,
		res.Osc1Waveform,
		wvtVal,
		wvtVal*100.0,
		res.Filter1Cutoff,
		res.Filter1Cutoff*100.0,
		res.Filter1Resonance,
		res.Filter1Resonance*100.0,
		res.Env1Attack,
		res.Env1Attack*100.0,
		res.Env1Decay,
		res.Env1Decay*100.0,
		res.Env1Sustain,
		res.Env1Sustain*100.0,
		res.Env1Release,
		res.Env1Release*100.0,
	)

	if len(res.AdditionalParameters) > 0 {
		md += "\n## 4. Advanced Modulations, Utilities & FX\n"
		md += "This patch uses additional parameters to model complex textures, motion, and space. Recreate them using the values and routing reasons below:\n\n"
		md += "| Parameter ID | Value | Percentage | Sound Design Function / Rationale |\n"
		md += "| :--- | :--- | :--- | :--- |\n"
		for _, p := range res.AdditionalParameters {
			id := strings.TrimSpace(p.ID)
			if id == "" {
				continue
			}
			valFormatted, percentFormatted := formatMarkdownParam(id, p.Value)
			md += fmt.Sprintf("| **%s** | `%s` | %s | %s |\n", id, valFormatted, percentFormatted, p.Description)
		}
	}

	md += "\n---\n*Created via Gemini Multi-modal Audio Analysis*\n"
	return md
}

// SaveMarkdown saves the manual recreation guide to a Markdown file
func SaveMarkdown(mdContent string, destPath string) error {
	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory for Markdown: %w", err)
	}

	return os.WriteFile(destPath, []byte(mdContent), 0644)
}

// RebuildParams parses the parameter map from templateBytes, merges updates/additions,
// sorts all parameters alphabetically, and rebuilds the serialized block with an updated count.
func RebuildParams(templateBytes []byte, newParams map[string]string) ([]byte, error) {
	// 1. Locate start marker (first parameter)
	firstParam := "36 AfterTouchCurve_LastActivePointIndex"
	idxFirst := bytes.Index(templateBytes, []byte(firstParam))
	if idxFirst == -1 {
		return nil, fmt.Errorf("first parameter marker %q not found in template", firstParam)
	}

	// 2. Find count prefix ' 0 0 0 ' backwards from idxFirst
	idxSuffix := bytes.LastIndex(templateBytes[:idxFirst], []byte(" 0 0 0 "))
	if idxSuffix == -1 {
		return nil, fmt.Errorf("class metadata suffix not found")
	}

	// Find space before idxSuffix
	idxSpace := bytes.LastIndex(templateBytes[:idxSuffix], []byte(" "))
	if idxSpace == -1 {
		return nil, fmt.Errorf("class metadata prefix space not found")
	}

	// 3. Locate last parameter '16 Voice_Send_Level'
	lastParam := "16 Voice_Send_Level"
	idxLast := bytes.Index(templateBytes, []byte(lastParam))
	if idxLast == -1 {
		return nil, fmt.Errorf("last parameter marker %q not found in template", lastParam)
	}

	// Find the first space after Voice_Send_Level's value segment
	idxValEnd := bytes.IndexByte(templateBytes[idxLast+len(lastParam)+1:], ' ')
	if idxValEnd == -1 {
		return nil, fmt.Errorf("value termination space for last parameter not found")
	}
	idxParamsEnd := idxLast + len(lastParam) + 1 + idxValEnd

	// 4. Parse the parameters segment
	paramsSegment := templateBytes[idxFirst : idxParamsEnd+1]
	paramsMap := make(map[string]string)

	pos := 0
	for pos < len(paramsSegment) {
		spaceIdx := bytes.IndexByte(paramsSegment[pos:], ' ')
		if spaceIdx == -1 {
			break
		}
		lenStr := string(paramsSegment[pos : pos+spaceIdx])

		var length int
		if _, err := fmt.Sscanf(lenStr, "%d", &length); err != nil {
			return nil, fmt.Errorf("failed to parse parameter length at pos %d: %v", pos, err)
		}

		idStart := pos + spaceIdx + 1
		idEnd := idStart + length
		if idEnd > len(paramsSegment) {
			return nil, fmt.Errorf("out of bounds parsing parameter ID")
		}
		paramID := string(paramsSegment[idStart:idEnd])

		valStart := idEnd + 1
		valEnd := bytes.IndexByte(paramsSegment[valStart:], ' ')
		var val string
		if valEnd == -1 {
			val = string(paramsSegment[valStart:])
			pos = len(paramsSegment)
		} else {
			valEnd += valStart
			val = string(paramsSegment[valStart:valEnd])
			pos = valEnd + 1
		}

		paramsMap[paramID] = val
	}

	// 5. Merge new updates
	for k, v := range newParams {
		paramsMap[k] = v
	}

	// 6. Sort parameters alphabetically
	var sortedKeys []string
	for k := range paramsMap {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	// 7. Rebuild parameter segment
	var rebuiltBuf bytes.Buffer
	for _, k := range sortedKeys {
		rebuiltBuf.WriteString(fmt.Sprintf("%d %s %s ", len(k), k, paramsMap[k]))
	}

	// 8. Rebuild final bytes
	var finalBytes bytes.Buffer

	// Everything before the count: templateBytes[:idxSpace+1]
	finalBytes.Write(templateBytes[:idxSpace+1])

	// New count: len(paramsMap)
	finalBytes.WriteString(fmt.Sprintf("%d", len(paramsMap)))

	// Count suffix: " 0 0 0 "
	finalBytes.WriteString(" 0 0 0 ")

	// Parameters list: rebuiltBuf
	finalBytes.Write(rebuiltBuf.Bytes())

	// Everything after parameters segment: templateBytes[idxParamsEnd+1:]
	finalBytes.Write(templateBytes[idxParamsEnd+1:])

	return finalBytes.Bytes(), nil
}
