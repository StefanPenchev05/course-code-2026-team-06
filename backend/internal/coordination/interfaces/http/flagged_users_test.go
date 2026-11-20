package coordinationhttp

import "testing"

func TestFlaggedUsersFilters(t *testing.T) {
	for _, raw := range []string{"faculty_id=x", "user_id=x", "semester_id=x", "category=FLAG", "limit=1&limit=2", "offset=0&offset=1", "limit=", "limit=abc", "offset=1.1", "%zz=x", "limit=1;offset=0", "limit=999999999999999999999999"} {
		if _, err := flaggedUsersFilters(raw); err == nil {
			t.Fatal(raw)
		}
	}
	p, err := flaggedUsersFilters("")
	if err != nil || p.Limit != 25 || p.Offset != 0 || p.HasMore {
		t.Fatal(p, err)
	}
	p, err = flaggedUsersFilters("limit=100&offset=100000")
	if err != nil || p.Limit != 100 || p.Offset != 100000 {
		t.Fatal(p, err)
	}
}
