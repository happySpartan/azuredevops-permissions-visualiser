package main

import (
	"reflect"
	"testing"
)

func TestBrowserCommand(t *testing.T) {
	tests := []struct {
		goos string
		url  string
		want []string
	}{
		{goos: "linux", url: "http://127.0.0.1:8080", want: []string{"xdg-open", "http://127.0.0.1:8080"}},
		{goos: "windows", url: "http://127.0.0.1:8080", want: []string{"rundll32", "url.dll,FileProtocolHandler", "http://127.0.0.1:8080"}},
	}

	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			got, ok := browserCommand(tt.goos, tt.url)
			if !ok {
				t.Fatal("expected a browser command")
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("browserCommand() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestBrowserCommandRejectsUnsupportedOS(t *testing.T) {
	if command, ok := browserCommand("plan9", "http://127.0.0.1:8080"); ok || command != nil {
		t.Fatalf("browserCommand() = %#v, %v; want nil, false", command, ok)
	}
}
