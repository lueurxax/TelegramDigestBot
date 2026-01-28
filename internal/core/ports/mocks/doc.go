// Package mocks provides test doubles for ports interfaces.
//
// These mocks are designed to be simple, thread-safe, in-memory implementations
// suitable for unit testing. Each mock provides:
//
//   - Default behavior that returns reasonable test values
//   - Callback functions (xxxFn) for customizing behavior per test
//   - Helper methods for setting state directly
//   - Clear/Reset methods for test isolation
//
// # Usage Example
//
//	func TestMyService(t *testing.T) {
//		settings := mocks.NewSettingsStore()
//		settings.Set("my_key", true)
//
//		svc := NewService(settings)
//		// ... test service behavior
//	}
//
// # Available Mocks
//
//   - SettingsStore: implements ports.SettingsStore
//   - CacheRepository: implements ports.CacheRepository
//   - BudgetRepository: implements ports.BudgetRepository
//
// Additional mocks can be added as needed following the same patterns.
package mocks
