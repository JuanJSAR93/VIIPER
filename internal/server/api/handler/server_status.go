package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"

	"github.com/Alia5/VIIPER/internal/server/api"
	apierror "github.com/Alia5/VIIPER/internal/server/api/error"
	"github.com/Alia5/VIIPER/internal/server/usb"
	"github.com/Alia5/VIIPER/viipertypes"
)

// ServerStatus returns the live server state known at VIIPER's ownership
// boundaries. usbipImported means the USB/IP server has an active import
// connection; inputStreamActive means the feeder/API stream is connected.
// Neither field claims that a remote Windows client has finished PnP setup.
func ServerStatus(usbServer *usb.Server, apiServer *api.Server) api.HandlerFunc {
	return func(_ *api.Request, res *api.Response, _ *slog.Logger) error {
		if usbServer == nil || apiServer == nil {
			return apierror.ErrInternal("VIIPER server status is unavailable")
		}

		busIDs := usbServer.ListBuses()
		sort.Slice(busIDs, func(i, j int) bool { return busIDs[i] < busIDs[j] })
		status := viipertypes.ServerStatusResponse{
			Server:    "VIIPER",
			State:     "running",
			USBIPAddr: usbServer.Addr(),
			APIAddr:   apiServer.Addr(),
			Buses:     make([]viipertypes.ServerStatusBus, 0, len(busIDs)),
		}

		for _, busID := range busIDs {
			bus := usbServer.GetBus(busID)
			if bus == nil {
				continue
			}
			metas := bus.GetAllDeviceMetas()
			busStatus := viipertypes.ServerStatusBus{
				BusID:   busID,
				Devices: make([]viipertypes.ServerStatusDevice, 0, len(metas)),
			}
			for _, meta := range metas {
				descriptor, err := usbServer.SnapshotDeviceDescriptorForStatus(meta)
				if err != nil || descriptor == nil {
					return apierror.ErrConflict(fmt.Sprintf(
						"device %d changed during status inspection: %v",
						meta.Meta.DevID, err))
				}
				imported := usbServer.IsDeviceImported(meta.Dev)
				streamActive := apiServer.IsDeviceStreamActive(meta)
				if imported {
					status.ActiveImports++
				}
				if streamActive {
					status.ActiveStreams++
				}
				busStatus.Devices = append(busStatus.Devices,
					viipertypes.ServerStatusDevice{
						BusID:             busID,
						DevID:             fmt.Sprintf("%d", meta.Meta.DevID),
						Vid:               fmt.Sprintf("0x%04x", descriptor.Device.IDVendor),
						Pid:               fmt.Sprintf("0x%04x", descriptor.Device.IDProduct),
						Type:              inferDeviceType(meta.Dev),
						DeviceSpecific:    meta.Dev.GetDeviceSpecificArgs(),
						USBIPImported:     imported,
						InputStreamActive: streamActive,
					})
			}
			sort.Slice(busStatus.Devices, func(i, j int) bool {
				return busStatus.Devices[i].DevID < busStatus.Devices[j].DevID
			})
			status.Buses = append(status.Buses, busStatus)
		}

		payload, err := json.Marshal(status)
		if err != nil {
			return apierror.ErrInternal(fmt.Sprintf(
				"failed to marshal server status: %v", err))
		}
		res.JSON = string(payload)
		return nil
	}
}
