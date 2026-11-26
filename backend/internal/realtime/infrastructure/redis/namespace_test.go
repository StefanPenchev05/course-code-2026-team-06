package redis

import "testing"

func TestNamespaceValidationAndIndependentKeys(t *testing.T) {
	for _, name := range []string{"", "UPPER", "a:b", "a*", "a?", "a[0]", "a{b}", "a b", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"} {
		if ValidateNamespace(name) == nil {
			t.Fatalf("accepted invalid namespace %q", name)
		}
	}
	for _, name := range []string{"a", "tutti-prod_2"} {
		if err := ValidateNamespace(name); err != nil {
			t.Fatal(err)
		}
	}
	a, _ := NewScoped(nil, "prod")
	b, _ := NewScoped(nil, "test")
	wa, _ := NewScopedWatches(nil, "prod")
	wb, _ := NewScopedWatches(nil, "test")
	if a.channel == b.channel || wa.prefix == wb.prefix || a.channel == Channel {
		t.Fatal("deployment keys collide")
	}
}
