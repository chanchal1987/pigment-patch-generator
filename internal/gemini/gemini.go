package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"google.golang.org/genai"
)

type AdditionalParam struct {
	ID          string `json:"id"`
	Value       string `json:"value"`
	Description string `json:"description"`
}

// AnalysisResult holds the structured parameters from Gemini
type AnalysisResult struct {
	PatchName            string            `json:"patch_name"`
	Osc1Waveform         string            `json:"osc1_waveform"`
	Filter1Cutoff        float64           `json:"filter1_cutoff"`
	Filter1Resonance     float64           `json:"filter1_resonance"`
	Env1Attack           float64           `json:"env1_attack"`
	Env1Decay            float64           `json:"env1_decay"`
	Env1Sustain          float64           `json:"env1_sustain"`
	Env1Release          float64           `json:"env1_release"`
	AdditionalParameters []AdditionalParam `json:"additional_parameters"`
}

// DefaultModel is the fallback model if GEMINI_MODEL is not set
const DefaultModel = "gemini-3.5-flash"

// AnalyzeAudio uploads an audio file to Gemini, runs deconstruction with a structured JSON schema, and returns parameters
func AnalyzeAudio(ctx context.Context, filePath string, mimeType string, modelName string) (*AnalysisResult, error) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY environment variable is not set")
	}

	// Determine model to use
	if modelName == "" {
		modelName = os.Getenv("GEMINI_MODEL")
		if modelName == "" {
			modelName = DefaultModel
		}
	}

	// Initialize the Google GenAI Client
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize GenAI client: %w", err)
	}

	// 1. Upload audio file via Files API
	uploadConfig := &genai.UploadFileConfig{
		DisplayName: "Input Audio for Pigments Preset",
		MIMEType:    mimeType,
	}
	uploadedFile, err := client.Files.UploadFromPath(ctx, filePath, uploadConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to upload audio file to Gemini: %w", err)
	}

	// Defer file deletion from Gemini servers
	defer func() {
		_, _ = client.Files.Delete(ctx, uploadedFile.Name, nil)
	}()

	// 2. Define the response schema matching the requested parameters
	responseSchema := &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"patch_name": {
				Type:        genai.TypeString,
				Description: "A creative, short, descriptive name for the synthesizer patch reflecting its sonic character (e.g. 'Warm Analog Bass', 'Cyberpunk Lead', 'Lofi Ambient Pad'), maximum 3-4 words",
			},
			"osc1_waveform": {
				Type:        genai.TypeString,
				Enum:        []string{"Saw", "Square", "Triangle", "Sine"},
				Description: "Waveform selection for oscillator 1",
			},
			"filter1_cutoff": {
				Type:        genai.TypeNumber,
				Description: "Filter 1 cutoff frequency, normalized between 0.0 and 1.0",
			},
			"filter1_resonance": {
				Type:        genai.TypeNumber,
				Description: "Filter 1 resonance value, normalized between 0.0 and 1.0",
			},
			"env1_attack": {
				Type:        genai.TypeNumber,
				Description: "Envelope 1 attack time, normalized between 0.0 and 1.0",
			},
			"env1_decay": {
				Type:        genai.TypeNumber,
				Description: "Envelope 1 decay time, normalized between 0.0 and 1.0",
			},
			"env1_sustain": {
				Type:        genai.TypeNumber,
				Description: "Envelope 1 sustain level, normalized between 0.0 and 1.0",
			},
			"env1_release": {
				Type:        genai.TypeNumber,
				Description: "Envelope 1 release time, normalized between 0.0 and 1.0",
			},
			"additional_parameters": {
				Type:        genai.TypeArray,
				Description: "Specify additional parameters to recreate complex timbres, LFO modulations, secondary envelopes, sub-oscillators, noise, or FX chains if the sound has rich texture, movement, or space. Use standard Pigments parameter IDs.",
				Items: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"id": {
							Type:        genai.TypeString,
							Description: `The parameter ID. Use these exact IDs:
ENGINE 2: "Engine2_Type" (value: "Analog"/"Wavetable"/"Sample"/"Harmonic"), "Engine2_Wvt_Waveform" (0.0-1.0), "Engine2_Volume" (0.0-1.0).
UTILITY: "Utility_Volume" (0.0-1.0), "Utility_Noise1_Volume"/"Utility_Noise2_Volume" (0.0-1.0), "Utility_Noise1_Type"/"Utility_Noise2_Type" (0.0-1.0).
MODULATION SETTINGS: "Lfo1_Rate"/"Lfo2_Rate"/"Lfo3_Rate" (0.0-1.0), "Env2_Attack"/"Env2_Decay"/"Env2_Sustain"/"Env2_Release" (0.0-1.0), "Function1_Rate"/"Function2_Rate"/"Function3_Rate" (0.0-1.0), "Mod_Random1_Rate"/"Mod_Random1_Type" (0.0-1.0).
MODULATION ROUTINGS (Crucial for vibrato, tremolo, filter sweeps): Use the format "Modulations_<Target>_<Source>_Amount" (value 0.0-1.0, where 0.5 is no modulation, 0.7-0.9 is strong positive, 0.1-0.3 is strong negative).
  Common Targets: "F1 Cutoff", "F2 Cutoff", "F1 Resonance", "F2 Resonance", "WT1 FrameIndex", "WT2 FrameIndex", "Engine1 Volume", "Engine2 Volume", "WT1 PhaseDist", "WT2 PhaseDist".
  Common Sources: "LFO 1", "LFO 2", "LFO 3", "Env 2", "Env 3", "Func 1", "Func 2", "Func 3".
  Examples: "Modulations_F1 Cutoff_LFO 1_Amount" (routes LFO 1 to Filter 1 Cutoff), "Modulations_WT1 FrameIndex_LFO 1_Amount" (routes LFO 1 to Wavetable 1 Frame), "Modulations_F1 Cutoff_Env 2_Amount" (routes Env 2 to Cutoff).
FILTERS: "Filter2_Cutoff"/"Filter2_Resonance" (0.0-1.0).
FX SLOTS (use paired Type+Amount for each slot):
  "Fx1_Type" (value: "Reverb"/"Delay"/"Chorus"/"Distortion"/"Phaser"/"Flanger"/"Compressor"/"Shimmer"/"None"), "Fx1_Amount" (0.0-1.0).
  "Fx2_Type"/"Fx2_Amount", "Fx3_Type"/"Fx3_Amount" (same format).
LEGACY FX: "Fx_BusA_Reverb_Wet", "Fx_BusA_Delay_Wet", "Fx_BusA_Chorus_Wet", "Fx_BusB_Distortion_Wet", "Fx_BusB_Flanger_Wet" (0.0-1.0).`,
						},
						"value": {
							Type:        genai.TypeString,
							Description: "The parameter value as a string. For continuous knobs: '%.6f' precision floats (0.0-1.0). For type selectors: use descriptive strings ('Wavetable', 'Reverb', 'Delay', etc.).",
						},
						"description": {
							Type:        genai.TypeString,
							Description: "A brief reason why this parameter is used to replicate the sound texture",
						},
					},
					Required: []string{"id", "value", "description"},
				},
			},
		},
		Required: []string{
			"patch_name",
			"osc1_waveform",
			"filter1_cutoff",
			"filter1_resonance",
			"env1_attack",
			"env1_decay",
			"env1_sustain",
			"env1_release",
		},
	}

	// 3. Construct System Instruction and Parts
	systemInstruction := `You are an expert sound designer for the Arturia Pigments synthesizer. 
Analyze the spectral, harmonic, and temporal characteristics of the provided audio file. 
Translate these characteristics into the exact synthesis parameters of the Arturia Pigments architecture:

1. WAVEFORM SELECTION (osc1_waveform):
   - Choose "Sine" if the sound is extremely pure, warm, and sub-heavy with no high-frequency harmonics (e.g. sub-bass, clean whistle).
   - Choose "Triangle" if the sound is soft, flute-like, or a warm woodwind, containing only odd harmonics that roll off quickly.
   - Choose "Square" if the sound is hollow, woody, nasal, or resembles a clarinet, chiptune lead, or classic hollow bass.
   - Choose "Saw" if the sound is rich, bright, buzzy, brassy, harsh, or highly textured with a full spectrum of odd and even harmonics (e.g. synth brass, strings, aggressive leads, supersaws, complex textures).

2. FILTER 1 CUTOFF (filter1_cutoff):
   - Evaluate the overall brightness and high-frequency content of the sound.
   - For dark, muffled sounds: set cutoff low (0.0 to 0.4).
   - For moderate, warm sounds: set cutoff mid-range (0.4 to 0.7).
   - For bright, sharp, or buzzy sounds: set cutoff high (0.7 to 1.0).

3. FILTER 1 RESONANCE (filter1_resonance):
   - Listen for narrow spectral peaks, whistling, accentuation of the cutoff frequency, or nasal/ringing textures.
   - High resonance (0.5 to 1.0) should be used if the sound rings, squeals, or has a distinct acid/resonance sweep.
   - Low resonance (0.0 to 0.3) should be used if the filter transition is smooth, flat, or lacks accentuation.

4. AMPLITUDE ENVELOPE (env1_attack, env1_decay, env1_sustain, env1_release):
   - ATTACK (env1_attack): How fast does the sound reach its peak volume? 
     * Immediate/percussive click/pluck: set attack very low (0.0 to 0.05).
     * Soft brass/bowed string: set attack medium (0.1 to 0.3).
     * Slow swelling pad: set attack high (0.4 to 1.0).
   - DECAY & SUSTAIN (env1_decay, env1_sustain): How does the volume behave after the initial strike?
     * Plucked/struck sound (bell, pluck, percussion): Sustain should be low (0.0 to 0.2), and Decay should dictate the fade time (0.1 to 0.5).
     * Sustained sound (organ, lead, pad): Sustain should be high (0.7 to 1.0), and Decay should be moderate.
   - RELEASE (env1_release): How long does the sound linger after note release?
     * Dampened/staccato: set release very low (0.0 to 0.1).
     * Resonant room/long tail (reverb-like tail, pad release): set release high (0.4 to 1.0).

5. COMPLEX TIMBRES, UTILITIES, MODULATIONS & EFFECTS (additional_parameters):
   If the sound is highly textured, rich, modulated, has noise, sub-bass, or has space/reverb, you MUST specify additional parameters:
   
   - ENGINE 2 (Choose the engine that complements Engine 1):
     * "Engine2_Type": Use descriptive value: "Wavetable", "Sample", "Analog", or "Harmonic".
     * "Engine2_Wvt_Waveform": Set position if Wavetable (0.0 to 1.0).
     * "Engine2_Volume": Set volume level via "FilterMix_Engine2Volume" (0.0 to 1.0).
   
   - UTILITY ENGINE (Add sub-bass or texture noise):
     * "Utility_Volume": Sub-oscillator level (0.0 to 1.0) for low-end body.
     * "Utility_Noise1_Volume" / "Utility_Noise2_Volume": Noise source levels (0.0 to 1.0).
     * "Utility_Noise1_Type" / "Utility_Noise2_Type": Noise color selections (0.0 to 1.0).
    - MODULATION SOURCES & ROUTINGS (Crucial for movement, vibrato, tremolo, filter sweeps):
      If the sound has vibrato, pitch sweeps, cutoff modulation, or movement, you MUST set both the source rate/settings AND the routing amount:
      * LFO rates: "Lfo1_Rate", "Lfo2_Rate", "Lfo3_Rate" (0.0 to 1.0).
      * Envelope 2 (for modulation sweeps): "Env2_Attack", "Env2_Decay", "Env2_Sustain", "Env2_Release" (0.0 to 1.0).
      * Functions (complex custom LFO curves): "Function1_Rate", "Function2_Rate", "Function3_Rate" (0.0 to 1.0).
      * Routing connections: You MUST use "Modulations_<Target>_<Source>_Amount" (0.0 to 1.0, where 0.5 is no modulation, 0.7-0.9 is strong positive, 0.1-0.3 is strong negative).
        Targets: "F1 Cutoff" (Filter 1 Cutoff), "F2 Cutoff" (Filter 2 Cutoff), "F1 Resonance", "F2 Resonance", "WT1 FrameIndex" (Wavetable 1 Position), "WT2 FrameIndex" (Wavetable 2 Position), "Engine1 Volume" (Engine 1 Level), "Engine2 Volume" (Engine 2 Level), "WT1 PhaseDist" (Phase Distortion).
        Sources: "LFO 1", "LFO 2", "LFO 3", "Env 2", "Env 3", "Func 1", "Func 2", "Func 3", "Macro 1", "Macro 2", "Macro 3", "Macro 4".
        Example: If you hear a filter cutoff sweeping up and down cyclically at a fast rate: set "Lfo1_Rate" to 0.6, and add ID "Modulations_F1 Cutoff_LFO 1_Amount" with value 0.75.
        Example: If you hear a pitch or filter sweep on note strike: set "Env2_Attack" to 0.2, "Env2_Decay" to 0.4, and add ID "Modulations_F1 Cutoff_Env 2_Amount" with value 0.8.

    - ARTURIA 4 MACROS (You MUST assign these to shape the sound, personalized for each specific patch):
      * Macro 1 (Brightness): Route to parameters controlling brightness. E.g. "Modulations_F1 Cutoff_Macro 1_Amount" (value 0.70-0.85 for positive sweep).
      * Macro 2 (Timbre): Route to parameters shaping core timbre or wavetable morphing. E.g. "Modulations_WT1 FrameIndex_Macro 2_Amount" (value 0.70-0.85) or "Modulations_WT1 PhaseDist_Macro 2_Amount" (value 0.70-0.85).
      * Macro 3 (Time): Route to envelope parameters (decay, release) to shape speed/length of the sound. E.g. "Modulations_Env1 Release_Macro 3_Amount" (value 0.70-0.85) or "Modulations_Env1 Decay_Macro 3_Amount" (value 0.70-0.85).
      * Macro 4 (Movement/Space): Route to effects mix levels or modulation speeds. E.g. "Modulations_FX1 Dry/Wet_Macro 4_Amount" (value 0.70-0.80) or "Modulations_FX2 Dry/Wet_Macro 4_Amount" (value 0.70-0.80).
   
   - FILTERS:
     * "Filter2_Cutoff" / "Filter2_Resonance" (0.0 to 1.0) if a secondary filter is needed.
   
   - EFFECTS (FX Slots 1-3): Each slot needs BOTH a type and an amount.
     * "Fx1_Type" with value "Reverb", "Delay", "Chorus", "Distortion", "Phaser", "Flanger", "Compressor", "Shimmer", or "None".
     * "Fx1_Amount" with value 0.0 to 1.0 (dry/wet mix).
     * Similarly for "Fx2_Type"/"Fx2_Amount" and "Fx3_Type"/"Fx3_Amount".
     * Use Reverb for spatial room/hall reflections, Delay for echoes, Chorus for wide/lush/detuned tones,
       Distortion for warmth/grit, Phaser/Flanger for comb-filtering sweeps, Compressor for punch.

Output only the requested JSON matching the schema. Be precise, expressive, and try to capture the authentic vibe of the sound.`

	parts := []*genai.Part{
		{Text: "Listen to this audio sample. Deconstruct its timbre, envelope, and frequency characteristics. Map them accurately and expressively to the Pigments synthesizer parameters schema, prioritizing high fidelity replication of the sound's perceived brightness, resonance, and volume changes over time."},
		{
			FileData: &genai.FileData{
				FileURI:  uploadedFile.URI,
				MIMEType: uploadedFile.MIMEType,
			},
		},
	}

	// 4. Generate structured content
	config := &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{
				{Text: systemInstruction},
			},
		},
		ResponseMIMEType:  "application/json",
		ResponseSchema:    responseSchema,
		Temperature:       genai.Ptr(float32(0.1)), // Low temperature for deterministic/factual parameter mapping
	}

	resp, err := client.Models.GenerateContent(ctx, modelName, []*genai.Content{{Parts: parts}}, config)
	if err != nil {
		return nil, fmt.Errorf("failed to generate structured content from Gemini: %w", err)
	}

	// 5. Unmarshal structured response
	text := resp.Text()
	if text == "" {
		return nil, fmt.Errorf("model returned empty response text")
	}

	var result AnalysisResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return nil, fmt.Errorf("failed to parse structured JSON response: %w. Raw text: %s", err, text)
	}

	// Ensure boundary safety of returned parameters
	result.Filter1Cutoff = clamp(result.Filter1Cutoff, 0.0, 1.0)
	result.Filter1Resonance = clamp(result.Filter1Resonance, 0.0, 1.0)
	result.Env1Attack = clamp(result.Env1Attack, 0.0, 1.0)
	result.Env1Decay = clamp(result.Env1Decay, 0.0, 1.0)
	result.Env1Sustain = clamp(result.Env1Sustain, 0.0, 1.0)
	result.Env1Release = clamp(result.Env1Release, 0.0, 1.0)

	return &result, nil
}

func clamp(val, min, max float64) float64 {
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}

// ModelInfo holds information about a Gemini model
type ModelInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
}

// ListModels queries the Gemini API for available models
func ListModels(ctx context.Context) ([]ModelInfo, error) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY environment variable is not set")
	}

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize GenAI client: %w", err)
	}

	resp, err := client.Models.List(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list models: %w", err)
	}

	var models []ModelInfo
	for _, m := range resp.Items {
		// Filter for active gemini models, excluding embedding/tuning/utility models
		name := m.Name
		// If name has prefix "models/", strip it for cleaner select values
		cleanName := strings.TrimPrefix(name, "models/")

		isGemini := strings.Contains(cleanName, "gemini")
		isEmbedding := strings.Contains(cleanName, "embed")
		isTuned := strings.Contains(cleanName, "tuned")

		if isGemini && !isEmbedding && !isTuned {
			disp := m.DisplayName
			if disp == "" {
				disp = cleanName
			}
			models = append(models, ModelInfo{
				Name:        cleanName,
				DisplayName: disp,
			})
		}
	}
	return models, nil
}
