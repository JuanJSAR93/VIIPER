package dualsense

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Alia5/VIIPER/device"
	"github.com/Alia5/VIIPER/usbip"
)

// This opt-in evidence test reads rear-only captures and independent original
// WDL C++ golden output. It never opens an audio/HID endpoint or starts apps.
// Ordinary CI retains the checked-in synthetic golden and integration tests.
func TestSonyRearCapturedPipelineMatchesIndependentWDL(t *testing.T) {
	manifestPath := os.Getenv("VIIPER_WDL_CAPTURE_PROOF")
	if manifestPath == "" {
		t.Skip("set VIIPER_WDL_CAPTURE_PROOF to the rear-only offline capture manifest")
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Schema   int    `json:"schema"`
		Revision string `json:"oracle_commit"`
		Cases    []struct {
			Name           string `json:"name"`
			Source         string `json:"source"`
			Expected       string `json:"expected"`
			SourceHash     string `json:"source_sha256"`
			ExpectedHash   string `json:"expected_sha256"`
			SourceFrames   int    `json:"source_frames"`
			ExpectedFrames int    `json:"oracle_frames"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Schema != 1 || manifest.Revision != "8f4d783de745126ac8c201455dc30818c8613324" || len(manifest.Cases) == 0 {
		t.Fatal("invalid independent capture manifest")
	}
	readVerified := func(path, expectedHash string) []byte {
		t.Helper()
		info, err := os.Stat(path)
		if err != nil || info.Size() > 64*1024*1024 {
			t.Fatalf("bounded capture unavailable: %s, %v", path, err)
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(contents)
		if !strings.EqualFold(hex.EncodeToString(digest[:]), expectedHash) {
			t.Fatalf("capture/oracle hash mismatch: %s", path)
		}
		return contents
	}
	for _, capture := range manifest.Cases {
		source := readVerified(capture.Source, capture.SourceHash)
		expected := readVerified(capture.Expected, capture.ExpectedHash)
		if len(source) != capture.SourceFrames*4 || len(expected) != capture.ExpectedFrames*2 {
			t.Fatal("capture manifest byte count mismatch")
		}
		// Restore only the captured rear pair to their actual four-channel USB
		// positions. The front pair is synthetic silence, not private audio.
		usbPCM := make([]byte, capture.SourceFrames*USBHapticsAudioFrameSize)
		for frame := 0; frame < capture.SourceFrames; frame++ {
			copy(usbPCM[frame*USBHapticsAudioFrameSize+4:], source[frame*4:frame*4+4])
		}
		for _, edge := range []bool{false, true} {
			for _, irregular := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/edge=%v/irregular=%v", capture.Name, edge, irregular), func(t *testing.T) {
					options := &device.CreateOptions{DeviceSpecific: `{"hapticsConverter":"sony-bt-wdl-sinc64-v1"}`}
					var dev *DualSense
					var err error
					if edge {
						dev, err = NewEdge(options)
					} else {
						dev, err = New(options)
					}
					if err != nil {
						t.Fatal(err)
					}
					actual := make([]byte, 0, len(expected))
					speakerCount := 0
					dev.SetRealtimeHapticsCallback(func(feedback OutputState) {
						report := feedback.BluetoothCombinedOutputReport
						block := len(actual) / BluetoothHapticsSampleSize
						if report[0] != 0x36 || report[76] != 0x92 || report[77] != 64 ||
							report[10] != byte(block) || report[1] != byte(block&15)<<4 {
							t.Fatal("actual pipeline report framing or sequential rear generation changed")
						}
						actual = append(actual, report[BluetoothCombinedHapticsOffset:BluetoothCombinedHapticsOffset+BluetoothHapticsSampleSize]...)
					})
					dev.SetAtomicAudioHapticsCallback(func(_ OutputState, front []byte) {
						speakerCount++
						if len(front) != dualSenseV5SpeakerPayloadSize || bytes.Count(front, []byte{0}) != len(front) {
							t.Fatal("rear converter modified the independent front speaker stream")
						}
					})
					dev.SetInterfaceAltSetting(InterfaceHapticsAudio, 1)
					pattern := []int{480}
					if irregular {
						pattern = []int{1, 47, 432, 17, 495, 3, 256, 31, 509, 2, 113}
					}
					for frame, n := 0, 0; frame < capture.SourceFrames; n++ {
						end := min(capture.SourceFrames, frame+pattern[n%len(pattern)])
						dev.HandleTransfer(context.Background(), EndpointHapticsAudioOut, usbip.DirOut,
							usbPCM[frame*USBHapticsAudioFrameSize:end*USBHapticsAudioFrameSize])
						frame = end
					}
					complete := len(expected) / BluetoothHapticsSampleSize * BluetoothHapticsSampleSize
					if !bytes.Equal(actual, expected[:complete]) {
						for i := 0; i < min(len(actual), complete); i++ {
							if actual[i] != expected[i] {
								t.Fatalf("pipeline scalar %d: %d, independent WDL %d", i, int8(actual[i]), int8(expected[i]))
							}
						}
						t.Fatalf("pipeline emitted %d bytes, expected complete WDL %d", len(actual), complete)
					}
					if !bytes.Equal(dev.sonyRearSample[:dev.sonyRearSampleLength], expected[complete:]) {
						t.Fatal("unpublished partial tail differs from independent WDL")
					}
					if speakerCount != capture.SourceFrames/480 || dev.mediaReportDrops.Load() != 0 {
						t.Fatal("speaker clock changed or media reports dropped")
					}
					t.Logf("sourceSHA=%s oracleSHA=%s sourceFrames=%d exactPublishedScalars=%d exactPendingScalars=%d speakerFrames=%d drops=0",
						capture.SourceHash, capture.ExpectedHash, capture.SourceFrames, len(actual), dev.sonyRearSampleLength, speakerCount)
				})
			}
		}
	}
}
