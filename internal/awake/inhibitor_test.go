package awake

import "testing"

func TestInhibitorSetIsIdempotent(t *testing.T) {
	acquired, released := 0, 0
	inhibitor := &Inhibitor{acquire: func() (func() error, error) {
		acquired++
		return func() error { released++; return nil }, nil
	}}
	for range 2 {
		if err := inhibitor.Set(true); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := inhibitor.Set(false); err != nil {
			t.Fatal(err)
		}
	}
	if acquired != 1 || released != 1 {
		t.Fatalf("acquired %d times and released %d times", acquired, released)
	}
}
