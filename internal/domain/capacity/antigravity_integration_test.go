package capacity

import "testing"

func TestAntigravityConsentBounds(t *testing.T) {
	for name, limit := range map[string]int64{StoreAntigravityProjectConsents: 1024, StoreAntigravityConsentRootBytes: 4096, StoreAntigravityConsentIdentityBytes: 128} {
		found := false
		for _, entry := range Register() {
			if entry.Name == name {
				found = true
				if entry.Limit != limit || entry.AtLimit != Refuse {
					t.Fatalf("%s: %+v", name, entry)
				}
			}
		}
		if !found {
			t.Fatalf("missing %s", name)
		}
	}
}
