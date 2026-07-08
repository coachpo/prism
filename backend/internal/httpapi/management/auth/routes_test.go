package auth

import "testing"

func TestPublicManagementPathExact(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "login", path: "/api/auth/login", want: true},
		{name: "login slash suffix", path: "/api/auth/login/", want: false},
		{name: "login sibling", path: "/api/auth/logins", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isPublicManagementPath(tc.path); got != tc.want {
				t.Fatalf("isPublicManagementPath(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}
