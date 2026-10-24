package capacity

import (
	"github.com/sainteye/clawdline/internal/domain/cloud"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

func TestRekeyBounds(t *testing.T) {
	b, e := os.ReadFile(filepath.Join(moduleRoot(t), "web/console/src/cloud/rekey-crypto.ts"))
	if e != nil {
		t.Fatal(e)
	}
	for _, c := range []struct {
		name, source, js string
		value            int64
		unit             Unit
	}{{"cloud.rekey_lifetime", "internal/domain/cloud.RekeyLifetimeMSLimit", "REKEY_LIFETIME_MS_LIMIT", cloud.RekeyLifetimeMSLimit, Seconds}, {"cloud.rekey_body", "internal/domain/cloud.MaxRekeyBodyBytes", "MAX_REKEY_BODY_BYTES", cloud.MaxRekeyBodyBytes, Bytes}} {
		match := regexp.MustCompile(c.js + ` = ([0-9_]+)`).FindSubmatch(b)
		if len(match) != 2 {
			t.Fatalf("missing browser bound %s", c.js)
		}
		v, _ := strconv.ParseInt(regexp.MustCompile(`_`).ReplaceAllString(string(match[1]), ""), 10, 64)
		if v != c.value {
			t.Errorf("browser %s differs", c.js)
		}
		found := false
		for _, r := range Register() {
			if r.Name == c.name {
				found = true
				want := c.value
				if c.unit == Seconds {
					want /= 1000
				}
				if int64(r.Limit) != want || r.Unit != c.unit || len(r.Sources) != 1 || r.Sources[0] != c.source {
					t.Errorf("incorrect %s", c.name)
				}
			}
		}
		if !found {
			t.Errorf("missing capacity row %s", c.name)
		}
	}
}
