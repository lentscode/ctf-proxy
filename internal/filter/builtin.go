package filter

import "fmt"

// RegisterBuiltins registers every filter compiled into the proxy binary.
//
// Add a Registry.Register call here when introducing a new Go filter. Keeping
// registration in the filter package makes the set of compiled filters explicit
// and keeps the data-plane packages independent of individual implementations.
func RegisterBuiltins(registry *Registry) error {
	if registry == nil {
		return fmt.Errorf("register built-in filters: nil registry")
	}

	// Example for a future filter:
	//
	// if err := registry.Register("block-admin", func() (Filter, error) {
	// 	return AdminBlocker{}, nil
	// }); err != nil {
	// 	return err
	// }

	return nil
}
