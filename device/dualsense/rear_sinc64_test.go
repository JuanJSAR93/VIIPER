package dualsense

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// The expected bytes were emitted by unmodified public WDL C++, not this Go
// implementation or a second implementation of its coefficient calculation.
// The JSON records the upstream revision, oracle and generator hashes.
//
//go:embed testdata/rear_sinc64_wdl_golden.json
var sonyRearOracleFixture []byte

func loadSonyRearOracle(t testing.TB) (source, expected []byte) {
	t.Helper()
	var fixture struct {
		Schema         int    `json:"schema"`
		Revision       string `json:"wdl_commit"`
		Source         []byte `json:"input_s16le_base64"`
		Expected       []byte `json:"expected_s8_base64"`
		SourceHash     string `json:"source_sha256"`
		ExpectedHash   string `json:"expected_sha256"`
		InputFrames    int    `json:"input_frames"`
		ExpectedFrames int    `json:"expected_frames"`
	}
	if err := json.Unmarshal(sonyRearOracleFixture, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Schema != 1 || fixture.Revision != "8f4d783de745126ac8c201455dc30818c8613324" ||
		len(fixture.Source) != fixture.InputFrames*4 || len(fixture.Expected) != fixture.ExpectedFrames*2 {
		t.Fatal("invalid independently generated oracle fixture identity or size")
	}
	for _, item := range []struct {
		data []byte
		hash string
	}{{fixture.Source, fixture.SourceHash}, {fixture.Expected, fixture.ExpectedHash}} {
		digest := sha256.Sum256(item.data)
		if !strings.EqualFold(hex.EncodeToString(digest[:]), item.hash) {
			t.Fatal("independent fixture hash mismatch")
		}
	}
	return fixture.Source, fixture.Expected
}

func appendSonyRearFixture(r *sonyRearResampler, source []byte, chunk int) []byte {
	result := make([]byte, 0, len(source)/32)
	for at := 0; at < len(source); {
		end := min(len(source), at+chunk*4)
		for ; at < end; at += 4 {
			left, right, ready := r.Append(int16(binary.LittleEndian.Uint16(source[at:])),
				int16(binary.LittleEndian.Uint16(source[at+2:])))
			if ready {
				result = append(result, byte(left), byte(right))
			}
		}
	}
	return result
}

func TestSonyRearSinc64IndependentWDLGolden(t *testing.T) {
	source, expected := loadSonyRearOracle(t)
	for _, chunk := range []int{1, 7, 16, 48, 240, 480, 512, 1024, 4096, 16384} {
		t.Run(fmt.Sprint(chunk), func(t *testing.T) {
			var resampler sonyRearResampler
			actual := appendSonyRearFixture(&resampler, source, chunk)
			if !bytes.Equal(actual, expected) {
				for i := 0; i < min(len(actual), len(expected)); i++ {
					if actual[i] != expected[i] {
						t.Fatalf("sample byte %d: got %d, WDL oracle %d", i, int8(actual[i]), int8(expected[i]))
					}
				}
				t.Fatalf("output length got %d, WDL oracle %d", len(actual), len(expected))
			}
		})
	}
}

func TestSonyRearSinc64AvailabilityAndSilentStartup(t *testing.T) {
	var r sonyRearResampler
	for frame := 1; frame <= 4096; frame++ {
		left, right, ready := r.Append(0, 0)
		wantReady := frame >= 35 && (frame-35)%16 == 0
		if ready != wantReady || left != 0 || right != 0 {
			t.Fatalf("frame %d: (%d,%d,%v), ready expected %v", frame, left, right, ready, wantReady)
		}
	}
}

func TestSonyRearSinc64BlockBoundaryCounts(t *testing.T) {
	var r sonyRearResampler
	if r.framesUntilOutput() != 35 {
		t.Fatal("zero-value startup lookahead differs from WDL")
	}
	wants := map[int]int{34: 0, 35: 1, 530: 31, 531: 32, 1042: 63, 1043: 64}
	outputs := 0
	for frame := 1; frame <= 1043; frame++ {
		previous := r.framesUntilOutput()
		_, _, ready := r.Append(0, 0)
		if ready {
			outputs++
			if previous != 1 || r.framesUntilOutput() != 16 {
				t.Fatal("output countdown differs from fixed 16:1 phase")
			}
		} else if r.framesUntilOutput() != previous-1 {
			t.Fatal("source append did not advance exactly one frame")
		}
		if want, exists := wants[frame]; exists && outputs != want {
			t.Fatalf("after %d frames: %d outputs, expected %d", frame, outputs, want)
		}
	}
}

func TestSonyRearSinc64ResetRetiresEveryPhaseAndTail(t *testing.T) {
	source, expected := loadSonyRearOracle(t)
	for _, prefix := range []int{1, 15, 31, 34, 35, 36, 49, 50, 51, 63, 64, 127, 480, 511, 512, 1024, 16383} {
		t.Run(fmt.Sprint(prefix), func(t *testing.T) {
			var r sonyRearResampler
			for i := 0; i < prefix; i++ {
				r.Append(32767, -32768)
			}
			r.Reset()
			actual := appendSonyRearFixture(&r, source, 48)
			if !bytes.Equal(actual, expected) {
				t.Fatal("reset preserved old source history, phase, or tail")
			}
			r.Reset()
			for i := 0; i < 4096; i++ {
				left, right, _ := r.Append(0, 0)
				if left != 0 || right != 0 {
					t.Fatal("retired source leaked into successor silence")
				}
			}
		})
	}
}

func TestSonyRearQuantizerTruncatesAndClamps(t *testing.T) {
	for _, tc := range []struct {
		input float64
		want  int8
	}{
		{-10000, -128}, {-128.001, -128}, {-128, -128}, {-127.999, -127},
		{-2.999, -2}, {-1.001, -1}, {-1, -1}, {-0.999, 0}, {-0.001, 0},
		{0, 0}, {0.001, 0}, {0.999, 0}, {1, 1}, {1.999, 1}, {2.001, 2},
		{126.999, 126}, {127, 127}, {127.999, 127}, {128, 127}, {10000, 127},
	} {
		if got := quantizeSonyRearSample(tc.input); got != tc.want {
			t.Errorf("quantize(%v) = %d, expected %d", tc.input, got, tc.want)
		}
	}
}

var sonyRearAllocationSink int8

func TestSonyRearSinc64SteadyStateDoesNotAllocate(t *testing.T) {
	var r sonyRearResampler
	r.Reset()
	allocations := testing.AllocsPerRun(100, func() {
		for i := 0; i < 512; i++ {
			left, right, _ := r.Append(int16(i*53), int16(-i*37))
			sonyRearAllocationSink = left ^ right
		}
	})
	if allocations != 0 {
		t.Fatalf("steady-state allocations per 512 source frames: %v", allocations)
	}
}

func BenchmarkSonyRearSinc64(b *testing.B) {
	var r sonyRearResampler
	r.Reset()
	b.ReportAllocs()
	b.SetBytes(512 * 4)
	for b.Loop() {
		for i := 0; i < 512; i++ {
			left, right, _ := r.Append(int16(i*53), int16(-i*37))
			sonyRearAllocationSink = left ^ right
		}
	}
}
