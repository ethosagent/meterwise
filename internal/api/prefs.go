package api

import "github.com/ethosagent/meterwise/internal/contract"

// DefaultPreferences is what a user who never changed anything gets.
func DefaultPreferences() contract.Preferences {
	return contract.Preferences{Theme: contract.ThemeSystem}
}
