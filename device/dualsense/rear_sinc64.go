// This fixed-rate Go port is derived from Cockos WDL resample.cpp, commit
// 8f4d783de745126ac8c201455dc30818c8613324. It is an altered implementation,
// not the original WDL source. See docs/licenses/WDL-resampler.txt for the
// complete original notice and docs/dualsense-rear-resampler.md for provenance.
package dualsense

import "math"

// sonyRearResampler is the fixed 48 kHz stereo S16 -> 3 kHz stereo S8
// WDL sinc64 conversion. Its zero value is ready for use. The caller owns
// serialization and must Reset at a source-stream generation boundary.
// It contains no output queue, clock, goroutine, allocation, or device access.
// Merely constructing it does not change any existing V5 consumer's contract.
type sonyRearResampler struct {
	history [128][2]float64
	write   uint8
	read    uint8
	wait    uint8
}

// Reset retires all prior source history, fractional phase, and pending tail.
// It deliberately emits no synthesized end-of-stream samples.
func (r *sonyRearResampler) Reset() {
	*r = sonyRearResampler{write: 31, wait: 35}
}

// framesUntilOutput permits the existing assembler to stop a source segment
// exactly at a completed 32-frame rear block without introducing another queue.
func (r *sonyRearResampler) framesUntilOutput() int {
	if r.wait == 0 {
		return 35
	}
	return int(r.wait)
}

// Append consumes exactly one stereo source frame. The first output is ready
// after 35 frames and each subsequent output after 16 more frames, matching
// WDL's 31-frame zero prefix and guarded 64-tap window. Output n is centered at
// source frame 16*n, using context [-31,+32]; availability includes WDL's two
// extra guard frames. Packet boundaries do not reset the history or phase.
func (r *sonyRearResampler) Append(left, right int16) (int8, int8, bool) {
	if r.wait == 0 {
		r.Reset()
	}
	r.history[r.write] = [2]float64{float64(left) / 32768, float64(right) / 32768}
	r.write = (r.write + 1) & 127
	r.wait--
	if r.wait != 0 {
		return 0, 0, false
	}
	r.wait = 16

	// WDL's stereo SSE2 SincSample2N has separate even/odd accumulators,
	// then adds odd+even. Preserve that order rather than a serial dot product.
	var evenLeft, evenRight, oddLeft, oddRight float64
	for i := uint8(0); i < 64; i += 2 {
		even := r.history[(r.read+i)&127]
		odd := r.history[(r.read+i+1)&127]
		c0, c1 := sonyRearSinc64Coefficients[i], sonyRearSinc64Coefficients[i+1]
		evenLeft += float64(c0 * even[0])
		evenRight += float64(c0 * even[1])
		oddLeft += float64(c1 * odd[0])
		oddRight += float64(c1 * odd[1])
	}
	r.read = (r.read + 16) & 127
	return quantizeSonyRearSample((oddLeft + evenLeft) * 128),
		quantizeSonyRearSample((oddRight + evenRight) * 128), true
}

func quantizeSonyRearSample(sample float64) int8 {
	if sample >= 127 {
		return 127
	}
	if sample <= -128 {
		return -128
	}
	return int8(sample) // Go conversion truncates toward zero, never arithmetic >>8.
}

// Exact IEEE-754 float32 coefficients of the public WDL BuildLowPass ideal
// phase table [64:128] for SetMode(true,0,true,64,32), SetRates(48000,3000).
// Pinning bits avoids platform libm differences in coefficient generation.
// Samples and accumulation remain float64, as in WDL's default configuration.
var sonyRearSinc64Coefficients = func() [64]float64 {
	bits := [...]uint32{
		0xb557ed5a, 0xb68673be, 0xb751d312, 0xb7fe6eef,
		0xb883c571, 0xb8f3c2c1, 0xb94e002c, 0xb9a10c1a,
		0xb9ea6282, 0xba1ef729, 0xba482643, 0xba66fe50,
		0xba6d0191, 0xba46ae92, 0xb9b7a056, 0x39def217,
		0x3adc71a4, 0x3b6759e8, 0x3bc6c2a7, 0x3c194919,
		0x3c5bfa57, 0x3c95822d, 0x3cc27c48, 0x3cf3af71,
		0x3d13b0e0, 0x3d2db7ea, 0x3d46b6f0, 0x3d5d6997,
		0x3d7095f1, 0x3d7f2624, 0x3d842010, 0x3d85ac5d,
		0x3d842010, 0x3d7f2624, 0x3d7095f1, 0x3d5d6997,
		0x3d46b6f0, 0x3d2db7ea, 0x3d13b0e0, 0x3cf3af71,
		0x3cc27c48, 0x3c95822d, 0x3c5bfa57, 0x3c194919,
		0x3bc6c2a7, 0x3b6759e8, 0x3adc71a4, 0x39def217,
		0xb9b7a056, 0xba46ae92, 0xba6d0191, 0xba66fe50,
		0xba482643, 0xba1ef729, 0xb9ea6282, 0xb9a10c1a,
		0xb94e002c, 0xb8f3c2c1, 0xb883c571, 0xb7fe6eef,
		0xb751d312, 0xb68673be, 0xb557ed5a, 0xb3fae4da,
	}
	var result [64]float64
	for i, value := range bits {
		result[i] = float64(math.Float32frombits(value))
	}
	return result
}()
