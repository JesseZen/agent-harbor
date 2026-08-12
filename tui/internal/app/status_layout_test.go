package app

import "testing"

func TestPlaceStatusAfterChrome(t *testing.T) {
	page := "strip\ntable\nrow"
	got := placeStatusAfterChrome(page, "toast", 1)
	want := "strip\ntoast\ntable\nrow"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	got = placeStatusAfterChrome(page, "toast", 0)
	want = "toast\nstrip\ntable\nrow"
	if got != want {
		t.Fatalf("no-chrome got %q want %q", got, want)
	}
	if placeStatusAfterChrome(page, "", 1) != page {
		t.Fatal("empty status should leave page unchanged")
	}
}
