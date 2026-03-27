package main

import "testing"

func TestContainsGlob(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{name: "no glob", in: "photo.jpg", want: false},
		{name: "asterisk", in: "*.jpg", want: true},
		{name: "question", in: "IMG_?.jpg", want: true},
		{name: "character class", in: "IMG_[0-9].jpg", want: true},
		{name: "regex delimiters are not glob", in: "/IMG_\\d+\\.jpg/", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := containsGlob(tc.in)
			if got != tc.want {
				t.Fatalf("containsGlob(%q) = %t, want %t", tc.in, got, tc.want)
			}
		})
	}
}

func TestIsValidExtension(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{name: "jpg", in: "photo.jpg", want: true},
		{name: "jpeg uppercase", in: "photo.JPEG", want: true},
		{name: "heic", in: "photo.heic", want: true},
		{name: "png", in: "photo.png", want: false},
		{name: "no extension", in: "photo", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isValidExtension(tc.in)
			if got != tc.want {
				t.Fatalf("isValidExtension(%q) = %t, want %t", tc.in, got, tc.want)
			}
		})
	}
}
