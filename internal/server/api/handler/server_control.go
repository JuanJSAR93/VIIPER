package handler

import (
	"log/slog"

	"github.com/Alia5/VIIPER/internal/server/api"
	apierror "github.com/Alia5/VIIPER/internal/server/api/error"
)

func ServerShutdown(shutdown func()) api.HandlerFunc {
	return serverLifecycleCommand("shutdown", shutdown)
}

func ServerRestart(restart func()) api.HandlerFunc {
	return serverLifecycleCommand("restart", restart)
}

func serverLifecycleCommand(action string, request func()) api.HandlerFunc {
	return func(req *api.Request, res *api.Response, _ *slog.Logger) error {
		if request == nil {
			return apierror.ErrConflict("server lifecycle control is unavailable")
		}
		if req == nil || req.Ctx == nil {
			return apierror.ErrBadRequest("missing request context")
		}
		res.JSON = `{"accepted":true,"action":"` + action + `"}`
		res.AfterWrite = request
		return nil
	}
}
