package main

/*
#include <stdint.h>
#include <stdbool.h>

typedef uintptr_t USBServerHandle;
typedef uintptr_t XboxSeriesDeviceHandle;

typedef struct {
	uint16_t Buttons;
	uint16_t LeftTrigger, RightTrigger;
	int16_t LeftStickX, LeftStickY, RightStickX, RightStickY;
} XboxSeriesDeviceState;

typedef void (*XboxSeriesRumbleCallback)(XboxSeriesDeviceHandle handle,
	uint8_t leftMotor, uint8_t rightMotor);

static void viiper_call_xboxseries_rumble(XboxSeriesRumbleCallback fn,
	XboxSeriesDeviceHandle handle, uint8_t left, uint8_t right) {
	fn(handle, left, right);
}
*/
import "C"

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/cgo"
	"slices"

	"github.com/Alia5/VIIPER/device"
	"github.com/Alia5/VIIPER/device/xboxseries"
	"github.com/Alia5/VIIPER/internal/server/api"
)

// CreateXboxSeriesDevice creates the native Xbox Series X|S USB/IP persona.
//
//export CreateXboxSeriesDevice
func CreateXboxSeriesDevice(
	serverHandle C.USBServerHandle,
	outDeviceHandle *C.XboxSeriesDeviceHandle,
	busID uint32,
	autoAttachLocalhost C.bool,
	idVendor uint16,
	idProduct uint16,
) bool {
	if outDeviceHandle == nil {
		return false
	}
	shw, ok := cgo.Handle(serverHandle).Value().(*usbServerHandleWrapper)
	if !ok || shw == nil {
		return false
	}
	bus := shw.s.GetBus(busID)
	if bus == nil {
		return false
	}
	opts := &device.CreateOptions{}
	if idVendor != 0 {
		opts.IDVendor = &idVendor
	}
	if idProduct != 0 {
		opts.IDProduct = &idProduct
	}
	d, err := xboxseries.New(opts)
	if err != nil {
		return false
	}
	devCtx, err := bus.Add(d)
	if err != nil {
		return false
	}
	exportMeta := device.GetDeviceMeta(devCtx)
	if exportMeta == nil {
		return false
	}
	if autoAttachLocalhost {
		if err := api.AttachLocalhostClient(context.Background(), exportMeta,
			shw.s.GetListenPort(), true, slog.Default()); err != nil {
			return false
		}
	}
	handle := C.XboxSeriesDeviceHandle(cgo.NewHandle(&deviceHandleWrapper{
		device: d, exportMeta: exportMeta, usbServer: shw,
	}))
	*outDeviceHandle = handle
	shw.mtx.Lock()
	shw.deviceHandles[busID] = append(shw.deviceHandles[busID], deviceHandle(handle))
	shw.mtx.Unlock()
	return true
}

// SetXboxSeriesDeviceState updates the native Xbox Series X|S HID state.
//
//export SetXboxSeriesDeviceState
func SetXboxSeriesDeviceState(handle C.XboxSeriesDeviceHandle,
	state C.XboxSeriesDeviceState) bool {
	dhw, ok := cgo.Handle(handle).Value().(*deviceHandleWrapper)
	if !ok || dhw == nil {
		return false
	}
	gamepad, ok := dhw.device.(*xboxseries.XboxSeries)
	if !ok {
		return false
	}
	gamepad.UpdateInputState(xboxseries.InputState{
		Buttons: uint16(state.Buttons), LeftTrigger: uint16(state.LeftTrigger),
		RightTrigger: uint16(state.RightTrigger), LeftStickX: int16(state.LeftStickX),
		LeftStickY: int16(state.LeftStickY), RightStickX: int16(state.RightStickX),
		RightStickY: int16(state.RightStickY),
	})
	return true
}

// SetXboxSeriesRumbleCallback installs the native HID feedback callback.
//
//export SetXboxSeriesRumbleCallback
func SetXboxSeriesRumbleCallback(handle C.XboxSeriesDeviceHandle,
	callback C.XboxSeriesRumbleCallback) bool {
	dhw, ok := cgo.Handle(handle).Value().(*deviceHandleWrapper)
	if !ok || dhw == nil {
		return false
	}
	gamepad, ok := dhw.device.(*xboxseries.XboxSeries)
	if !ok {
		return false
	}
	if callback == nil {
		gamepad.SetRumbleCallback(nil)
		return true
	}
	gamepad.SetRumbleCallback(func(state xboxseries.RumbleState) {
		C.viiper_call_xboxseries_rumble(callback, handle,
			C.uint8_t(state.LeftMotor), C.uint8_t(state.RightMotor))
	})
	return true
}

// RemoveXboxSeriesDevice removes the native Xbox Series X|S device.
//
//export RemoveXboxSeriesDevice
func RemoveXboxSeriesDevice(handle C.XboxSeriesDeviceHandle) bool {
	dhw, ok := cgo.Handle(handle).Value().(*deviceHandleWrapper)
	if !ok || dhw == nil || dhw.usbServer == nil || dhw.exportMeta == nil {
		return false
	}
	if err := dhw.usbServer.s.RemoveDeviceByID(dhw.exportMeta.BusID,
		fmt.Sprintf("%d", dhw.exportMeta.DevID)); err != nil {
		return false
	}
	shw := dhw.usbServer
	shw.mtx.Lock()
	shw.deviceHandles[dhw.exportMeta.BusID] = slices.DeleteFunc(
		shw.deviceHandles[dhw.exportMeta.BusID],
		func(candidate deviceHandle) bool { return candidate == deviceHandle(handle) })
	shw.mtx.Unlock()
	cgo.Handle(handle).Delete()
	return true
}
