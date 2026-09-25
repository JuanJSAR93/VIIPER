package main

/*
#include <stdint.h>
#include <stdlib.h>
#include <stdbool.h>

typedef uintptr_t USBServerHandle;
typedef uintptr_t XboxOneDeviceHandle;

#define VIIPER_XBOXONE_PROFILE_ONE     0
#define VIIPER_XBOXONE_PROFILE_SERIES  1

typedef struct {
	uint8_t Menu, View, A, B, X, Y;
	uint8_t DPadUp, DPadDown, DPadLeft, DPadRight;
	uint8_t LeftBumper, RightBumper, LeftStickButton, RightStickButton;
	uint8_t Guide, Share;
	uint16_t LeftTrigger, RightTrigger;
	int16_t LeftStickX, LeftStickY, RightStickX, RightStickY;
} XboxOneDeviceState;

// The callback receives the canonical GIP Direct Motor fields. Values are
// percentages (0..100), exactly as defined by MS-GIPUSB, not an invented
// XInput-specific conversion.
typedef void (*XboxOneOutputCallback)(
	XboxOneDeviceHandle handle,
	uint8_t action,
	uint8_t leftImpulse, uint8_t rightImpulse,
	uint8_t leftVibration, uint8_t rightVibration,
	uint8_t duration, uint8_t delay, uint8_t repeat,
	uint8_t guidePattern, uint8_t guideIntensity);

static void viiper_call_xboxone_output(
	XboxOneOutputCallback fn, XboxOneDeviceHandle handle, uint8_t action,
	uint8_t leftImpulse, uint8_t rightImpulse,
	uint8_t leftVibration, uint8_t rightVibration,
	uint8_t duration, uint8_t delay, uint8_t repeat,
	uint8_t guidePattern, uint8_t guideIntensity) {
	fn(handle, action, leftImpulse, rightImpulse, leftVibration, rightVibration,
		duration, delay, repeat, guidePattern, guideIntensity);
}
*/
import "C"

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/cgo"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Alia5/VIIPER/controllerfeedback"
	"github.com/Alia5/VIIPER/device/xboxone"
	"github.com/Alia5/VIIPER/internal/registry"
	"github.com/Alia5/VIIPER/internal/server/api"
)

const (
	xboxOneVID    uint16 = 0x045e
	xboxOnePID    uint16 = 0x02ea
	xboxSeriesPID uint16 = 0x0b12
)

var xboxOneDeviceSequence atomic.Uint64

type xboxOneLibraryDevice struct {
	registration *registry.AuthorizedXboxOneRetainedUSBRegistration
	executor     *xboxOneLibraryExecutor
	inputMu      sync.Mutex
	revision     atomic.Uint64
}

type xboxOneLibraryExecutor struct {
	mu       sync.RWMutex
	callback C.XboxOneOutputCallback
	handle   C.XboxOneDeviceHandle
	stopped  bool
}

func (e *xboxOneLibraryExecutor) emit(execution xboxone.ControllerPersonaLocalExecution) {
	e.mu.RLock()
	callback, handle, stopped := e.callback, e.handle, e.stopped
	e.mu.RUnlock()
	if callback == nil || stopped {
		return
	}
	switch execution.Action {
	case xboxone.ControllerPersonaApplyDirectMotor:
		m := execution.DirectMotor
		C.viiper_call_xboxone_output(callback, handle, C.uint8_t(execution.Action),
			C.uint8_t(m.LeftImpulse), C.uint8_t(m.RightImpulse),
			C.uint8_t(m.LeftVibration), C.uint8_t(m.RightVibration),
			C.uint8_t(m.Duration), C.uint8_t(m.Delay), C.uint8_t(m.Repeat), 0, 0)
	case xboxone.ControllerPersonaApplyGuideLED:
		led := execution.GuideLED
		C.viiper_call_xboxone_output(callback, handle, C.uint8_t(execution.Action),
			0, 0, 0, 0, 0, 0, 0, C.uint8_t(led.Pattern), C.uint8_t(led.Intensity))
	}
}

func (e *xboxOneLibraryExecutor) Execute(
	execution xboxone.ControllerPersonaLocalExecution, _ time.Time,
) error {
	e.emit(execution)
	return nil
}

func (e *xboxOneLibraryExecutor) ResetAndDrain(time.Time) error {
	e.mu.Lock()
	e.stopped = true
	e.mu.Unlock()
	return nil
}

func (e *xboxOneLibraryExecutor) ResetNeutral(
	execution xboxone.ControllerPersonaLocalExecution, _ time.Time,
) error {
	e.mu.Lock()
	e.stopped = false
	e.mu.Unlock()
	e.emit(execution)
	return nil
}

func (e *xboxOneLibraryExecutor) CancelAndDrain(time.Time) error {
	e.mu.Lock()
	e.stopped = true
	e.mu.Unlock()
	return nil
}

func (e *xboxOneLibraryExecutor) DisconnectNeutral(
	execution xboxone.ControllerPersonaLocalExecution, _ time.Time,
) error {
	e.emit(execution)
	return nil
}

func nextXboxOneDeviceID() uint64 {
	// MS-GIPUSB primary Device IDs use the 0x0000fffb prefix. The low word is
	// unique for the process while the timestamp makes separate processes very
	// unlikely to reuse an identity.
	serial := uint64(time.Now().UnixNano()) ^ xboxOneDeviceSequence.Add(1)
	return 0x0000fffb00000000 | (serial & 0xffffffff)
}

func xboxOneStateFromC(state C.XboxOneDeviceState) xboxone.InputStateV1 {
	return xboxone.InputStateV1{
		Menu: state.Menu != 0, View: state.View != 0,
		A: state.A != 0, B: state.B != 0, X: state.X != 0, Y: state.Y != 0,
		DPadUp: state.DPadUp != 0, DPadDown: state.DPadDown != 0,
		DPadLeft: state.DPadLeft != 0, DPadRight: state.DPadRight != 0,
		LeftBumper: state.LeftBumper != 0, RightBumper: state.RightBumper != 0,
		LeftStickButton: state.LeftStickButton != 0, RightStickButton: state.RightStickButton != 0,
		Guide: state.Guide != 0, Share: state.Share != 0,
		LeftTrigger: uint16(state.LeftTrigger), RightTrigger: uint16(state.RightTrigger),
		LeftStickX: int16(state.LeftStickX), LeftStickY: int16(state.LeftStickY),
		RightStickX: int16(state.RightStickX), RightStickY: int16(state.RightStickY),
	}
}

func xboxOneIdentity(profile C.uint8_t, deviceID uint64) (xboxone.ControllerIdentity, xboxone.ControllerUSBIdentityStrings) {
	pid := xboxOnePID
	product := "VIIPER Xbox One Controller"
	if profile == C.VIIPER_XBOXONE_PROFILE_SERIES {
		pid = xboxSeriesPID
		product = "VIIPER Xbox Series X|S Controller"
	}
	return xboxone.ControllerIdentity{
		VendorID: xboxOneVID, ProductID: pid, DeviceReleaseBCD: 0x0100,
		DeviceID: deviceID, Firmware: xboxone.FirmwareVersion{Major: 1, Build: 1},
		HardwareMajor: 1,
	}, xboxone.ControllerUSBIdentityStrings{
		Manufacturer: "©Microsoft Corporation", Product: product,
		Serial: fmt.Sprintf("%016xA1B2C3D4E5F60706", deviceID),
	}
}

// CreateXboxOneDevice creates an official GIP/XGIP gamepad persona using the
// same retained USB engine used by the server's production Xbox path. Profile
// 0 is Xbox One (045E:02EA); profile 1 is Xbox Series X|S (045E:0B12).
// The API intentionally uses canonical 14-byte base metadata: it is the most
// broadly compatible official Windows gamepad shape.
//
//export CreateXboxOneDevice
func CreateXboxOneDevice(
	serverHandle C.USBServerHandle,
	outDeviceHandle *C.XboxOneDeviceHandle,
	busID uint32,
	profile C.uint8_t,
	autoAttachLocalhost C.bool,
) bool {
	if outDeviceHandle == nil || profile > C.VIIPER_XBOXONE_PROFILE_SERIES {
		return false
	}
	serverHandleValue := cgo.Handle(serverHandle)
	shw, ok := serverHandleValue.Value().(*usbServerHandleWrapper)
	if !ok || shw == nil || shw.s.GetBus(busID) == nil {
		return false
	}
	authorityID, ok := shw.s.RetainedImportAuthorityID()
	if !ok {
		return false
	}
	protocolTime, ok := controllerfeedback.HostMonotonicMicroseconds()
	if !ok {
		return false
	}
	deviceID := nextXboxOneDeviceID()
	identity, strings := xboxOneIdentity(profile, deviceID)
	profileDescriptor, err := xboxone.NewUnregisteredControllerProfile(identity,
		xboxone.ControllerUSBConfig{MaxPower2mA: 250, OUTIntervalMS: 4, INIntervalMS: 4})
	if err != nil {
		return false
	}
	metadata, err := profileDescriptor.BindOfficialGamepadMetadataV1(xboxone.OfficialGamepadMetadataBase)
	if err != nil {
		return false
	}
	config := xboxone.ControllerPersonaConfig{
		Profile: profileDescriptor, Metadata: metadata,
		CurrentInput:      xboxone.GamepadInputReportV1{},
		CurrentStatus:     xboxone.NewWiredNoBatteryStatus(false),
		PoweringOffStatus: xboxone.NewWiredNoBatteryStatus(true),
	}
	authorization, err := xboxone.NewAuthorizedControllerPersonaConfig(config, strings,
		xboxone.ControllerIdentityAuthorizationGranted)
	if err != nil {
		return false
	}
	executor := &xboxOneLibraryExecutor{}
	registration, err := registry.RegisterAuthorizedXboxOneRetainedUSB(
		shw.s, registry.AuthorizedXboxOneRetainedUSBRequest{
			BusID: busID, AuthorityID: authorityID, DeviceID: deviceID,
			ProtocolTimeMilliseconds: protocolTime / 1000,
			Authorization:            authorization, LocalExecutor: executor,
			LocalTimeout: 100 * time.Millisecond,
		})
	if err != nil {
		return false
	}
	meta, active := registration.DeviceMeta()
	if !active {
		_ = registration.Close()
		return false
	}
	device := &xboxOneLibraryDevice{registration: registration, executor: executor}
	handle := cgo.NewHandle(&deviceHandleWrapper{
		device: device, exportMeta: &meta.Meta, usbServer: shw,
	})
	deviceHandleValue := C.XboxOneDeviceHandle(handle)
	executor.handle = deviceHandleValue
	// Revision 2 is the canonical first semantic frame for a retained import.
	var neutral [xboxone.SemanticInputWireSize]byte
	if err := xboxone.EncodeSemanticInputWireV1Into(neutral[:], xboxone.InputStateV1{}); err != nil ||
		registration.PublishSemanticInputWire(2, neutral[:]) != nil {
		_ = registration.Close()
		handle.Delete()
		return false
	}
	device.revision.Store(2)
	if autoAttachLocalhost {
		if err := api.AttachLocalhostClient(context.Background(), &meta.Meta,
			shw.s.GetListenPort(), true, slog.Default()); err != nil {
			_ = registration.Close()
			handle.Delete()
			return false
		}
	}
	*outDeviceHandle = deviceHandleValue
	shw.mtx.Lock()
	shw.deviceHandles[busID] = append(shw.deviceHandles[busID], deviceHandle(handle))
	shw.mtx.Unlock()
	return true
}

// SetXboxOneDeviceState publishes a transport-neutral state through the
// retained semantic ingress. GIP packet framing, sequencing, metadata, and
// XGIP lifecycle remain owned by the canonical Xbox engine.
//
//export SetXboxOneDeviceState
func SetXboxOneDeviceState(handle C.XboxOneDeviceHandle, state C.XboxOneDeviceState) bool {
	dh := cgo.Handle(handle)
	dhw, ok := dh.Value().(*deviceHandleWrapper)
	if !ok {
		return false
	}
	device, ok := dhw.device.(*xboxOneLibraryDevice)
	if !ok || device.registration == nil {
		return false
	}
	device.inputMu.Lock()
	defer device.inputMu.Unlock()
	var wire [xboxone.SemanticInputWireSize]byte
	if err := xboxone.EncodeSemanticInputWireV1Into(wire[:], xboxOneStateFromC(state)); err != nil {
		return false
	}
	revision := device.revision.Add(1)
	if err := device.registration.PublishSemanticInputWire(revision, wire[:]); err != nil {
		device.revision.Add(^uint64(0))
		return false
	}
	return true
}

// SetXboxOneOutputCallback receives canonical GIP motor and Guide LED actions.
// Pass NULL to disable callbacks; the protocol path remains fully functional.
//
//export SetXboxOneOutputCallback
func SetXboxOneOutputCallback(handle C.XboxOneDeviceHandle, callback C.XboxOneOutputCallback) bool {
	dh := cgo.Handle(handle)
	dhw, ok := dh.Value().(*deviceHandleWrapper)
	if !ok {
		return false
	}
	device, ok := dhw.device.(*xboxOneLibraryDevice)
	if !ok {
		return false
	}
	device.executor.mu.Lock()
	device.executor.callback = callback
	device.executor.mu.Unlock()
	return true
}

// RemoveXboxOneDevice removes only this exact retained registration.
//
//export RemoveXboxOneDevice
func RemoveXboxOneDevice(handle C.XboxOneDeviceHandle) bool {
	dh := cgo.Handle(handle)
	dhw, ok := dh.Value().(*deviceHandleWrapper)
	if !ok {
		return false
	}
	device, ok := dhw.device.(*xboxOneLibraryDevice)
	if !ok || device.registration == nil {
		return false
	}
	if err := device.registration.Close(); err != nil {
		return false
	}
	shw := dhw.usbServer
	busID := dhw.exportMeta.BusID
	shw.mtx.Lock()
	shw.deviceHandles[busID] = slices.DeleteFunc(shw.deviceHandles[busID], func(h deviceHandle) bool {
		return h == deviceHandle(handle)
	})
	shw.mtx.Unlock()
	dh.Delete()
	return true
}
