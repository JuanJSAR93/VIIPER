package usb

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"github.com/Alia5/VIIPER/usbip"
	"github.com/stretchr/testify/require"
)

type isoOutClockAdmission struct {
	value byte
	at    time.Time
}

type isoOutSampleClockDevice struct {
	*schedulerTestDevice
	clock    *fakeEndpointClock
	admitted chan isoOutClockAdmission
}

func (d *isoOutSampleClockDevice) HandleTransfer(
	ctx context.Context, ep, dir uint32, payload []byte,
) []byte {
	result := d.schedulerTestDevice.HandleTransfer(ctx, ep, dir, payload)
	d.admitted <- isoOutClockAdmission{value: payload[0], at: d.clock.Now()}
	return result
}

// This intentionally fails the old scheduler. A delayed completion notification
// is not a new media clock: already-queued PCM must retain its reserved sample
// timeline when the delay is shorter than the next URB's service window.
func TestIsoOutSampleClockCompletionJitterDoesNotAccumulate(t *testing.T) {
	base := time.Unix(900, 0)
	clock := newFakeEndpointClock(base)
	device := &isoOutSampleClockDevice{
		schedulerTestDevice: &schedulerTestDevice{desc: testCompositeDescriptor()},
		clock:               clock,
		admitted:            make(chan isoOutClockAdmission, 3),
	}
	recorder := newRecordingWriter()
	ctx, cancel := context.WithCancel(context.Background())
	worker := newEndpointWorkerWithClock(
		ctx, device, 1, usbip.DirOut, isoOutWorker, time.Millisecond,
		384, newResponseWriter(recorder, nil), func(err error) { t.Errorf("worker: %v", err) }, clock,
	)
	defer func() {
		cancel()
		worker.signal()
		<-worker.done
	}()

	packets := make([]usbip.IsoPacketDescriptor, 10)
	for i := range packets {
		packets[i] = usbip.IsoPacketDescriptor{Offset: uint32(i * 384), Length: 384}
	}
	expectedPayloads := make([][]byte, 0, 3)
	for seq := uint32(1); seq <= 3; seq++ {
		payload := make([]byte, 3840)
		for i := range payload {
			payload[i] = byte(i*17 + int(seq))
		}
		expectedPayloads = append(expectedPayloads, append([]byte(nil), payload...))
		require.True(t, worker.enqueue(seq, uint32(len(payload)), payload, packets, base))
		clear(payload) // The command reader may immediately reuse its scratch.
	}

	readAdmission := func(want byte) isoOutClockAdmission {
		t.Helper()
		select {
		case got := <-device.admitted:
			require.Equal(t, want, got.value, "each owned PCM payload must be consumed once, in order")
			return got
		case <-time.After(time.Second):
			t.Fatalf("no media admission for job %d", want)
			return isoOutClockAdmission{}
		}
	}
	currentEnd := func() time.Time {
		t.Helper()
		worker.mu.Lock()
		defer worker.mu.Unlock()
		require.GreaterOrEqual(t, worker.inFlight, 0)
		return worker.slots[worker.inFlight].serviceEnd
	}

	first := readAdmission(1)
	require.Equal(t, base, first.at)
	clock.waitForDeadline(t, base.Add(10*time.Millisecond))
	recorder.mu.Lock()
	writesBeforeEnd := len(recorder.writes)
	recorder.mu.Unlock()
	require.Zero(t, writesBeforeEnd, "no ACK before the reserved service end")
	clock.advance(11500 * time.Microsecond) // Completion timer delivered 1.5 ms late.
	recorder.waitForWrites(t, 1)
	second := readAdmission(2)
	require.Equal(t, base.Add(11500*time.Microsecond), second.at)
	secondEnd := currentEnd()
	t.Logf("A completion woke at +11.5ms; B callback=+%s, reserved end=+%s",
		second.at.Sub(base), secondEnd.Sub(base))
	if want := base.Add(20 * time.Millisecond); !secondEnd.Equal(want) {
		t.Errorf("completion jitter permanently shifted queued B: end=%s, want original %s",
			secondEnd.Sub(base), want.Sub(base))
	}

	// Deliver B's own completion 1.5 ms late as well. Following the observed
	// deadline keeps the baseline run bounded while demonstrating accumulated
	// drift (old: C ends at 33ms; corrected: original C ends at 30ms).
	clock.waitForDeadline(t, secondEnd)
	clock.set(secondEnd.Add(1500*time.Microsecond), true)
	recorder.waitForWrites(t, 2)
	third := readAdmission(3)
	thirdEnd := currentEnd()
	t.Logf("B completion woke +1.5ms late; C callback=+%s, reserved end=+%s",
		third.at.Sub(base), thirdEnd.Sub(base))
	if want := base.Add(30 * time.Millisecond); !thirdEnd.Equal(want) {
		t.Errorf("completion jitter accumulated into queued C: end=%s, want original %s",
			thirdEnd.Sub(base), want.Sub(base))
	}
	clock.waitForDeadline(t, thirdEnd)
	clock.set(thirdEnd, true)
	writes := recorder.waitForWrites(t, 3)
	for i, write := range writes {
		require.Equal(t, uint32(i+1), binary.BigEndian.Uint32(write.packet[4:8]),
			"all normal RET_SUBMIT completions remain ordered")
		require.Zero(t, binary.BigEndian.Uint32(write.packet[20:24]), "completion status")
		require.Equal(t, uint32(3840), binary.BigEndian.Uint32(write.packet[24:28]), "actual length")
		for packet := range packets {
			offset := retSubmitHeaderSize + packet*isoPacketDescriptorSize
			require.Equal(t, uint32(384), binary.BigEndian.Uint32(write.packet[offset+8:offset+12]))
			require.Zero(t, binary.BigEndian.Uint32(write.packet[offset+12:offset+16]))
		}
	}
	device.mu.Lock()
	actualPayloads := append([][]byte(nil), device.isoOutPayloads...)
	device.mu.Unlock()
	require.Equal(t, expectedPayloads, actualPayloads, "front/speaker and rear bytes remain exact and ordered")
	if got := worker.telemetry.reanchored.Load(); got != 0 {
		t.Errorf("sub-URB completion jitter must not reanchor media: got %d reanchors", got)
	}
}

func TestIsoOutSampleClockReanchorBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name           string
		kind           endpointWorkerKind
		packets, phase int
		lateness       time.Duration
		reanchor       bool
	}{
		{"ten_packet_below_window", isoOutWorker, 10, 0, 9999 * time.Microsecond, false},
		{"ten_packet_exact_window", isoOutWorker, 10, 0, 10 * time.Millisecond, true},
		{"ten_packet_large_stall", isoOutWorker, 10, 0, time.Second, true},
		{"completion_1_5ms_late", isoOutWorker, 10, 10, 1500 * time.Microsecond, false},
		{"completion_whole_window_late", isoOutWorker, 10, 10, 10 * time.Millisecond, false},
		{"completion_huge_stall", isoOutWorker, 10, 10, time.Second, false},
		{"one_packet_below_interval", isoOutWorker, 1, 0, 999 * time.Microsecond, false},
		{"one_packet_exact_interval", isoOutWorker, 1, 0, time.Millisecond, true},
		{"one_packet_completion_late", isoOutWorker, 1, 1, time.Millisecond, false},
		{"empty_below_interval", isoOutWorker, 0, 0, 999 * time.Microsecond, false},
		{"empty_exact_interval", isoOutWorker, 0, 0, time.Millisecond, true},
		{"interrupt_unchanged", interruptInWorker, 0, 0, time.Millisecond, true},
		{"microphone_unchanged", isoInWorker, 10, 1, time.Millisecond, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel() // Actual reservation/phase methods, without a racing worker.
			w := newEndpointWorker(ctx, &schedulerTestDevice{desc: testCompositeDescriptor()},
				1, usbip.DirOut, tc.kind, time.Millisecond, 384,
				newResponseWriter(bytes.NewBuffer(nil), nil), func(error) {})
			<-w.done
			base := time.Unix(910, 0)
			packets := make([]usbip.IsoPacketDescriptor, tc.packets)
			require.True(t, w.enqueue(1, 0, nil, packets, base))
			require.True(t, w.enqueue(2, 0, nil, packets, base))
			idx := w.claimNext()
			originalEnd := w.slots[idx].serviceEnd
			deadline := base.Add(time.Duration(tc.phase) * time.Millisecond)
			now := deadline.Add(tc.lateness)
			got, active := w.phaseDeadline(idx, tc.phase, now)
			require.True(t, active)
			wantDeadline, wantEnd := deadline, originalEnd
			if tc.reanchor {
				wantDeadline = now
				wantEnd = originalEnd.Add(tc.lateness)
			}
			require.Equal(t, wantDeadline, got)
			require.Equal(t, wantEnd, w.slots[idx].serviceEnd)
			require.Equal(t, wantEnd, w.slots[w.order[w.orderHead]].serviceAt)
			require.Equal(t, tc.reanchor, w.telemetry.reanchored.Load() == 1)
		})
	}
}

func TestIsoOutSampleClockHugeCompletionDelayDoesNotBurstQueuedMedia(t *testing.T) {
	base := time.Unix(920, 0)
	clock := newFakeEndpointClock(base)
	device := &isoOutSampleClockDevice{schedulerTestDevice: &schedulerTestDevice{desc: testCompositeDescriptor()},
		clock: clock, admitted: make(chan isoOutClockAdmission, 3)}
	recorder := newRecordingWriter()
	ctx, cancel := context.WithCancel(context.Background())
	w := newEndpointWorkerWithClock(ctx, device, 1, usbip.DirOut, isoOutWorker, time.Millisecond,
		384, newResponseWriter(recorder, nil), func(err error) { t.Errorf("worker: %v", err) }, clock)
	defer func() { cancel(); w.signal(); <-w.done }()
	packets := make([]usbip.IsoPacketDescriptor, 10)
	for i := range packets {
		packets[i] = usbip.IsoPacketDescriptor{Offset: uint32(i), Length: 1}
	}
	for seq := uint32(1); seq <= 3; seq++ {
		data := make([]byte, 10)
		data[0] = byte(seq)
		require.True(t, w.enqueue(seq, 10, data, packets, base))
	}
	read := func(want byte, wantAt time.Time) {
		t.Helper()
		select {
		case got := <-device.admitted:
			require.Equal(t, want, got.value)
			require.Equal(t, wantAt, got.at)
		case <-time.After(time.Second):
			t.Fatalf("missing callback %d", want)
		}
	}
	read(1, base)
	clock.waitForDeadline(t, base.Add(10*time.Millisecond))
	clock.advance(100 * time.Millisecond)
	recorder.waitForWrites(t, 1)
	read(2, base.Add(100*time.Millisecond))
	clock.waitForDeadline(t, base.Add(110*time.Millisecond))
	select {
	case unexpected := <-device.admitted:
		t.Fatalf("queued media catch-up burst: %+v", unexpected)
	default:
	}
	recorder.mu.Lock()
	writesBeforeEnd := len(recorder.writes)
	recorder.mu.Unlock()
	require.Equal(t, 1, writesBeforeEnd, "no second ACK before its fresh full window")
	require.Equal(t, uint64(1), w.telemetry.reanchored.Load())
	clock.advance(10 * time.Millisecond)
	recorder.waitForWrites(t, 2)
	read(3, base.Add(110*time.Millisecond))
	clock.waitForDeadline(t, base.Add(120*time.Millisecond))
	clock.advance(10 * time.Millisecond)
	recorder.waitForWrites(t, 3)
}

func TestIsoOutSampleClockIdleAdmissionStillAnchorsToArrival(t *testing.T) {
	for _, count := range []int{0, 1, 10} {
		t.Run(fmt.Sprintf("packets_%d", count), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			w := newEndpointWorker(ctx, &schedulerTestDevice{desc: testCompositeDescriptor()},
				1, usbip.DirOut, isoOutWorker, time.Millisecond, 384,
				newResponseWriter(bytes.NewBuffer(nil), nil), func(error) {})
			<-w.done
			base := time.Unix(930, 0)
			packets := make([]usbip.IsoPacketDescriptor, count)
			require.True(t, w.enqueue(1, 0, nil, packets, base))
			idx := w.claimNext()
			w.finishCurrentWithRetention(idx, true, false)
			arrival := base.Add(50 * time.Millisecond)
			require.True(t, w.enqueue(2, 0, nil, packets, arrival))
			idx = w.claimNext()
			require.Equal(t, arrival, w.slots[idx].serviceAt)
			require.Equal(t, arrival.Add(time.Duration(count)*time.Millisecond), w.slots[idx].serviceEnd)
		})
	}
}
