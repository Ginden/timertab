package reconcile

import (
	"fmt"
	"strings"
)

// DesiredUnit is a rendered unit file expected after reconcile.
type DesiredUnit struct {
	Name    string
	Content string
}

// ExistingUnit describes one discovered unit in the target systemd scope.
// Managed should be true only when ownership marker metadata confirms timertab ownership.
type ExistingUnit struct {
	Name    string
	Content string
	Managed bool
}

// Plan is a deterministic reconcile decision result.
type Plan struct {
	Create []DesiredUnit
	Update []DesiredUnit
	Keep   []string
	Remove []string
}

func validateDesiredUnitName(name string, targetUID uint32) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("desired unit name cannot be empty")
	}
	if !IsManagedUnitForUID(targetUID, name) {
		return fmt.Errorf("desired unit %q is outside managed scope for uid %d", name, targetUID)
	}
	return nil
}

func managedUnitPrefix(targetUID uint32) string {
	return fmt.Sprintf("timertab-u%d-", targetUID)
}

// IsManagedUnitForUID reports whether unitName belongs to timertab namespace
// for targetUID and has a supported unit extension.
func IsManagedUnitForUID(targetUID uint32, unitName string) bool {
	if !strings.HasPrefix(unitName, managedUnitPrefix(targetUID)) {
		return false
	}
	return strings.HasSuffix(unitName, ".service") || strings.HasSuffix(unitName, ".timer")
}
