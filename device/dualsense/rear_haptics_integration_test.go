package dualsense

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"github.com/Alia5/VIIPER/device"
)

type rearIntegrationCapture struct {
	rear           []byte
	speaker        []byte
	rearPackets    int
	speakerPackets int
}

func newRearIntegrationDevice(t testing.TB, selected, edge bool) (*DualSense, *rearIntegrationCapture) {
	t.Helper()
	var options *device.CreateOptions
	if selected {
		options = &device.CreateOptions{DeviceSpecific: `{"hapticsConverter":"sony-bt-wdl-sinc64-v1"}`}
	}
	var d *DualSense
	var err error
	if edge {
		d, err = NewEdge(options)
	} else {
		d, err = New(options)
	}
	if err != nil {
		t.Fatal(err)
	}
	capture := &rearIntegrationCapture{}
	d.SetRealtimeHapticsCallback(func(out OutputState) {
		capture.rear = append(capture.rear, out.BluetoothCombinedOutputReport[BluetoothCombinedHapticsOffset:BluetoothCombinedHapticsOffset+BluetoothHapticsSampleSize]...)
		capture.rearPackets++
	})
	d.SetAtomicAudioHapticsCallback(func(_ OutputState, pcm []byte) {
		capture.speaker = append(capture.speaker, pcm...)
		capture.speakerPackets++
	})
	d.SetOutputCallback(func(OutputState) {})
	d.SetInterfaceAltSetting(InterfaceHapticsAudio, 1)
	return d, capture
}

func rearIntegrationUSB(rear []byte) ([]byte, []byte) {
	frames := len(rear) / 4
	source := make([]byte, frames*8)
	front := make([]byte, frames*4)
	for i := 0; i < frames; i++ {
		binary.LittleEndian.PutUint16(source[i*8:], uint16(int16(i*29)))
		binary.LittleEndian.PutUint16(source[i*8+2:], uint16(int16(-i*43)))
		copy(source[i*8+4:i*8+8], rear[i*4:i*4+4])
		copy(front[i*4:i*4+4], source[i*8:i*8+4])
	}
	return source, front
}

func feedRearIntegration(t testing.TB, d *DualSense, pcm []byte) {
	t.Helper()
	if !d.HandleIsoOutTransfer(EndpointHapticsAudioOut, d.IsoOutGeneration(EndpointHapticsAudioOut), pcm) {
		t.Fatal("current source generation rejected")
	}
}

func TestSonyRearIntegrationBoundaryCountsAndIndependentSpeaker(t *testing.T) {
	for _, edge := range []bool{false, true} {
		t.Run(fmt.Sprint("edge=", edge), func(t *testing.T) {
			d, c := newRearIntegrationDevice(t, true, edge)
			checkpoints := []int{34, 35, 480, 512, 530, 531, 960, 1042, 1043, 7680, 7699}
			previous := 0
			for _, n := range checkpoints {
				feedRearIntegration(t, d, make([]byte, (n-previous)*8))
				previous = n
				want := 0
				if n >= 531 {
					want = 1 + (n-531)/512
				}
				if c.rearPackets != want || c.speakerPackets != n/480 {
					t.Fatalf("after%d frames rear/speaker=%d/%d want%d/%d", n, c.rearPackets, c.speakerPackets, want, n/480)
				}
			}
		})
	}
}

func TestSonyRearIntegrationIndependentGoldenAcrossChunksAndControls(t *testing.T) {
	rear, expected := loadSonyRearOracle(t)
	pcm, front := rearIntegrationUSB(rear)
	for _, chunk := range []int{1, 47, 48, 479, 480, 512, 531, 3840, 16384} {
		for _, controls := range []bool{false, true} {
			t.Run(fmt.Sprintf("chunk%d/controls%v", chunk, controls), func(t *testing.T) {
				d, c := newRearIntegrationDevice(t, true, false)
				for at := 0; at < len(pcm); {
					end := min(len(pcm), at+chunk*8)
					feedRearIntegration(t, d, pcm[at:end])
					at = end
					if controls {
						var report [OutputReportSize]byte
						report[0] = ReportIDOutput
						report[2] = 0x04
						report[45] = byte(at)
						report[46] = byte(at >> 8)
						handled, accepted := d.TryHandleOutputCommand(EndpointOut&0xf, [8]byte{}, report[:])
						if !handled || !accepted {
							t.Fatal("LED update rejected")
						}
					}
				}
				complete := len(expected) / BluetoothHapticsSampleSize * BluetoothHapticsSampleSize
				if !bytes.Equal(c.rear, expected[:complete]) {
					t.Fatalf("real media pipeline differs from independent WDL: got%d expected%d bytes", len(c.rear), complete)
				}
				frontComplete := len(front) / dualSenseV5SpeakerPayloadSize * dualSenseV5SpeakerPayloadSize
				if !bytes.Equal(c.speaker, front[:frontComplete]) {
					t.Fatal("front480-frame stream changed, duplicated or reordered")
				}
				if d.mediaReportDrops.Load() != 0 || d.mediaReportBuildFailures.Load() != 0 {
					t.Fatal("ordinary conversion dropped or failed a report")
				}
			})
		}
	}
}

func TestSonyRearIntegrationResetRetiresHistoryPartialAndStaleGeneration(t *testing.T) {
	rear, _ := loadSonyRearOracle(t)
	pcm, _ := rearIntegrationUSB(rear)
	for _, prefix := range []int{34, 35, 480, 512, 530, 531, 1042, 1043} {
		for _, kind := range []string{"endpoint", "alternate-same", "alternate-close-open"} {
			t.Run(fmt.Sprintf("%s/%d", kind, prefix), func(t *testing.T) {
				d, c := newRearIntegrationDevice(t, true, false)
				old := d.IsoOutGeneration(EndpointHapticsAudioOut)
				feedRearIntegration(t, d, pcm[:prefix*8])
				before := c.rearPackets
				switch kind {
				case "endpoint":
					d.ResetEndpoint(EndpointHapticsAudioOut)
				case "alternate-same":
					d.SetInterfaceAltSetting(InterfaceHapticsAudio, 1)
				default:
					d.SetInterfaceAltSetting(InterfaceHapticsAudio, 0)
					if d.HandleIsoOutTransfer(EndpointHapticsAudioOut, old, pcm[:8]) {
						t.Fatal("inactive stream accepted PCM")
					}
					d.SetInterfaceAltSetting(InterfaceHapticsAudio, 1)
				}
				if d.HandleIsoOutTransfer(EndpointHapticsAudioOut, old, pcm[:531*8]) {
					t.Fatal("old ISO generation reached new filter")
				}
				if c.rearPackets != before {
					t.Fatal("reset synthesized/flushed a retired filter tail")
				}
				c.rear = nil
				c.speaker = nil
				c.rearPackets = 0
				c.speakerPackets = 0
				feedRearIntegration(t, d, make([]byte, 530*8))
				if c.rearPackets != 0 {
					t.Fatal("partial old output shortened new startup")
				}
				feedRearIntegration(t, d, make([]byte, 513*8))
				if c.rearPackets != 2 || !bytes.Equal(c.rear, make([]byte, 128)) {
					t.Fatal("old source history leaked into successor silence")
				}
				if d.hapticsConverter != sonyRearHapticsConverter {
					t.Fatal("stream reset changed immutable converter selection")
				}
			})
		}
	}
}

func TestSonyRearIntegrationDefaultLegacyIsUnchanged(t *testing.T) {
	rear, oracle := loadSonyRearOracle(t)
	pcm, front := rearIntegrationUSB(rear)
	for _, edge := range []bool{false, true} {
		t.Run(fmt.Sprint(edge), func(t *testing.T) {
			d, c := newRearIntegrationDevice(t, false, edge)
			feedRearIntegration(t, d, pcm)
			var expected []byte
			var block [BluetoothHapticsSampleSize]byte
			for at := 0; at+512*8 <= len(pcm); at += 512 * 8 {
				copyUSBHapticsChannelsToBluetoothSample(block[:], pcm[at:at+512*8])
				expected = append(expected, block[:]...)
			}
			if d.hapticsConverter != legacyRearHapticsConverter || !bytes.Equal(c.rear, expected) {
				t.Fatal("default/legacy consumer no longer uses exact prior512-frame conversion")
			}
			if bytes.Equal(c.rear[:min(len(c.rear), len(oracle))], oracle[:min(len(c.rear), len(oracle))]) {
				t.Fatal("fixture does not distinguish legacy from Sony conversion")
			}
			if !bytes.Equal(c.speaker, front[:len(c.speaker)]) {
				t.Fatal("legacy front PCM changed")
			}
		})
	}
}

func TestSonyRearIntegrationIndependentDevicesDoNotShareHistory(t *testing.T) {
	rear, expected := loadSonyRearOracle(t)
	pcm, _ := rearIntegrationUSB(rear)
	a, ca := newRearIntegrationDevice(t, true, false)
	b, cb := newRearIntegrationDevice(t, true, true)
	for at := 0; at < len(pcm); {
		end := min(len(pcm), at+47*8)
		feedRearIntegration(t, a, pcm[at:end])
		feedRearIntegration(t, b, make([]byte, end-at))
		at = end
	}
	want := expected[:len(expected)/64*64]
	if !bytes.Equal(ca.rear, want) || !bytes.Equal(cb.rear, make([]byte, len(want))) {
		t.Fatal("device-local filter history crossed controller identities")
	}
}

func TestSonyRearIntegrationFeatureGainPrecedesConversionAndFrontExtraction(t *testing.T) {
	rear, _ := loadSonyRearOracle(t)
	source, _ := rearIntegrationUSB(rear)
	for _, mute := range []bool{false, true} {
		t.Run(fmt.Sprint("mute=", mute), func(t *testing.T) {
			d, c := newRearIntegrationDevice(t, true, false)
			reference := newSpeakerAudioFeatureState()
			if mute {
				d.speakerAudioFeature.setMute(true)
				reference.setMute(true)
			} else {
				d.speakerAudioFeature.setVolume(audioSpeakerVolumeDefault - 6*256)
				reference.setVolume(audioSpeakerVolumeDefault - 6*256)
			}
			var processed []byte
			for at := 0; at < len(source); {
				end := min(len(source), at+480*8)
				value, lease := reference.applyPCM(source[at:end], 4)
				processed = append(processed, value...)
				lease.release()
				feedRearIntegration(t, d, source[at:end])
				at = end
			}
			inputRear := make([]byte, len(processed)/2)
			front := make([]byte, len(processed)/2)
			for frame := 0; frame < len(processed)/8; frame++ {
				copy(inputRear[frame*4:], processed[frame*8+4:frame*8+8])
				copy(front[frame*4:], processed[frame*8:frame*8+4])
			}
			var r sonyRearResampler
			expected := appendSonyRearFixture(&r, inputRear, 48)
			expected = expected[:len(expected)/64*64]
			if !bytes.Equal(c.rear, expected) || !bytes.Equal(c.speaker, front[:len(c.speaker)]) {
				t.Fatal("feature gain/ramp moved after conversion or changed one lane only")
			}
		})
	}
}

func TestSonyRearIntegrationBlockedCallbackCannotCrossResetGeneration(t *testing.T) {
	d, _ := newRearIntegrationDevice(t, true, false)
	writer := newDualSenseOutputWriter(nil, nil, nil)
	writer.SetSpeakerGeneration(d.IsoOutGeneration(EndpointHapticsAudioOut))
	entered := make(chan struct{})
	resume := make(chan struct{})
	done := make(chan bool, 1)
	d.setV5OutputCallbacks(1, writer.EnqueueOutputState, writer.EnqueueAtomicAudioHapticsState, func(out OutputState, generation uint64) {
		close(entered)
		<-resume
		writer.EnqueueRealtimeHapticsStateGeneration(out, generation)
	}, writer.ResetSpeakerGeneration)
	old := d.IsoOutGeneration(EndpointHapticsAudioOut)
	go func() { done <- d.HandleIsoOutTransfer(EndpointHapticsAudioOut, old, makeV5USBPCM(0, 531, 12000)) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		close(resume)
		t.Fatal("producer never reached real rear callback")
	}
	d.ResetEndpoint(EndpointHapticsAudioOut)
	close(resume)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("callback did not retire")
	}
	if len(writer.realtimeHaptics) != 0 || len(writer.audio) != 0 {
		t.Fatal("old prepared report entered post-reset writer")
	}
	d.setV5OutputCallbacks(1, writer.EnqueueOutputState, writer.EnqueueAtomicAudioHapticsState, writer.EnqueueRealtimeHapticsStateGeneration, writer.ResetSpeakerGeneration)
	feedRearIntegration(t, d, make([]byte, 531*8))
	if len(writer.realtimeHaptics) != 1 || len(writer.audio) != 1 {
		t.Fatal("new generation cannot publish after retirement")
	}
	writer.drainMediaQueues()
}

var rearIntegrationAllocationPositive []byte

func TestSonyRearIntegrationLoadedWriterAllocatesZero(t *testing.T) {
	if raceEnabled {
		t.Skip("race instrumentation allocates")
	}
	d, _ := newRearIntegrationDevice(t, true, false)
	writer := newDualSenseOutputWriter(discardV5MediaConn{}, nil, nil)
	generation := d.IsoOutGeneration(EndpointHapticsAudioOut)
	writer.SetSpeakerGeneration(generation)
	d.setV5OutputCallbacks(1, writer.EnqueueOutputState, writer.EnqueueAtomicAudioHapticsState, writer.EnqueueRealtimeHapticsStateGeneration, writer.ResetSpeakerGeneration)
	pcm := makeV5USBPCM(0, 480, 12000)
	for _, gain := range []bool{false, true} {
		t.Run(fmt.Sprint("gain=", gain), func(t *testing.T) {
			if gain {
				d.speakerAudioFeature.setVolume(audioSpeakerVolumeDefault - 6*256)
				d.speakerAudioFeature.resetStreamGain()
			}
			for i := 0; i < 100; i++ {
				feedRearIntegration(t, d, pcm)
				writer.drainMediaQueues()
			}
			allocations := testing.AllocsPerRun(1000, func() {
				if !d.HandleIsoOutTransfer(EndpointHapticsAudioOut, generation, pcm) {
					panic("rejected")
				}
				writer.drainMediaQueues()
			})
			if allocations != 0 {
				t.Fatalf("selected full pipeline allocated%.2f/run", allocations)
			}
		})
	}
	positive := testing.AllocsPerRun(10, func() { rearIntegrationAllocationPositive = make([]byte, 128) })
	if positive < 1 {
		t.Fatal("allocation instrumentation positive control failed")
	}
}
