package xboxgip

import (
	"encoding/binary"
)

const (
	gipHeaderSize       = 4
	gipMaxPacketSize    = 64
	gipMaxPayload       = gipMaxPacketSize - gipHeaderSize
	messageInput        = 0
	messageProtocolACK  = 1
	messageHello        = 2
	messageDirectMotor  = 9
	messageMetadata     = 4
	messageDeviceState  = 5
	messageSecurityData = 6
	messageStatus       = 3
	dataClassCommand    = 0
	dataClassLowLatency = 1
	flagFragment        = 1 << 7
	flagInitFragment    = 1 << 6
	flagSystem          = 1 << 5
	flagAcknowledge     = 1 << 4
)

const (
	metadataHeaderSize = 16
	// The Windows-PC controller metadata carries the normal three gamepad
	// interfaces plus Microsoft's documented IDevAuthPCOptOut capability.
	// The opt-out is metadata, not a fabricated security response.
	metadataDeviceSize       = 135
	metadataMessageSize      = 23
	metadataTotalSize        = 198
	metadataFragmentHeader   = 6
	metadataFragmentCapacity = 58
)

func nextSequence(sequence *uint8) uint8 {
	*sequence++
	if *sequence == 0 {
		*sequence = 1
	}
	return *sequence
}

func makeMessage(dataClass, message, flags, sequence byte, payload []byte) []byte {
	if len(payload) > gipMaxPayload {
		return nil
	}
	out := make([]byte, gipHeaderSize+len(payload))
	out[0] = dataClass<<5 | (message & 0x1f)
	out[1] = flags
	out[2] = sequence
	out[3] = byte(len(payload))
	copy(out[gipHeaderSize:], payload)
	return out
}

func makeHello(profile controllerProfile, deviceID uint64, sequence byte) []byte {
	payload := make([]byte, 28)
	binary.LittleEndian.PutUint64(payload[0:8], deviceID)
	binary.LittleEndian.PutUint16(payload[8:10], profile.vid)
	binary.LittleEndian.PutUint16(payload[10:12], profile.pid)
	// Non-zero firmware identity; the exact build is intentionally VIIPER's
	// virtual persona rather than a claim to be Microsoft firmware.
	binary.LittleEndian.PutUint16(payload[12:14], 1)
	binary.LittleEndian.PutUint16(payload[14:16], 1)
	binary.LittleEndian.PutUint16(payload[16:18], 0)
	binary.LittleEndian.PutUint16(payload[18:20], 1)
	payload[20] = 1
	payload[21] = 0
	// RF, security, and GIP protocol versions: 1.0.
	copy(payload[22:28], []byte{1, 0, 1, 0, 1, 0})
	return makeMessage(dataClassCommand, messageHello, flagSystem, sequence, payload)
}

func mapButtons(buttons uint16) uint16 {
	var out uint16
	set := func(source uint16, destination uint16) {
		if buttons&source != 0 {
			out |= destination
		}
	}
	// MS-GIPUSB Table 4-68: bit 0 is reserved and bit 1 is Keep Alive.
	// USB gamepads do not toggle Keep Alive, so every ordinary digital control
	// begins at bit 2 and the high-byte controls begin at bit 8.
	set(ButtonMenu, 1<<2)
	set(ButtonView, 1<<3)
	set(ButtonA, 1<<4)
	set(ButtonB, 1<<5)
	set(ButtonX, 1<<6)
	set(ButtonY, 1<<7)
	set(ButtonDPadUp, 1<<8)
	set(ButtonDPadDown, 1<<9)
	set(ButtonDPadLeft, 1<<10)
	set(ButtonDPadRight, 1<<11)
	set(ButtonLeftBumper, 1<<12)
	set(ButtonRightBumper, 1<<13)
	set(ButtonLeftStick, 1<<14)
	set(ButtonRightStick, 1<<15)
	return out
}

func makeInput(state InputState, sequence byte) []byte {
	payload := make([]byte, 14)
	binary.LittleEndian.PutUint16(payload[0:2], mapButtons(state.Buttons))
	binary.LittleEndian.PutUint16(payload[2:4], clampTrigger(state.LeftTrigger))
	binary.LittleEndian.PutUint16(payload[4:6], clampTrigger(state.RightTrigger))
	binary.LittleEndian.PutUint16(payload[6:8], uint16(state.LeftStickX))
	binary.LittleEndian.PutUint16(payload[8:10], uint16(state.LeftStickY))
	binary.LittleEndian.PutUint16(payload[10:12], uint16(state.RightStickX))
	binary.LittleEndian.PutUint16(payload[12:14], uint16(state.RightStickY))
	return makeMessage(dataClassLowLatency, messageInput, 0, sequence, payload)
}

func makeStatus(sequence byte, poweringOff bool) []byte {
	payload := make([]byte, 4)
	if !poweringOff {
		// Full power, no battery, no events.
		payload[0] = 0x80
	}
	return makeMessage(dataClassCommand, messageStatus, flagSystem, sequence, payload)
}

func makeGuideStatus(sequence byte, down bool) []byte {
	status := byte(0)
	if down {
		status = 1
	}
	// GIP Guide Button Status uses the Windows left-logo virtual key.
	return makeMessage(dataClassCommand, 7, flagSystem, sequence, []byte{status, 0x5b})
}

// makeSecurityDataComplete is the small, non-cryptographic completion marker
// used by the documented Windows-PC USB security opt-out. It is intentionally
// limited to the standard two-byte body; this persona never attempts to
// emulate a hardware authentication challenge or certificate exchange.
func makeSecurityDataComplete(sequence byte) []byte {
	return makeMessage(dataClassCommand, messageSecurityData, flagSystem, sequence, []byte{1, 0})
}

func makeOfficialGamepadMetadata() []byte {
	blob := make([]byte, metadataTotalSize)
	binary.LittleEndian.PutUint16(blob[0:2], metadataHeaderSize)
	binary.LittleEndian.PutUint16(blob[2:4], 1)
	binary.LittleEndian.PutUint16(blob[14:16], metadataTotalSize)

	device := blob[metadataHeaderSize : metadataHeaderSize+metadataDeviceSize]
	binary.LittleEndian.PutUint16(device[0:2], metadataDeviceSize)
	binary.LittleEndian.PutUint16(device[2:4], 22)
	binary.LittleEndian.PutUint16(device[4:6], 27)
	binary.LittleEndian.PutUint16(device[6:8], 28)
	binary.LittleEndian.PutUint16(device[8:10], 35)
	binary.LittleEndian.PutUint16(device[10:12], 41)
	binary.LittleEndian.PutUint16(device[12:14], 70)
	device[22] = 1
	binary.LittleEndian.PutUint16(device[23:25], 1)
	binary.LittleEndian.PutUint16(device[25:27], 1)
	copy(device[28:35], []byte{6, 1, 2, 3, 4, 6, 7})
	copy(device[35:41], []byte{5, 1, 4, 5, 6, 10})
	device[41] = 1
	preferredType := []byte("Windows.Xbox.Input.Gamepad")
	binary.LittleEndian.PutUint16(device[42:44], uint16(len(preferredType)))
	copy(device[44:70], preferredType)
	interfaces := [...][16]byte{
		{0x56, 0xff, 0x76, 0x97, 0xfd, 0x9b, 0x81, 0x45, 0xad, 0x45, 0xb6, 0x45, 0xbb, 0xa5, 0x26, 0xd6},
		{0x2c, 0x40, 0x2e, 0x08, 0xdf, 0x07, 0xe1, 0x45, 0xa5, 0xab, 0xa3, 0x12, 0x7a, 0xf1, 0x97, 0xb5},
		{0xe7, 0x1f, 0xf3, 0xb8, 0x86, 0x73, 0xe9, 0x40, 0xa9, 0xf8, 0x2f, 0x21, 0x26, 0x3a, 0xcf, 0xb7},
		// IDevAuthPCOptOut, as documented for controllers on Windows PC USB.
		{0x77, 0xce, 0x34, 0x7a, 0xe2, 0x7d, 0xc6, 0x45, 0x8c, 0xa4, 0x00, 0x42, 0xc0, 0x8b, 0xd9, 0x4a},
	}
	device[70] = byte(len(interfaces))
	for i, identifier := range interfaces {
		copy(device[71+i*16:], identifier[:])
	}

	messages := blob[metadataHeaderSize+metadataDeviceSize:]
	messages[0] = 2
	encodeMetadataMessage(messages[1:1+metadataMessageSize], 0x20, 14, 1<<4)
	encodeMetadataMessage(messages[1+metadataMessageSize:], messageDirectMotor, 9, 1<<3)
	return blob
}

func encodeMetadataMessage(dst []byte, message byte, payloadLength uint16, flags uint32) {
	binary.LittleEndian.PutUint16(dst[0:2], metadataMessageSize)
	dst[2] = message
	binary.LittleEndian.PutUint16(dst[3:5], payloadLength)
	binary.LittleEndian.PutUint16(dst[5:7], 1)
	binary.LittleEndian.PutUint32(dst[7:11], flags)
}

func makeMetadataFragment(sequence byte, data []byte, offset, total int, initial, requestACK bool) []byte {
	if len(data) == 0 || len(data) > metadataFragmentCapacity || offset < 0 || total < offset+len(data) {
		return nil
	}
	flags := byte(flagFragment | flagSystem)
	if initial {
		flags |= flagInitFragment
	}
	if requestACK {
		flags |= flagAcknowledge
	}
	payload := make([]byte, metadataFragmentHeader+len(data))
	payload[0] = messageMetadata
	payload[1] = flags
	payload[2] = sequence
	payload[3] = byte(len(data))
	marker := offset
	if initial {
		marker = total
	}
	payload[4] = 0x80 | byte(marker&0x7f)
	payload[5] = byte(marker >> 7)
	copy(payload[metadataFragmentHeader:], data)
	return payload
}

func makeMetadataComplete(sequence byte, total int) []byte {
	if total < 0 || total > 0x3fff {
		return nil
	}
	// The exact Metadata Complete wire record remains part of the fragmented
	// transfer. Its data length is zero, but the two extension bytes carry the
	// full metadata length (MS-GIPUSB Table 42).
	payload := make([]byte, metadataFragmentHeader)
	payload[0] = messageMetadata
	payload[1] = flagFragment | flagSystem
	payload[2] = sequence
	payload[4] = 0x80 | byte(total&0x7f)
	payload[5] = byte(total >> 7)
	return payload
}

type parsedMessage struct {
	dataClass byte
	message   byte
	flags     byte
	sequence  byte
	payload   []byte
}

func parseMessages(packet []byte, visit func(parsedMessage)) {
	for len(packet) >= gipHeaderSize {
		flags := packet[1]
		payloadLength := int(packet[3])
		if payloadLength > gipMaxPayload || flags&0x80 != 0 || flags&0x40 != 0 || flags&0x08 != 0 {
			return
		}
		total := gipHeaderSize + payloadLength
		if total > len(packet) || packet[2] == 0 {
			return
		}
		visit(parsedMessage{
			dataClass: packet[0] >> 5,
			message:   packet[0] & 0x1f,
			flags:     flags,
			sequence:  packet[2],
			payload:   packet[gipHeaderSize:total],
		})
		packet = packet[total:]
	}
}

func makeACK(message parsedMessage, sequence byte) []byte {
	payload := make([]byte, 9)
	payload[1] = message.dataClass<<5 | (message.message & 0x1f)
	payload[2] = message.flags & (flagSystem | 0x07)
	// Fragment offset is zero and the device advertises its complete 64-byte
	// interrupt buffer for this small controller-only implementation.
	binary.LittleEndian.PutUint16(payload[7:9], gipMaxPacketSize)
	return makeMessage(dataClassCommand, messageProtocolACK, flagSystem, sequence, payload)
}

func parseMetadataACK(message parsedMessage) (int, bool) {
	if message.dataClass != dataClassCommand || message.message != messageProtocolACK ||
		!message.system() || len(message.payload) != 9 || message.payload[0] != 0 {
		return 0, false
	}
	if message.payload[1] != messageMetadata || message.payload[2] != flagSystem {
		return 0, false
	}
	end := binary.LittleEndian.Uint32(message.payload[3:7])
	if end > metadataTotalSize {
		return 0, false
	}
	return int(end), true
}
