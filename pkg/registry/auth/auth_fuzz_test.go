// Package auth_test provides fuzz tests for the registry authentication functionality.
// These tests ensure robust handling of various inputs to prevent crashes and unexpected behavior.
package auth_test

import (
	"encoding/json"
	"testing"

	"github.com/distribution/reference"

	"github.com/nicholas-fedor/watchtower/pkg/registry/auth"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// FuzzProcessChallenge fuzzes the processChallenge function to test for crashes or unexpected behavior
// with malformed authentication challenge strings. It ensures robust parsing of WWW-Authenticate headers
// for Bearer authentication, covering valid challenges, malformed headers, missing fields, and edge cases.
func FuzzProcessChallenge(f *testing.F) {
	// Seed with valid challenge headers
	f.Add(
		`bearer realm="https://ghcr.io/token",service="ghcr.io",scope="repository:test/image:pull"`,
		"ghcr.io/test/image",
	)
	f.Add(
		`bearer realm="https://registry.example.com/token",service="registry.example.com",scope="repository:user/repo:pull"`,
		"registry.example.com/user/repo",
	)
	f.Add(
		`bearer realm="https://auth.docker.io/token",service="registry.docker.io",scope="repository:library/alpine:pull"`,
		"alpine",
	)

	// Seed with malformed headers - missing fields
	f.Add(`bearer realm="https://ghcr.io/token"`, "ghcr.io/test/image") // Missing service
	f.Add(`bearer service="ghcr.io"`, "ghcr.io/test/image")             // Missing realm
	f.Add(
		`bearer scope="repository:test/image:pull"`,
		"ghcr.io/test/image",
	) // Missing realm and service

	// Seed with empty or invalid headers
	f.Add(``, "ghcr.io/test/image")                   // Empty header
	f.Add(`basic realm="test"`, "ghcr.io/test/image") // Wrong auth type
	f.Add(`bearer`, "ghcr.io/test/image")             // Just bearer
	f.Add(`bearer realm=""`, "ghcr.io/test/image")    // Empty realm
	f.Add(`bearer service=""`, "ghcr.io/test/image")  // Empty service

	// Seed with malformed syntax
	f.Add(
		`bearer realm="https://ghcr.io/token",service="ghcr.io",scope="repository:test/image:pull",`,
		"ghcr.io/test/image",
	) // Trailing comma
	f.Add(
		`bearer realm="https://ghcr.io/token" service="ghcr.io"`,
		"ghcr.io/test/image",
	) // Wrong separator
	f.Add(
		`bearer realm="https://ghcr.io/token",service="ghcr.io",invalidkey`,
		"ghcr.io/test/image",
	) // Valueless key
	f.Add(
		`bearer realm="https://ghcr.io/token",service="ghcr.io",scope=`,
		"ghcr.io/test/image",
	) // Empty scope

	// Seed with edge cases
	f.Add(
		`BEARER realm="https://ghcr.io/token",service="ghcr.io",scope="repository:test/image:pull"`,
		"ghcr.io/test/image",
	) // Uppercase
	f.Add(
		`bearer realm="https://ghcr.io/token",service="ghcr.io",scope="repository:test/image:pull",extra="value"`,
		"ghcr.io/test/image",
	) // Extra fields
	f.Add(
		`bearer realm="http://localhost:5000/token",service="localhost:5000",scope="repository:test/image:pull"`,
		"localhost:5000/test/image",
	) // Local registry

	f.Fuzz(func(_ *testing.T, wwwAuthHeader, image string) {
		// Call ProcessChallenge; we don't care about the result, just that it doesn't panic
		_, _, _, _ = auth.ProcessChallenge(wwwAuthHeader, image)
	})
}

// FuzzGetBearerHeader fuzzes the JSON unmarshaling in GetBearerHeader to ensure robust parsing
// of token responses from registries. It tests various JSON inputs including valid responses,
// malformed JSON, missing fields, and edge cases to prevent crashes or unexpected behavior.
func FuzzGetBearerHeader(f *testing.F) {
	// Seed with valid JSON responses
	f.Add([]byte(`{"token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"}`))
	f.Add([]byte(`{"token":""}`))
	f.Add([]byte(`{"token":null}`))

	// Seed with malformed JSON
	f.Add([]byte(`{"token":"test"`))
	f.Add([]byte(`invalid json`))
	f.Add([]byte(`{"token":}`))
	f.Add([]byte(`{"token": "test",}`))

	// Seed with missing fields
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"other":"field"}`))
	f.Add([]byte(`[]`))

	// Seed with edge cases
	f.Add([]byte(`{"token":123}`))
	f.Add([]byte(`{"token":true}`))
	f.Add([]byte(`{"token":{"nested":"object"}}`))
	f.Add([]byte(`{"token":["array"]}`))
	f.Add([]byte(`null`))
	f.Add([]byte(``))
	f.Add(
		[]byte(
			`{"token":"very long token string that might cause issues if not handled properly in some parsers but json should handle it fine"}`,
		),
	)

	f.Fuzz(func(_ *testing.T, jsonData []byte) {
		// Attempt to unmarshal the JSON data into a TokenResponse struct
		// We don't care about the result, just that it doesn't panic or cause undefined behavior
		var tokenResponse types.TokenResponse

		_ = json.Unmarshal(jsonData, &tokenResponse)
	})
}

// FuzzTransformAuth fuzzes the TransformAuth function to test for crashes or unexpected behavior
// with various base64-encoded inputs. It ensures robust decoding and unmarshaling of JSON credentials,
// covering valid inputs, malformed base64, invalid JSON, and edge cases.
func FuzzTransformAuth(f *testing.F) {
	// Seed with valid base64-encoded JSON credentials
	f.Add(
		"eyJ1c2VybmFtZSI6InRlc3R1c2VyIiwicGFzc3dvcmQiOiJ0ZXN0cGFzcyJ9",
	) // {"username":"testuser","password":"testpass"}
	// Seed with valid base64 but incomplete JSON (missing password)
	f.Add("eyJ1c2VybmFtZSI6InVzZXIifQ==") // {"username":"user"}
	// Seed with valid base64 but password only
	f.Add("eyJwYXNzd29yZCI6InBhc3MifQ==") // {"password":"pass"}
	// Seed with valid base64 but empty JSON
	f.Add("e30=") // {}
	// Seed with invalid JSON (malformed)
	f.Add("eyJ1c2VybmFtZSI6InVzZXIi") // {"username":"user" (missing closing)
	// Seed with invalid base64
	f.Add("notbase64")
	f.Add("aGVsbG8=") // "hello" base64, not JSON
	// Seed with empty string
	f.Add("")
	// Seed with edge cases
	f.Add("dGVzdA==") // "test" base64
	f.Add(
		"eyJ1c2VybmFtZSI6bnVsbCwgcGFzc3dvcmQiOiJwYXNzIn0=",
	) // {"username":null, "password":"pass"}
	f.Fuzz(func(_ *testing.T, input string) {
		// Call TransformAuth; we don't care about the result, just that it doesn't panic
		auth.TransformAuth(input)
	})
}

// FuzzGetAuthURL fuzzes the GetAuthURL function to test for crashes or unexpected behavior
// with malformed authentication challenge strings. It ensures robust URL construction from
// challenge headers, covering valid challenges, malformed headers, missing fields, and edge cases.
func FuzzGetAuthURL(f *testing.F) {
	// Parse a fixed image reference for fuzzing
	imageRef, _ := reference.ParseNormalizedNamed("ghcr.io/test/image")

	// Seed with valid challenge headers
	f.Add(
		`bearer realm="https://ghcr.io/token",service="ghcr.io",scope="repository:test/image:pull"`,
	)
	f.Add(
		`bearer realm="https://registry.example.com/token",service="registry.example.com",scope="repository:user/repo:pull"`,
	)
	f.Add(
		`bearer realm="https://auth.docker.io/token",service="registry.docker.io",scope="repository:library/alpine:pull"`,
	)

	// Seed with malformed headers - missing fields
	f.Add(`bearer realm="https://ghcr.io/token"`)      // Missing service
	f.Add(`bearer service="ghcr.io"`)                  // Missing realm
	f.Add(`bearer scope="repository:test/image:pull"`) // Missing realm and service

	// Seed with empty or invalid headers
	f.Add(``)                   // Empty header
	f.Add(`basic realm="test"`) // Wrong auth type
	f.Add(`bearer`)             // Just bearer
	f.Add(`bearer realm=""`)    // Empty realm
	f.Add(`bearer service=""`)  // Empty service

	// Seed with malformed syntax
	f.Add(
		`bearer realm="https://ghcr.io/token",service="ghcr.io",scope="repository:test/image:pull",`,
	) // Trailing comma
	f.Add(
		`bearer realm="https://ghcr.io/token" service="ghcr.io"`,
	) // Wrong separator
	f.Add(
		`bearer realm="https://ghcr.io/token",service="ghcr.io",invalidkey`,
	) // Valueless key
	f.Add(
		`bearer realm="https://ghcr.io/token",service="ghcr.io",scope=`,
	) // Empty scope

	// Seed with edge cases
	f.Add(
		`BEARER realm="https://ghcr.io/token",service="ghcr.io",scope="repository:test/image:pull"`,
	) // Uppercase
	f.Add(
		`bearer realm="https://ghcr.io/token",service="ghcr.io",scope="repository:test/image:pull",extra="value"`,
	) // Extra fields
	f.Add(
		`bearer realm="http://localhost:5000/token",service="localhost:5000",scope="repository:test/image:pull"`,
	) // Local registry

	f.Fuzz(func(_ *testing.T, challenge string) {
		// Call GetAuthURL; we don't care about the result, just that it doesn't panic
		_, _ = auth.GetAuthURL(challenge, imageRef)
	})
}
