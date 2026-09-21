package dualsense

import "fmt"

const (
	legacyRearHapticsConverter = "box16"
	sonyRearHapticsConverter   = "sony-bt-wdl-sinc64-v1"
)

// dualSenseCreateState extends the existing identity payload without making
// the converter mutable device metadata. It is selected for the lifetime of
// the virtual device. Omission preserves every existing client's rear format.
type dualSenseCreateState struct {
	MetaState
	HapticsConverter string `json:"hapticsConverter,omitempty"`
}

func selectRearHapticsConverter(requested string, gamepadOnly bool) (string, error) {
	if requested == "" {
		return legacyRearHapticsConverter, nil
	}
	if gamepadOnly {
		return "", fmt.Errorf("hapticsConverter requires an audio-capable DualSense device")
	}
	switch requested {
	case legacyRearHapticsConverter, sonyRearHapticsConverter:
		return requested, nil
	default:
		return "", fmt.Errorf("unsupported DualSense hapticsConverter %q", requested)
	}
}
