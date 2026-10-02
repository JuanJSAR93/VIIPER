// Package xboxgip provides controller-only USB GIP personas for Xbox One and
// Xbox Series X|S. The existing HID personas remain in device/xboxone and
// device/xboxseries; these are separate selectable device types.
package xboxgip

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"

	"github.com/Alia5/VIIPER/device"
	"github.com/Alia5/VIIPER/internal/server/api"
	"github.com/Alia5/VIIPER/usb"
)

func init() {
	api.RegisterDevice("xboxone-gip", &handler{profile: xboxOneProfile})
	api.RegisterDevice("xboxseries-gip", &handler{profile: xboxSeriesProfile})
}

type handler struct{ profile controllerProfile }

// feedbackWriter is the stream's one independent output lane. USB/IP may
// deliver feedback while the API goroutine is blocked waiting for the next
// input state, so writing from HandleTransfer would couple host output to
// client input and can stall the virtual controller. The queue retains only
// the latest motor state: a delayed or failed client must never replay an old
// vibration after a newer stop has arrived.
type feedbackWriter struct {
	conn    net.Conn
	logger  *slog.Logger
	profile string

	mu      sync.Mutex
	state   RumbleState
	pending bool
	closed  bool
	wake    chan struct{}
	done    chan struct{}
	once    sync.Once
	wg      sync.WaitGroup
}

func newFeedbackWriter(conn net.Conn, logger *slog.Logger, profile string) *feedbackWriter {
	writer := &feedbackWriter{
		conn: conn, logger: logger, profile: profile,
		wake: make(chan struct{}, 1), done: make(chan struct{}),
	}
	writer.wg.Add(1)
	go writer.run()
	return writer
}

func (writer *feedbackWriter) Enqueue(state RumbleState) {
	writer.mu.Lock()
	if writer.closed {
		writer.mu.Unlock()
		return
	}
	writer.state = state
	if writer.pending {
		writer.mu.Unlock()
		return
	}
	writer.pending = true
	writer.mu.Unlock()
	select {
	case writer.wake <- struct{}{}:
	case <-writer.done:
	}
}

func (writer *feedbackWriter) run() {
	defer writer.wg.Done()
	for {
		select {
		case <-writer.done:
			return
		case <-writer.wake:
			writer.mu.Lock()
			state := writer.state
			writer.pending = false
			writer.mu.Unlock()
			if err := writeRumbleState(writer.conn, state); err != nil {
				if writer.logger != nil {
					writer.logger.Error("send Xbox GIP rumble", "profile", writer.profile, "error", err)
				}
				writer.signalClose()
				return
			}
		}
	}
}

func writeRumbleState(conn net.Conn, state RumbleState) error {
	payload := []byte{state.LeftMotor, state.RightMotor}
	for len(payload) != 0 {
		written, err := conn.Write(payload)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		payload = payload[written:]
	}
	return nil
}

func (writer *feedbackWriter) signalClose() {
	writer.once.Do(func() {
		writer.mu.Lock()
		writer.closed = true
		writer.mu.Unlock()
		close(writer.done)
		_ = writer.conn.Close()
	})
}

func (writer *feedbackWriter) Close() {
	writer.signalClose()
	writer.wg.Wait()
}

func (h *handler) CreateDevice(o *device.CreateOptions) (usb.Device, error) {
	return newDevice(h.profile, o)
}

func (h *handler) StreamHandler() api.StreamHandlerFunc {
	return func(conn net.Conn, devPtr *usb.Device, logger *slog.Logger) error {
		if devPtr == nil || *devPtr == nil {
			return fmt.Errorf("nil device")
		}
		gamepad, ok := (*devPtr).(*Device)
		if !ok || gamepad.profile.name != h.profile.name {
			return fmt.Errorf("%w: expected %s GIP device", device.ErrWrongDeviceType, h.profile.name)
		}

		writer := newFeedbackWriter(conn, logger, h.profile.name)
		gamepad.setRumbleCallback(writer.Enqueue)
		defer writer.Close()
		defer gamepad.setRumbleCallback(nil)

		buffer := make([]byte, InputWireSize)
		for {
			if _, err := io.ReadFull(conn, buffer); err != nil {
				if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, net.ErrClosed) {
					return nil
				}
				return fmt.Errorf("read Xbox GIP input state: %w", err)
			}
			var state InputState
			if err := state.UnmarshalBinary(buffer); err != nil {
				return fmt.Errorf("decode Xbox GIP input state: %w", err)
			}
			gamepad.UpdateInputState(state)
		}
	}
}

func (h *handler) UpdateMetaState(_ string, _ *usb.Device) error { return nil }
