package dashboard

import (
	"errors"
	"net/http"
	"sync/atomic"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

func init() {
	caddy.RegisterModule(caddyModule{})
}

// active is the handler the Caddy module serves. Caddy builds modules from
// JSON config, so the running dashboard is handed over through this variable
// rather than through the config.
var active atomic.Pointer[Handler]

// Activate makes h the dashboard Caddy serves.
func Activate(h *Handler) {
	active.Store(h)
}

// caddyModule plugs the dashboard into the embedded Caddy as the
// "tero_dashboard" HTTP handler, so requests reach it in-process with the
// real client address.
type caddyModule struct{}

func (caddyModule) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.tero_dashboard",
		New: func() caddy.Module { return caddyModule{} },
	}
}

func (caddyModule) ServeHTTP(w http.ResponseWriter, r *http.Request, _ caddyhttp.Handler) error {
	h := active.Load()
	if h == nil {
		return caddyhttp.Error(http.StatusServiceUnavailable, errors.New("dashboard not ready"))
	}

	h.ServeHTTP(w, r)
	return nil
}

var _ caddyhttp.MiddlewareHandler = caddyModule{}
