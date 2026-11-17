package domain

import "testing"

func TestCapabilities(t *testing.T) {
	p := Principal{ID: "account", Student: true, Tutor: true, CoordinatorFaculty: "science", Verification: Pending}
	if p.VerifiedStudent() || p.CanTutor() || !p.CanAccessFaculty("science") || p.CanAccessFaculty("law") {
		t.Fatal("pending student must be restricted without losing independent coordinator access")
	}
	p.Verification = Approved
	if !p.VerifiedStudent() || !p.CanTutor() {
		t.Fatal("approved tutor should retain student and tutor capabilities")
	}
	p.Suspended = true
	if p.VerifiedStudent() || p.CanTutor() || p.Coordinator() {
		t.Fatal("suspension must disable all capabilities")
	}
}
