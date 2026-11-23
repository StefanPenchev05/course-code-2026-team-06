package identityhttp

import (
	"encoding/json"
	"testing"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
)

func TestSafeProfileProgram(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    domain.Principal
		want string
	}{
		{"student", domain.Principal{ID: "student-id", Student: true, ProgramID: "program-a", Verification: domain.Approved}, "program-a"},
		{"tutor is also student", domain.Principal{ID: "student-id", Student: true, Tutor: true, ProgramID: "program-b", Verification: domain.Approved}, "program-b"},
		{"pending student", domain.Principal{ID: "student-id", Student: true, ProgramID: "program-c", Verification: domain.Pending}, "program-c"},
		{"coordinator without student role", domain.Principal{CoordinatorFaculty: "faculty-a", ProgramID: "must-not-leak"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(safeProfile(tc.p))
			if err != nil {
				t.Fatal(err)
			}
			var result map[string]any
			if err := json.Unmarshal(b, &result); err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if _, ok := result["program_id"]; ok {
					t.Fatal("non-student profile has program")
				}
			} else if result["program_id"] != tc.want {
				t.Fatalf("missing current program: %s", b)
			}
		})
	}
}
