package coordinationhttp

import "testing"

func TestMonitoringQueryShape(t *testing.T) {
	for _, v := range []struct{ query, mode string }{
		{"faculty_id=anything", "list"}, {"limit=1&limit=2", "list"}, {"subject_id=", "list"},
		{"limit=1;offset=2", "list"}, {"limit=NaN", "list"}, {"%zz=1", "list"},
		{"subject_id=abc", "detail"}, {"from=tomorrow", "statistics"}, {"limit=1", "statistics"},
		{"from=one&from=two", "schedule"},
	} {
		if _, _, err := monitoringQuery(v.query, v.mode); err == nil {
			t.Fatal(v)
		}
	}
	q, p, err := monitoringQuery("program_id=abc&subject_id=def&limit=2&offset=3", "list")
	if err != nil || p.Limit != 2 || p.Offset != 3 || q.Get("program_id") != "abc" || q.Get("subject_id") != "def" {
		t.Fatal(q, p, err)
	}
}
