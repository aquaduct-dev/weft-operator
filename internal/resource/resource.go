// Package resource provides a small reconciliation helper that centralizes
// the common pattern of ensuring a resource exists (or not), is up-to-date,
// and is created/updated/deleted accordingly.
//
// File purpose: implement Resource(options) which performs the reconciliation
// logic given a set of function callbacks. Each file in this repository should
// include helpful log statements to aid debugging.
package resource

import "fmt"

// Options bundles a set of callback functions that describe and operate on a
// resource. Each callback is intentionally simple so callers can provide
// closures capturing controller context, clients, and objects.
type Options struct {
	// Name returns a human-friendly name for logs.
	Name string

	// Log is used for debug/trace logging. Accepts variadic args similar to
	// fmt.Print* so callers can pass strings and structured values.
	Log func(v ...any)

	// Exists returns true if the resource currently exists in the cluster (or
	// whatever backing store).
	Exists func() bool

	// ShouldExist returns true if the desired state requires the resource to
	// exist.
	ShouldExist func() bool

	// IsUpToDate returns true if the existing resource already matches the
	// desired state (so no update is needed).
	IsUpToDate func() bool

	// Create attempts to create the resource. Returns true on success.
	Create func() error

	// Update attempts to update the resource. Returns true on success.
	Update func() error

	// Delete attempts to delete the resource. Returns true on success.
	Delete func() error
}

// Resource performs reconciliation for a single resource described by opts.
// It returns (changed bool) indicating whether a mutation was attempted and
// (ok bool) indicating whether the reconciliation succeeded for the attempted
// operation. If no mutation was needed, (changed == false, ok == true).
//
// Behaviour summary:
// - If resource does not exist and should exist -> Create()
// - If resource exists and should exist:
//   - if !IsUpToDate() -> Update()
//   - else -> nothing to do
//
// - If resource exists but should not exist -> Delete()
// - If resource does not exist and should not exist -> nothing to do
//
// The function logs each step through opts.Log to provide traceability.
func Resource(opts Options) (changed bool, err error) {
	// Assume required callbacks are non-nil (per caller contract).
	log := opts.Log
	name := opts.Name
	log(fmt.Sprintf("resource: %s starting reconciliation", name))

	// Evaluate current and desired state (assume callbacks exist).
	exists := opts.Exists()
	should := opts.ShouldExist()

	log(fmt.Sprintf("resource: %s exists=%t shouldExist=%t", name, exists, should))

	// Case: resource should exist but doesn't -> create
	if !exists && should {
		log(fmt.Sprintf("resource: %s does not exist but should -> creating", name))
		changed = true
		err := opts.Create()
		if err == nil {
			log(fmt.Sprintf("resource: %s create succeeded", name))
		} else {
			log(fmt.Sprintf("resource: %s create failed", name))
		}
		return changed, err
	}

	// Case: resource exists and should exist -> maybe update
	if exists && should {
		upToDate := opts.IsUpToDate()
		if upToDate {
			log(fmt.Sprintf("resource: %s exists and is up-to-date -> nothing to do", name))
			return false, nil
		}

		log(fmt.Sprintf("resource: %s exists but is not up-to-date -> updating", name))
		changed = true
		err := opts.Update()
		if err == nil {
			log(fmt.Sprintf("resource: %s update succeeded", name))
		} else {
			log(fmt.Sprintf("resource: %s update failed", name))
		}
		return changed, err
	}

	// Case: resource exists but should NOT exist -> delete
	if exists && !should {
		log(fmt.Sprintf("resource: %s exists but should not -> deleting", name))
		changed = true
		err = opts.Delete()
		if err == nil {
			log(fmt.Sprintf("resource: %s delete succeeded", name))
		} else {
			log(fmt.Sprintf("resource: %s delete failed", name))
		}
		return changed, err
	}

	// Case: does not exist and should not exist -> nothing to do
	log(fmt.Sprintf("resource: %s does not exist and should not -> nothing to do", name))
	return false, nil
}
