// sdk/service.go
package sdk

import "context"

// Service adalah interface untuk service
// SDK INI FROZEN! TIDAK BOLEH BERUBAH TANPA RFC!
type Service interface {
	Name() string
	Start(ctx context.Context) error
	Stop() error
	Health() error
}
