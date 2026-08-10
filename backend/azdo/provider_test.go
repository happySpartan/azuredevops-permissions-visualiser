package azdo

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestAzCLITokenProvider(t *testing.T) {
	p := NewAzCLITokenProvider()
	p.LookPath = func(string) (string, error) { return "/usr/bin/az", nil }
	p.Run = func(ctx context.Context, azPath string, args ...string) ([]byte, error) {
		if azPath != "/usr/bin/az" {
			t.Errorf("azPath = %q", azPath)
		}
		// find the --resource argument
		found := false
		for i, a := range args {
			if a == "--resource" && i+1 < len(args) && args[i+1] == AzureDevOpsResourceID {
				found = true
			}
		}
		if !found {
			t.Errorf("missing --resource %s in args: %v", AzureDevOpsResourceID, args)
		}
		return []byte(`{"accessToken":"real-token","expiresOn":"..."}`), nil
	}

	tok, err := p.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if tok != "real-token" {
		t.Fatalf("token = %q", tok)
	}
}

func TestAzCLITokenProviderNotInstalled(t *testing.T) {
	p := NewAzCLITokenProvider()
	p.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	p.Run = func(ctx context.Context, s string, a ...string) ([]byte, error) { return nil, nil }

	_, err := p.Token(context.Background())
	if err == nil {
		t.Fatal("expected error when az not installed")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAzCLITokenProviderEmptyOutput(t *testing.T) {
	p := NewAzCLITokenProvider()
	p.LookPath = func(string) (string, error) { return "/usr/bin/az", nil }
	p.Run = func(ctx context.Context, s string, a ...string) ([]byte, error) {
		return []byte(`{}`), nil
	}
	_, err := p.Token(context.Background())
	if err == nil {
		t.Fatal("expected error on empty accessToken")
	}
}

func TestBuildSecurityToken(t *testing.T) {
	cases := []struct {
		proj, folder string
		def          int
		want         string
	}{
		{"p1", "", 0, "p1"},
		{"p1", "", 12, "p1/12"},
		{"p1", "/Shared", 12, "p1/Shared/12"},
		{"p1", "Shared", 12, "p1/Shared/12"},
	}
	for _, tc := range cases {
		got := BuildSecurityToken(tc.proj, tc.folder, tc.def)
		if got != tc.want {
			t.Errorf("BuildSecurityToken(%q,%q,%d) = %q, want %q", tc.proj, tc.folder, tc.def, got, tc.want)
		}
	}
}
