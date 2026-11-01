// tests/test_modules/malicious.go
package test_modules

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"trailseal/sdk"
)

// MaliciousType adalah jenis behavior yang digunakan untuk menguji
// defensive layer/runtime.
type MaliciousType string

const (
	MaliciousPrivilegeEscalation MaliciousType = "privilege_escalation"
	MaliciousResourceExhaustion  MaliciousType = "resource_exhaustion"
	MaliciousDataExfiltration    MaliciousType = "data_exfiltration"
	MaliciousPanic               MaliciousType = "panic"
	MaliciousDeadlock            MaliciousType = "deadlock"
	MaliciousInfiniteLoop        MaliciousType = "infinite_loop"
)

// MaliciousModule adalah test module yang mensimulasikan behavior
// tidak terpercaya tanpa melakukan aksi destruktif terhadap host.
type MaliciousModule struct {
	name  string
	mType MaliciousType

	logger   sdk.Logger
	eventBus sdk.EventBus
	storage  sdk.Storage

	isRunning atomic.Bool

	stopCh   chan struct{}
	stopOnce sync.Once

	wg sync.WaitGroup

	mu sync.Mutex
}

// NewMaliciousModule membuat malicious test module baru.
func NewMaliciousModule(
	name string,
	mType MaliciousType,
) *MaliciousModule {
	return &MaliciousModule{
		name:   name,
		mType:  mType,
		stopCh: make(chan struct{}),
	}
}

func (m *MaliciousModule) Name() string {
	return m.name
}

func (m *MaliciousModule) Version() string {
	return "1.0.0"
}

func (m *MaliciousModule) Manifest() sdk.Manifest {
	manifest := sdk.Manifest{
		Name:          m.name,
		Version:       "1.0.0",
		SchemaVersion: "1.0.0",
		Category:      "malicious",
		Capabilities: []string{
			"test.malicious.execute",
		},
		Permissions: []string{
			"system",
		},
		Priority: 10,
	}

	// Untuk test capability validation, behavior ini sengaja
	// mendeklarasikan capability tambahan yang seharusnya ditolak
	// oleh policy/validator.
	if m.mType == MaliciousPrivilegeEscalation {
		manifest.Capabilities = []string{
			"test.malicious.execute",
			"security.admin.delete",
		}
	}

	return manifest
}

func (m *MaliciousModule) Init(config sdk.Config) error {
	return nil
}

// Start menjalankan behavior test dalam goroutine terisolasi.
//
// Setiap goroutine menggunakan safeGo() sehingga panic dari behavior
// test tidak menjatuhkan seluruh test process.
func (m *MaliciousModule) Start(ctx context.Context) error {
	if !m.isRunning.CompareAndSwap(false, true) {
		return nil
	}

	switch m.mType {
	case MaliciousPrivilegeEscalation:
		m.safeGo("privilegeEscalation", func() {
			m.privilegeEscalation(ctx)
		})

	case MaliciousResourceExhaustion:
		m.safeGo("resourceExhaustion", func() {
			m.resourceExhaustion(ctx)
		})

	case MaliciousDataExfiltration:
		m.safeGo("dataExfiltration", func() {
			m.dataExfiltration(ctx)
		})

	case MaliciousPanic:
		m.safeGo("panicAttack", func() {
			m.panicAttack(ctx)
		})

	case MaliciousDeadlock:
		m.safeGo("deadlockAttack", func() {
			m.deadlockAttack(ctx)
		})

	case MaliciousInfiniteLoop:
		m.safeGo("infiniteLoop", func() {
			m.infiniteLoop(ctx)
		})

	default:
		m.isRunning.Store(false)

		return fmt.Errorf(
			"unknown malicious type: %q",
			m.mType,
		)
	}

	return nil
}

// safeGo menjalankan behavior dalam goroutine dengan panic recovery.
//
// Recover harus berada di goroutine yang sama dengan panic.
func (m *MaliciousModule) safeGo(
	behaviorName string,
	fn func(),
) {
	m.wg.Add(1)

	go func() {
		defer m.wg.Done()

		defer func() {
			if recovered := recover(); recovered != nil {
				m.isRunning.Store(false)

				m.logError(
					fmt.Sprintf(
						"malicious test behavior recovered panic: module=%s behavior=%s panic=%v",
						m.name,
						behaviorName,
						recovered,
					),
				)
			}
		}()

		fn()
	}()
}

// Stop menghentikan seluruh behavior.
//
// Aman dipanggil berkali-kali.
func (m *MaliciousModule) Stop() error {
	m.stopOnce.Do(func() {
		m.isRunning.Store(false)
		close(m.stopCh)
	})

	m.wg.Wait()

	return nil
}

func (m *MaliciousModule) Health() error {
	if !m.isRunning.Load() {
		return fmt.Errorf("module not running")
	}

	return nil
}

// privilegeEscalation mensimulasikan percobaan capability escalation.
//
// Tidak melakukan privileged operation nyata.
func (m *MaliciousModule) privilegeEscalation(
	ctx context.Context,
) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.isRunning.Store(false)
			return

		case <-m.stopCh:
			m.isRunning.Store(false)
			return

		case <-ticker.C:
			if m.eventBus != nil {
				m.eventBus.Publish(sdk.Event{
					Type:   "security.admin.delete",
					Source: m.name,
					Data: map[string]interface{}{
						"user":   "admin",
						"action": "delete",
					},
				})
			}
		}
	}
}

// resourceExhaustion mensimulasikan tekanan resource secara TERBATAS.
//
// Jangan membuat test benar-benar mengalokasikan RAM tanpa batas.
// Tujuan test adalah memverifikasi bahwa runtime dapat menghentikan
// behavior resource-intensive.
func (m *MaliciousModule) resourceExhaustion(
	ctx context.Context,
) {
	const (
		maxAllocations = 16
		allocationSize = 64 * 1024
	)

	allocations := make([][]byte, 0, maxAllocations)

	for i := 0; i < maxAllocations; i++ {
		select {
		case <-ctx.Done():
			m.isRunning.Store(false)
			return

		case <-m.stopCh:
			m.isRunning.Store(false)
			return

		default:
		}

		allocations = append(
			allocations,
			make([]byte, allocationSize),
		)

		time.Sleep(10 * time.Millisecond)
	}

	// Pertahankan allocation selama behavior dianggap running,
	// kemudian berhenti ketika context atau Stop() dipanggil.
	select {
	case <-ctx.Done():
	case <-m.stopCh:
	}

	// Clear references.
	for i := range allocations {
		allocations[i] = nil
	}

	m.isRunning.Store(false)
}

// dataExfiltration mensimulasikan percobaan membaca namespace tenant
// lain. Storage implementation yang sebenarnya bertanggung jawab
// menolak akses tersebut.
func (m *MaliciousModule) dataExfiltration(
	ctx context.Context,
) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.isRunning.Store(false)
			return

		case <-m.stopCh:
			m.isRunning.Store(false)
			return

		case <-ticker.C:
			if m.storage == nil {
				continue
			}

			var data interface{}

			// Sengaja menggunakan path tenant lain untuk menguji
			// isolation boundary.
			_ = m.storage.Load(
				"tenant_a/sensitive_data",
				&data,
			)
		}
	}
}

// panicAttack sengaja menghasilkan panic.
//
// safeGo() harus menangkap panic ini sehingga test process tetap hidup.
func (m *MaliciousModule) panicAttack(
	ctx context.Context,
) {
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		m.isRunning.Store(false)
		return

	case <-m.stopCh:
		m.isRunning.Store(false)
		return

	case <-timer.C:
		panic("intentional panic from malicious test module")
	}
}

// deadlockAttack mensimulasikan behavior yang memegang mutex.
//
// Behavior tetap cancellable agar integration test tidak hang.
func (m *MaliciousModule) deadlockAttack(
	ctx context.Context,
) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.isRunning.Store(false)
			return

		case <-m.stopCh:
			m.isRunning.Store(false)
			return

		case <-ticker.C:
			// Sengaja tidak melakukan apa pun.
			// Mutex tetap dipegang untuk mensimulasikan contention.
		}
	}
}

// infiniteLoop mensimulasikan CPU-intensive behavior tetapi tetap
// cancellable sehingga test suite tidak hang.
func (m *MaliciousModule) infiniteLoop(
	ctx context.Context,
) {
	runtimeYield := time.NewTicker(1 * time.Millisecond)
	defer runtimeYield.Stop()

	for {
		select {
		case <-ctx.Done():
			m.isRunning.Store(false)
			return

		case <-m.stopCh:
			m.isRunning.Store(false)
			return

		case <-runtimeYield.C:
			// Sengaja melakukan sedikit pekerjaan lalu yield.
		}
	}
}

func (m *MaliciousModule) logError(message string) {
	if m.logger != nil {
		m.logger.Error(message)
	}
}

// SetLogger memasang logger.
func (m *MaliciousModule) SetLogger(logger sdk.Logger) {
	m.logger = logger
}

// SetEventBus memasang event bus.
func (m *MaliciousModule) SetEventBus(eventBus sdk.EventBus) {
	m.eventBus = eventBus
}

// SetStorage memasang storage.
func (m *MaliciousModule) SetStorage(storage sdk.Storage) {
	m.storage = storage
}
