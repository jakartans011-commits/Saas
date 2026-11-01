// core/lifecycle/state.go
package lifecycle

// AllowedTransitions adalah state transition yang diizinkan
var AllowedTransitions = map[State][]State{
	StateCreated:  {StateInit, StateDestroy},
	StateInit:     {StateValid, StateInvalid, StateError},
	StateValid:    {StateStarting, StateError},
	StateInvalid:  {StateInit, StateDestroy, StateError},
	StateStarting: {StateRunning, StateError},
	StateRunning:  {StatePausing, StateStopping, StateError},
	StatePausing:  {StatePaused, StateRunning, StateError},
	StatePaused:   {StateResuming, StateStopping, StateError},
	StateResuming: {StateRunning, StateError},
	StateStopping: {StateStopped, StateError},
	StateStopped:  {StateStarting, StateDestroy, StateError},
	StateDestroy:  {},
	StateError:    {StateStopped, StateDestroy},
}

// IsTransitionAllowed checks if transition is allowed
func IsTransitionAllowed(from, to State) bool {
	allowed, ok := AllowedTransitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}