// core/bootstrap/test_modules/throughput/throughput.go
package throughput

import (
	"context"
	"time"

	"trailseal/sdk"
)

// ThroughputModule adalah test module untuk menguji
// event publishing dengan frekuensi tinggi secara terkendali.
type ThroughputModule struct {
	name     string
	eventBus sdk.EventBus
	logger   sdk.Logger
}

func NewThroughputModule(
	name string,
	eventBus sdk.EventBus,
	logger sdk.Logger,
) *ThroughputModule {
	return &ThroughputModule{
		name:     name,
		eventBus: eventBus,
		logger:   logger,
	}
}

func (m *ThroughputModule) Name() string {
	return m.name
}

func (m *ThroughputModule) Version() string {
	return "2.0.0"
}

func (m *ThroughputModule) Manifest() sdk.Manifest {
	return sdk.Manifest{
		Name:    m.name,
		Version: "2.0.0",
		Capabilities: []string{
			"event.publish.high_frequency",
			"event.subscribe.batch",
		},
		Permissions: []string{
			"event",
		},
		Category: "throughput",
	}
}

func (m *ThroughputModule) Init(config sdk.Config) error {
	return nil
}

func (m *ThroughputModule) Start(ctx context.Context) error {
	if m.eventBus == nil {
		return nil
	}

	ticker := time.NewTicker(100 * time.Microsecond)

	go func() {
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return

			case <-ticker.C:
				m.eventBus.PublishAsync(sdk.Event{
					Type: "throughput.event",
					Data: map[string]interface{}{
						"timestamp": time.Now().UnixNano(),
					},
				})
			}
		}
	}()

	return nil
}

func (m *ThroughputModule) Stop() error {
	return nil
}

func (m *ThroughputModule) Health() error {
	return nil
}

func (m *ThroughputModule) SetEventBus(eventBus sdk.EventBus) {
	m.eventBus = eventBus
}

func (m *ThroughputModule) SetLogger(logger sdk.Logger) {
	m.logger = logger
}
