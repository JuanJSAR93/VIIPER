package registry_test

import (
	"testing"

	_ "github.com/Alia5/VIIPER/internal/devicecatalog"
	"github.com/Alia5/VIIPER/internal/server/api"
)

// Xbox One and Xbox Series are first-class native USB/IP personas. Loading the
// generic device catalog must expose only their canonical names.
func TestXboxPersonasRegisterCanonicalNames(t *testing.T) {
	if registration := api.GetRegistration("xbox360"); registration == nil {
		t.Fatal("canonical registry positive control xbox360 is not registered")
	}
	for _, name := range []string{"xboxone", "xboxseries"} {
		if registration := api.GetRegistration(name); registration != nil {
			continue
		}
		t.Fatalf("native persona %q is not registered", name)
	}
	for _, name := range []string{"xbox-one", "xbox-series"} {
		if registration := api.GetRegistration(name); registration != nil {
			t.Fatalf("unsupported alias %q unexpectedly registered as %T", name, registration)
		}
	}
}
