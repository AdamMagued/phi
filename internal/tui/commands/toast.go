package commands

import (
	"time"

	"github.com/pulseaiclub/phi/internal/components/toast"
	"github.com/pulseaiclub/phi/internal/tui/controller"
)

// publishToast sends a toast message via the bus.
func publishToast(bus *controller.Bus, msg string, kind toast.ToastKind, d time.Duration) {
	bus.Publish(controller.ToastMsg{Message: msg, Kind: kind, Duration: d})
}
