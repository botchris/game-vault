package game

import (
	"errors"
	"fmt"
	"slices"
)

// CheckProviderCoverage reports how a provider's table of systems disagrees with the catalog: a
// catalog system with neither an entry nor an exclusion, a key that is not a catalog name, an empty
// entry, or a system both mapped and excluded. Each provider's tests call it, so adding a system to
// the catalog forces a decision in every provider. Nil means the table covers the catalog.
func CheckProviderCoverage[V any](table map[string][]V, excluded []string) error {
	var errs []error

	for _, s := range catalog {
		ids, mapped := table[s.Name]
		skipped := slices.Contains(excluded, s.Name)

		switch {
		case mapped && skipped:
			errs = append(errs, fmt.Errorf("system %q is both mapped and excluded", s.Name))
		case mapped && len(ids) == 0:
			errs = append(errs, fmt.Errorf("system %q has an empty entry", s.Name))
		case !mapped && !skipped:
			errs = append(errs, fmt.Errorf("system %q is neither mapped nor excluded", s.Name))
		}
	}

	for name := range table {
		if !slices.ContainsFunc(catalog, func(s System) bool { return s.Name == name }) {
			errs = append(errs, fmt.Errorf("%q is not a catalog system", name))
		}
	}

	return errors.Join(errs...)
}
