package coordinationhttp

import "testing"

func TestAuditFiltersRejectAuthorityAndAmbiguity(t *testing.T) {
	for _, raw := range []string{"faculty_id=x", "actor_id=x", "category=FLAG&category=SUSPENSION", "limit=1&limit=2", "limit=x", "offset=1.1", "%zz=x"} {
		if _, _, err := auditFilters(raw); err == nil {
			t.Fatal(raw)
		}
	}
	p, k, err := auditFilters("")
	if err != nil || p.Limit != 25 || p.Offset != 0 || k != "SUSPENSION" {
		t.Fatal(p, k, err)
	}
	p, k, err = auditFilters("category=FLAG&limit=1&offset=10")
	if err != nil || p.Limit != 1 || p.Offset != 10 || k != "FLAG" {
		t.Fatal(p, k, err)
	}
}
