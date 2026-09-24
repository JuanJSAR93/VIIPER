// Package xboxone contains the shared Xbox One/Series semantic model plus two
// output personas: the retained MS-GIP/XInput path and the HID/DirectInput
// compatibility path used by joy.cpl. Both personas intentionally live in
// this package so button and axis mapping can be tested from one source.
//
// The implemented wire surface includes strict four-byte single-packet
// headers, exact standard Gamepad Input and Direct Motor messages, the Guide
// LED command, an offline transactional controller-only EP0 state machine,
// explicitly identified but unregistered controller USB descriptors, the
// primary Hello, identity-bound transactional metadata response framing,
// the event-free Extended Status form, transactional sequence allocation, and
// a pure transactional controller startup lifecycle. ControllerPersonaEngine
// composes those pieces with exact input emission and strict Direct Motor /
// Guide LED receive classification under one offline generation fence. A
// separate dormant owner can transfer a decoded packet to one whole-vector
// atomic participant. The canonical persona-backed batch composition admits
// any Direct Motor / Guide LED vector plus at most one exact lifecycle or ACK
// member; multiple context-bearing members and production transport
// integration remain blocked rather than using preview copies or prefix commit.
//
// This package does not register a USB device, select or grant a VID/PID,
// invent identity strings or metadata, encode status events, bind a Windows
// driver, perform backend I/O, or perform authentication. A dormant one-shot
// gate can privately pre-encode exact caller-supplied strings for one exact
// offline engine; it is caller attestation, not ownership or compatibility
// proof. Explicit capability blockers remain non-zero until all other external
// requirements and hardware-conformance gates are satisfied.
// Those boundaries remain fail-closed or explicit typed seams.
package xboxone
