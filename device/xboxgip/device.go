package xboxgip

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Alia5/VIIPER/device"
	"github.com/Alia5/VIIPER/usb"
	"github.com/Alia5/VIIPER/usbip"
)

type createOptions struct {
	SerialNumber string `json:"serial_number"`
}

// RumbleState exposes the two main GIP vibration levels as percentages.
// Trigger impulses are accepted by the protocol and folded into the main
// level so existing two-byte VIIPER clients continue to receive feedback.
type RumbleState struct {
	LeftMotor  uint8
	RightMotor uint8
}

type frameKind uint8

const (
	frameControl frameKind = iota
	frameHello
	frameMetadata
	frameInput
)

type queuedFrame struct {
	kind frameKind
	data []byte
}

// metadataTransfer keeps the one reliable GIP metadata exchange associated
// with this USB connection. The metadata blob itself is static, but Windows
// acknowledges its fragments through Protocol Control. Sending every fragment
// at once used to leave the host with no Metadata Complete record and no
// reliable progress boundary, which can prevent the GIP stack from exposing
// the controller to XInput.
//
// This intentionally is a small, device-local state machine: it does not
// revive the former broker or protocol package. Its stages follow the
// documented GIP fragment sequence: initial fragment with ACK request, host
// ACK, remaining fragments with an ACK request only on the final fragment,
// then the six-byte GIP Metadata Complete record. Windows' USB/IP XGIP route
// consumes that completion directly after the final fragment, while a final
// ACK can still arrive afterwards and is accepted as reliable confirmation.
type metadataTransfer struct {
	sequence       byte
	sentEnd        int
	awaitingACK    bool
	finalQueued    bool
	completeQueued bool
	completed      bool
}

// Device is a controller-only GIP USB/IP persona. It deliberately leaves
// audio, headset, chatpad, expansion, wireless and firmware functionality out
// of the descriptor and protocol surface.
type Device struct {
	mu         sync.Mutex
	profile    controllerProfile
	descriptor usb.Descriptor
	input      InputState
	sequence   uint8
	frames     []queuedFrame
	wake       chan struct{}
	claimToken uint64
	claimed    queuedFrame
	claimOpen  bool
	feedback   rumbleScheduler
	active     bool
	arrival    bool
	guideDown  bool
	helloDue   time.Time
	metadata   *metadataTransfer
	deviceID   uint64
	// The following fields are read-only runtime diagnostics returned by
	// server/status. They make an incomplete host handshake visible without
	// changing the USB identity or retaining packet payloads.
	hostMessages           uint64
	lastHostMessage        string
	recentHostMessages     []string
	metadataRequests       uint64
	metadataRetriesIgnored uint64
	metadataACKs           uint64
	metadataInvalidACK     uint64
	securityOptOutComplete uint64
	framesDelivered        uint64
	metadataDelivered      uint64
}

func newDevice(profile controllerProfile, o *device.CreateOptions) (*Device, error) {
	args := createOptions{}
	if o != nil && o.DeviceSpecific != "" {
		if err := json.Unmarshal([]byte(o.DeviceSpecific), &args); err != nil {
			return nil, fmt.Errorf("invalid device specific JSON: %w", err)
		}
	}
	wireProfile := profile
	if o != nil {
		if o.IDVendor != nil {
			wireProfile.vid = *o.IDVendor
		}
		if o.IDProduct != nil {
			wireProfile.pid = *o.IDProduct
		}
	}
	deviceID, err := newPrimaryDeviceID()
	if err != nil {
		return nil, fmt.Errorf("generate GIP primary device ID: %w", err)
	}
	serial := args.SerialNumber
	if serial == "" {
		serial, err = newSerialNumber(deviceID)
		if err != nil {
			return nil, fmt.Errorf("generate GIP serial number: %w", err)
		}
	} else if !validSerialNumber(serial, deviceID) {
		return nil, fmt.Errorf("invalid GIP serial number: must be 32 hexadecimal characters containing the device ID")
	}
	desc := makeDescriptor(wireProfile, serial)
	d := &Device{
		profile: wireProfile, descriptor: desc, deviceID: deviceID,
		wake: make(chan struct{}, 1), arrival: true,
	}
	d.queueHelloLocked(time.Now())
	return d, nil
}

const primaryDeviceIDPrefix uint64 = 0x0000fffb00000000

func newPrimaryDeviceID() (uint64, error) {
	var bytes [4]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return 0, err
	}
	suffix := binary.LittleEndian.Uint32(bytes[:])
	if suffix == 0 {
		suffix = 1
	}
	return primaryDeviceIDPrefix | uint64(suffix), nil
}

// newSerialNumber builds the USB iSerialNumber required by MS-GIPUSB: exactly
// 32 hexadecimal digits which include the 64-bit primary Device ID. The first
// half is another random instance value rather than a VIIPER marker, keeping
// the USB descriptor surface protocol-shaped while server/status remains the
// explicit place to identify the virtual source.
func newSerialNumber(deviceID uint64) (string, error) {
	var bytes [8]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return formatSerialNumber(binary.LittleEndian.Uint64(bytes[:]), deviceID), nil
}

func formatSerialNumber(prefix, deviceID uint64) string {
	return fmt.Sprintf("%016X%016X", prefix, deviceID)
}

func validSerialNumber(serial string, deviceID uint64) bool {
	if len(serial) != 32 {
		return false
	}
	for _, character := range serial {
		if (character < '0' || character > '9') &&
			(character < 'a' || character > 'f') &&
			(character < 'A' || character > 'F') {
			return false
		}
	}
	return strings.Contains(strings.ToUpper(serial), fmt.Sprintf("%016X", deviceID))
}

// NewXboxOne creates the controller-only Xbox One GIP persona.
func NewXboxOne(o *device.CreateOptions) (*Device, error) {
	return newDevice(xboxOneProfile, o)
}

// NewXboxSeries creates the controller-only Xbox Series X|S GIP persona.
func NewXboxSeries(o *device.CreateOptions) (*Device, error) {
	return newDevice(xboxSeriesProfile, o)
}

func (d *Device) nextSequenceLocked() uint8 { return nextSequence(&d.sequence) }

func (d *Device) enqueueLocked(kind frameKind, data []byte) {
	if len(data) == 0 {
		return
	}
	if kind == frameInput {
		filtered := d.frames[:0]
		for _, frame := range d.frames {
			if frame.kind != frameInput {
				filtered = append(filtered, frame)
			}
		}
		d.frames = filtered
	}
	d.frames = append(d.frames, queuedFrame{kind: kind, data: data})
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

func (d *Device) queueHelloLocked(now time.Time) {
	filtered := d.frames[:0]
	for _, frame := range d.frames {
		if frame.kind != frameHello {
			filtered = append(filtered, frame)
		}
	}
	d.frames = filtered
	d.enqueueLocked(frameHello, makeHello(d.profile, d.deviceID, d.nextSequenceLocked()))
	d.helloDue = now.Add(500 * time.Millisecond)
}

func (d *Device) dropFramesLocked(kind frameKind) {
	filtered := d.frames[:0]
	for _, frame := range d.frames {
		if frame.kind != kind {
			filtered = append(filtered, frame)
		}
	}
	d.frames = filtered
}

func (d *Device) queueMetadataLocked(data []byte) {
	if len(data) == 0 {
		return
	}
	d.frames = append(d.frames, queuedFrame{kind: frameMetadata, data: data})
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

func (d *Device) startMetadataLocked(sequence byte) {
	d.dropFramesLocked(frameMetadata)
	metadata := makeOfficialGamepadMetadata()
	length := len(metadata)
	if length > metadataFragmentCapacity {
		length = metadataFragmentCapacity
	}
	d.metadata = &metadataTransfer{sequence: sequence, sentEnd: length, awaitingACK: true}
	d.queueMetadataLocked(makeMetadataFragment(
		sequence, metadata[:length], 0, len(metadata), true, true,
	))
}

func (d *Device) queueMetadataRemainderLocked(transfer *metadataTransfer) {
	metadata := makeOfficialGamepadMetadata()
	for offset := transfer.sentEnd; offset < len(metadata); {
		length := len(metadata) - offset
		if length > metadataFragmentCapacity {
			length = metadataFragmentCapacity
		}
		final := offset+length == len(metadata)
		d.queueMetadataLocked(makeMetadataFragment(
			transfer.sequence, metadata[offset:offset+length], offset, len(metadata), false, final,
		))
		offset += length
	}
	transfer.sentEnd = len(metadata)
	transfer.finalQueued = true
	transfer.awaitingACK = true
	// Keep the complete record directly behind the final fragment. This has
	// been verified against the Windows XGIP stack over USB/IP: waiting for the
	// final ACK before queuing it leaves Windows waiting and it eventually
	// stops the device. The final ACME is retained and a later ACK is still
	// parsed below, so delivery failures remain visible rather than ignored.
	transfer.completeQueued = true
	d.queueMetadataLocked(makeMetadataComplete(transfer.sequence, transfer.sentEnd))
}

// UpdateInputState publishes the newest controller state and coalesces stale
// input frames so a slow USB/IP reader cannot build an unbounded queue.
func (d *Device) UpdateInputState(state InputState) {
	d.mu.Lock()
	d.input = state
	if d.active {
		d.queueGuideStatusLocked(state.Buttons&ButtonGuide != 0)
		d.enqueueLocked(frameInput, makeInput(state, d.nextSequenceLocked()))
	}
	d.mu.Unlock()
}

func (d *Device) queueGuideStatusLocked(down bool) {
	if down == d.guideDown {
		return
	}
	d.guideDown = down
	d.enqueueLocked(frameControl, makeGuideStatus(d.nextSequenceLocked(), down))
}

func (d *Device) setRumbleCallback(callback func(RumbleState)) {
	d.feedback.SetCallback(callback)
}

func (d *Device) popFrame(ctx context.Context) []byte {
	for {
		d.mu.Lock()
		if len(d.frames) != 0 {
			frame := d.frames[0]
			d.frames = d.frames[1:]
			d.framesDelivered++
			if frame.kind == frameMetadata {
				d.metadataDelivered++
			}
			if len(d.frames) != 0 {
				select {
				case d.wake <- struct{}{}:
				default:
				}
			}
			d.mu.Unlock()
			return frame.data
		}
		var helloDue time.Time
		if d.arrival {
			now := time.Now()
			if !d.helloDue.After(now) {
				d.queueHelloLocked(now)
				d.mu.Unlock()
				continue
			}
			helloDue = d.helloDue
		}
		d.mu.Unlock()

		if helloDue.IsZero() {
			select {
			case <-ctx.Done():
				return nil
			case <-d.wake:
			}
			continue
		}

		wait := time.Until(helloDue)
		if wait < 0 {
			wait = 0
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return nil
		case <-d.wake:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-timer.C:
		}
	}
}

// ClaimInputReport implements VIIPER's USB/IP interrupt-IN ownership seam.
// A frame is not retired when it is selected: the endpoint scheduler calls
// CompleteInputReport only after it owns the response write. This matters for
// GIP metadata because losing the first reliable fragment makes Windows retry
// four times and then remove the controller.
func (d *Device) ClaimInputReport(destination []byte) (int, uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.claimOpen {
		return 0, 0
	}
	if len(d.frames) == 0 && d.arrival && !d.helloDue.After(time.Now()) {
		d.queueHelloLocked(time.Now())
	}
	if len(d.frames) == 0 {
		return 0, 0
	}
	frame := d.frames[0]
	if len(frame.data) == 0 || len(frame.data) > len(destination) {
		return 0, 0
	}
	d.frames = d.frames[1:]
	d.claimToken++
	if d.claimToken == 0 {
		d.claimToken++
	}
	d.claimed = frame
	d.claimOpen = true
	copy(destination, frame.data)
	return len(frame.data), d.claimToken
}

// CompleteInputReport commits an accepted interrupt-IN frame or restores it
// at the front of the queue when USB/IP could not present it. The restored
// bytes and sequence are unchanged, preserving GIP reliable-transfer rules.
func (d *Device) CompleteInputReport(token uint64, presented bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.claimOpen || token == 0 || token != d.claimToken {
		return
	}
	frame := d.claimed
	d.claimed = queuedFrame{}
	d.claimOpen = false
	if presented {
		d.framesDelivered++
		if frame.kind == frameMetadata {
			d.metadataDelivered++
		}
		return
	}
	d.frames = append([]queuedFrame{frame}, d.frames...)
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// HandleTransfer implements the two GIP interrupt endpoints. Invalid OUT
// packets are ignored instead of closing the USB/IP device.
func (d *Device) HandleTransfer(ctx context.Context, ep uint32, dir uint32, out []byte) []byte {
	switch {
	case dir == usbip.DirIn && ep == uint32(EndpointIn&0x0f):
		return d.popFrame(ctx)
	case dir == usbip.DirOut && ep == uint32(EndpointOut&0x0f):
		d.handleOutput(out)
	}
	return nil
}

func (d *Device) handleOutput(packet []byte) {
	parseMessages(packet, func(message parsedMessage) {
		d.recordHostMessage(message)
		switch {
		case message.dataClass == dataClassCommand && message.message == messageProtocolACK && message.system():
			d.handleMetadataACK(message)
		case message.dataClass == dataClassCommand && message.message == messageMetadata && message.system():
			d.handleMetadataRequest(message)
		case message.dataClass == dataClassCommand && message.message == messageDeviceState && message.system():
			d.handleDeviceState(message.payload)
		case message.dataClass == dataClassCommand && message.message == messageSecurityData && message.system():
			d.handleSecurityData(message)
		case message.dataClass == dataClassCommand && message.message == messageDirectMotor && !message.system():
			d.handleDirectMotor(message.payload)
		}
		if message.flags&flagAcknowledge != 0 {
			d.mu.Lock()
			d.enqueueLocked(frameControl, makeACK(message, message.sequence))
			d.mu.Unlock()
		}
	})
}

func (d *Device) handleSecurityData(message parsedMessage) {
	// GIP's Security Data Complete format is bidirectional. Windows requests
	// the PC USB opt-out with the empty controller-only form; answer only that
	// exact form with the documented 01 00 completion body on the same unique
	// Security sequence. A host-sent 01 00 completion, fragments, ACME packets,
	// or all opaque authentication data deliberately have no device-side
	// behaviour here.
	if len(message.payload) != 0 || message.flags != flagSystem {
		return
	}
	d.mu.Lock()
	d.securityOptOutComplete++
	d.enqueueLocked(frameControl, makeSecurityDataComplete(message.sequence))
	d.mu.Unlock()
}

func (d *Device) recordHostMessage(message parsedMessage) {
	d.mu.Lock()
	d.hostMessages++
	summary := fmt.Sprintf(
		"class=%d message=%d flags=0x%02X sequence=%d payload=% X",
		message.dataClass, message.message, message.flags, message.sequence, message.payload,
	)
	d.lastHostMessage = summary
	d.recentHostMessages = append(d.recentHostMessages, summary)
	if len(d.recentHostMessages) > 16 {
		d.recentHostMessages = append([]string(nil), d.recentHostMessages[len(d.recentHostMessages)-16:]...)
	}
	d.mu.Unlock()
}

func (d *Device) handleMetadataRequest(message parsedMessage) {
	d.mu.Lock()
	d.metadataRequests++
	// A host can issue its next 500 ms retry while the accepted initial ACK is
	// being handled by the USB/IP endpoint scheduler. Once that ACK has opened
	// the final-fragment stage, replacing it would discard the only pending
	// response and make the host retry forever. Keep that reliable transfer
	// intact; retries before the initial ACK still restart from the newest
	// request as required by the GIP metadata stage.
	if d.metadata != nil && d.metadata.finalQueued && d.metadata.awaitingACK &&
		!d.metadata.completed {
		d.metadataRetriesIgnored++
		d.mu.Unlock()
		return
	}
	// A host may retry Metadata Request up to four times. Rebuild the transfer
	// on each early request and discard only stale metadata fragments.
	// An arrival Hello may already be waiting in the interrupt-IN queue when
	// Windows sends this request. It belongs to the discovery phase and must
	// never overtake the requested metadata fragment: XGIP treats that ordering
	// as a failed metadata transfer and retries until it stops the device.
	d.arrival = false
	d.helloDue = time.Time{}
	d.dropFramesLocked(frameHello)
	d.startMetadataLocked(message.sequence)
	d.mu.Unlock()
}

func (d *Device) handleMetadataACK(message parsedMessage) {
	acknowledgedEnd, ok := parseMetadataACK(message)
	if !ok {
		d.mu.Lock()
		d.metadataInvalidACK++
		d.mu.Unlock()
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	transfer := d.metadata
	if transfer == nil || transfer.completed || !transfer.awaitingACK ||
		message.sequence != transfer.sequence || acknowledgedEnd > transfer.sentEnd {
		d.metadataInvalidACK++
		return
	}
	// Delayed duplicate ACKs are harmless. The current stage must be fully
	// acknowledged before the next one is admitted.
	if acknowledgedEnd < transfer.sentEnd {
		return
	}
	d.metadataACKs++
	if !transfer.finalQueued {
		d.queueMetadataRemainderLocked(transfer)
		return
	}
	transfer.awaitingACK = false
	transfer.completed = true
	if !transfer.completeQueued {
		transfer.completeQueued = true
		d.queueMetadataLocked(makeMetadataComplete(transfer.sequence, transfer.sentEnd))
	}
}

func (d *Device) handleDeviceState(payload []byte) {
	state, ok := parseDeviceState(payload)
	if !ok {
		return
	}

	switch state {
	case 0: // START
		d.mu.Lock()
		d.arrival = false
		d.helloDue = time.Time{}
		d.active = true
		d.enqueueLocked(frameControl, makeStatus(d.nextSequenceLocked(), false))
		d.queueGuideStatusLocked(d.input.Buttons&ButtonGuide != 0)
		d.enqueueLocked(frameInput, makeInput(d.input, d.nextSequenceLocked()))
		d.mu.Unlock()
	case 1, 4: // STOP or OFF
		d.mu.Lock()
		d.active = false
		d.guideDown = false
		d.arrival = false
		d.helloDue = time.Time{}
		d.dropInputLocked()
		d.mu.Unlock()
		d.feedback.Stop()
	case 3: // FULL POWER is irrelevant to a wired-only persona.
		return
	case 5: // QUIESCE keeps the device active but must clear output state.
		d.feedback.Stop()
	case 7: // RESET: report powering off, then restart the normal Hello stage.
		d.mu.Lock()
		d.active = false
		d.guideDown = false
		d.arrival = true
		d.metadata = nil
		d.frames = nil
		d.enqueueLocked(frameControl, makeStatus(d.nextSequenceLocked(), true))
		d.queueHelloLocked(time.Now())
		d.mu.Unlock()
		d.feedback.Stop()
	}
}

func parseDeviceState(payload []byte) (byte, bool) {
	if len(payload) == 1 {
		return payload[0], true
	}
	// Windows sends this legacy 15-byte START form during some PC GIP
	// handshakes. Preserve support for it while keeping all other extended
	// forms fail-closed.
	if len(payload) == 15 {
		if payload[0] == 0x06 && payload[7] == 0x55 && payload[8] == 0x53 {
			return 0, true
		}
	}
	return 0, false
}

func (d *Device) dropInputLocked() {
	filtered := d.frames[:0]
	for _, frame := range d.frames {
		if frame.kind != frameInput {
			filtered = append(filtered, frame)
		}
	}
	d.frames = filtered
}

func (m parsedMessage) system() bool { return m.flags&flagSystem != 0 }

func (d *Device) handleDirectMotor(payload []byte) {
	command, ok := decodeDirectMotor(payload)
	if !ok {
		return
	}
	d.mu.Lock()
	active := d.active
	d.mu.Unlock()
	if !active && command.duration != 0 {
		return
	}
	d.feedback.Submit(command)
}

// GetDescriptor returns the controller-only Microsoft GIP USB descriptor.
func (d *Device) GetDescriptor() *usb.Descriptor { return &d.descriptor }

// VIIPERDeviceType keeps the two profiles dispatchable even though they share
// one implementation package. The API router uses this value for the live
// input/feedback stream instead of guessing from the Go package name.
func (d *Device) VIIPERDeviceType() string { return d.profile.name }

// GetDeviceSpecificArgs returns diagnostic metadata without changing the USB
// identity presented to Windows.
func (d *Device) GetDeviceSpecificArgs() map[string]any {
	d.mu.Lock()
	metadataStage := "not-requested"
	metadataSequence := 0
	metadataSent := 0
	if d.metadata != nil {
		metadataSequence = int(d.metadata.sequence)
		metadataSent = d.metadata.sentEnd
		switch {
		case d.metadata.completed:
			metadataStage = "complete"
		case d.metadata.completeQueued:
			metadataStage = "completion-sent-awaiting-final-ack"
		case d.metadata.finalQueued:
			metadataStage = "awaiting-final-ack"
		case d.metadata.awaitingACK:
			metadataStage = "awaiting-initial-ack"
		default:
			metadataStage = "ready"
		}
	}
	active, arrival := d.active, d.arrival
	hostMessages, lastHostMessage := d.hostMessages, d.lastHostMessage
	recentHostMessages := append([]string(nil), d.recentHostMessages...)
	metadataRequests, metadataRetriesIgnored, metadataACKs, metadataInvalidACK, securityOptOutComplete :=
		d.metadataRequests, d.metadataRetriesIgnored, d.metadataACKs, d.metadataInvalidACK, d.securityOptOutComplete
	framesQueued, framesDelivered, metadataDelivered :=
		len(d.frames), d.framesDelivered, d.metadataDelivered
	d.mu.Unlock()

	return map[string]any{
		"profile": d.profile.name, "label": d.profile.label,
		"backend": "usbip", "protocol": "gip", "virtual": true,
		"source": "viiper", "audio": false, "microphone": false,
		"headset": false, "chatpad": false, "expansions": false,
		"wireless": false, "firmware_update": false,
		"serial_number": d.descriptor.Strings[3],
		"gip_device_id": fmt.Sprintf("%016X", d.deviceID),
		"vid":           fmt.Sprintf("%04X", d.descriptor.Device.IDVendor),
		"pid":           fmt.Sprintf("%04X", d.descriptor.Device.IDProduct),
		"gip_active":    active, "gip_arrival": arrival,
		"gip_metadata_stage":            metadataStage,
		"gip_metadata_sequence":         metadataSequence,
		"gip_metadata_sent_bytes":       metadataSent,
		"gip_host_messages":             hostMessages,
		"gip_last_host_message":         lastHostMessage,
		"gip_recent_host_messages":      recentHostMessages,
		"gip_metadata_requests":         metadataRequests,
		"gip_metadata_retries_ignored":  metadataRetriesIgnored,
		"gip_metadata_acks":             metadataACKs,
		"gip_metadata_invalid_acks":     metadataInvalidACK,
		"gip_security_opt_out_complete": securityOptOutComplete,
		"gip_queued_frames":             framesQueued,
		"gip_frames_delivered":          framesDelivered,
		"gip_metadata_delivered":        metadataDelivered,
	}
}
