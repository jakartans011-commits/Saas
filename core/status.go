package core

// ModuleStatus adalah status module
type ModuleStatus string

const (
ModuleStatusStopped  ModuleStatus = "stopped"
ModuleStatusRunning  ModuleStatus = "running"
ModuleStatusError    ModuleStatus = "error"
ModuleStatusStarting ModuleStatus = "starting"
ModuleStatusStopping ModuleStatus = "stopping"
)
