package main

import "testing"

func TestParseIncludeRead(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
		want  bool
		err   bool
	}{
		{name: "default", value: "", want: false},
		{name: "unread only", value: "false", want: false},
		{name: "all items", value: "true", want: true},
		{name: "invalid", value: "yes", err: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseIncludeRead(test.value)
			if (err != nil) != test.err || got != test.want {
				t.Fatalf("parseIncludeRead(%q) = %v, %v", test.value, got, err)
			}
		})
	}
}
