package redact

import (
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// redactedUserinfo replaces the userinfo matched by userinfoInError.
const redactedUserinfo = "://xxxxx:xxxxx@"

// userinfoInError matches scheme://user@ and scheme://user:password@ in a daemon error.
//
// It matches through the last @ before the first /, ?, #, or whitespace, so a
// password that contains @ is matched whole.
var userinfoInError = regexp.MustCompile(`://[^/?#\s]+@`)

// GitURL removes credentials and sensitive query values from a Git URL.
//
// The host, port, path, non-sensitive query, and fragment remain available for identity. An
// unparseable URL is rejected so metadata cannot accidentally echo a secret.
//
// Parameters:
//   - raw: Git clone URL.
//
// Returns:
//   - string: Safe URL, or empty when it cannot be parsed.
func GitURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	if !strings.Contains(raw, "://") {
		if isNativeRepositoryPath(raw) {
			return raw
		}

		if strings.Contains(raw, "@") && strings.Contains(raw, ":") {
			return redactSCPGitURL(raw)
		}

		return redactUnqualifiedPath(raw)
	}

	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Host == "" && parsed.Scheme != "file") {
		return ""
	}

	if parsed.User != nil {
		switch {
		case parsed.Scheme == "http" || parsed.Scheme == "https" || parsed.Scheme == "file",
			gitUserIsCredential(parsed.User.Username()):
			parsed.User = nil
		default:
			if _, hasPassword := parsed.User.Password(); hasPassword {
				parsed.User = url.User(parsed.User.Username())
			}
		}
	}

	parsed.RawQuery = redactQuery(parsed.RawQuery)
	parsed.ForceQuery = parsed.RawQuery != ""

	return parsed.String()
}

// isNativeRepositoryPath reports whether raw is an explicit local filesystem path.
//
// Parameters:
//   - raw: Repository value.
//
// Returns:
//   - bool: True for absolute, explicitly relative, home-relative, or Windows paths.
func isNativeRepositoryPath(raw string) bool {
	return strings.HasPrefix(raw, "/") ||
		strings.HasPrefix(raw, "./") ||
		strings.HasPrefix(raw, "../") ||
		strings.HasPrefix(raw, "~/") ||
		strings.HasPrefix(raw, `C:\`) ||
		strings.HasPrefix(raw, "C:/") ||
		strings.HasPrefix(raw, `\\`) ||
		raw == "." ||
		raw == ".."
}

// redactUnqualifiedPath removes sensitive query values from a local or bare repository path.
//
// Parameters:
//   - raw: Local path or bare repository identifier.
//
// Returns:
//   - string: Path with non-sensitive query data preserved.
func redactUnqualifiedPath(raw string) string {
	base, fragment, hasFragment := strings.Cut(raw, "#")

	path, query, hasQuery := strings.Cut(base, "?")
	if !hasQuery {
		return raw
	}

	sanitizedQuery := redactQuery(query)
	if sanitizedQuery != "" {
		path += "?" + sanitizedQuery
	}

	if hasFragment {
		path += "#" + fragment
	}

	return path
}

// redactQuery removes credential-like query parameters while preserving benign routing data.
//
// Parameters:
//   - raw: URL query string without the leading question mark.
//
// Returns:
//   - string: Sanitized encoded query string.
func redactQuery(raw string) string {
	if raw == "" {
		return ""
	}

	parts := strings.Split(raw, "&")
	kept := make([]string, 0, len(parts))

	for _, part := range parts {
		keyPart, _, _ := strings.Cut(part, "=")

		key, err := url.QueryUnescape(keyPart)
		if err != nil {
			continue
		}

		if isSensitiveQueryKey(key) {
			continue
		}

		kept = append(kept, part)
	}

	return strings.Join(kept, "&")
}

// isSensitiveQueryKey reports whether a query parameter commonly contains credentials.
//
// The key matches when a part between separators names a credential. It also
// matches when a word of the key does, so accessToken and clientSecret match
// like access_token and client_secret. Two adjacent words count as one name, so
// dbPassWord and sshPassPhrase match too.
//
// Parameters:
//   - key: URL query parameter name.
//
// Returns:
//   - bool: True when the parameter should be removed.
func isSensitiveQueryKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	parts := strings.FieldsFunc(normalized, isQueryKeySeparator)

	if slices.ContainsFunc(parts, isCredentialWord) {
		return true
	}

	words := queryKeyWords(key)
	for i, word := range words {
		if isCredentialWord(word) || i > 0 && isCredentialWord(words[i-1]+word) {
			return true
		}
	}

	compact := strings.NewReplacer("_", "", "-", "", ".", "").Replace(normalized)

	return strings.Contains(compact, "oauth") || strings.Contains(compact, "apikey") || strings.Contains(compact, "accesskey") || strings.Contains(compact, "privatekey") ||
		strings.Contains(compact, "authtoken")
}

// isCredentialWord reports whether a lowercase word of a query parameter name
// names a credential.
//
// Parameters:
//   - word: Lowercase word or part of the name.
//
// Returns:
//   - bool: True when the word names a credential.
func isCredentialWord(word string) bool {
	switch word {
	case "token", "password", "passwd", "pwd", "passphrase", "secret", "credential", "credentials", "signature", "sig", "auth", "authorization", "session", "jwt", "bearer":
		return true
	default:
		return false
	}
}

// queryKeyWords splits a query parameter name into lowercase words at
// separators and camelCase boundaries.
//
// A capital letter starts a word after a lowercase letter or a digit. In a run
// of capitals, the last one starts a word when a lowercase letter follows it.
// APIToken splits into api and token.
//
// Parameters:
//   - key: URL query parameter name.
//
// Returns:
//   - []string: Lowercase words of the name.
func queryKeyWords(key string) []string {
	runes := []rune(key)
	words := []string{}
	start := 0

	for i, char := range runes {
		if isQueryKeySeparator(char) {
			if i > start {
				words = append(words, strings.ToLower(string(runes[start:i])))
			}

			start = i + 1

			continue
		}

		if i > start && unicode.IsUpper(char) {
			prev := runes[i-1]
			nextIsLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])

			if unicode.IsLower(prev) || unicode.IsDigit(prev) || unicode.IsUpper(prev) && nextIsLower {
				words = append(words, strings.ToLower(string(runes[start:i])))
				start = i
			}
		}
	}

	if start < len(runes) {
		words = append(words, strings.ToLower(string(runes[start:])))
	}

	return words
}

// isQueryKeySeparator reports whether r separates words in a query parameter name.
//
// Parameters:
//   - r: Character of the name.
//
// Returns:
//   - bool: True for an underscore, a hyphen, or a dot.
func isQueryKeySeparator(r rune) bool {
	return r == '_' || r == '-' || r == '.'
}

// redactSCPGitURL removes credentials and sensitive query values from an SCP-style Git URL.
//
// Parameters:
//   - raw: SCP-like Git URL.
//
// Returns:
//   - string: Safe URL, or empty when it is malformed.
func redactSCPGitURL(raw string) string {
	safe := redactUnqualifiedPath(raw)
	withoutFragment, fragment, hasFragment := strings.Cut(safe, "#")
	withoutQuery, query, hasQuery := strings.Cut(withoutFragment, "?")

	user, remote, found := strings.Cut(withoutQuery, "@")
	if !found || user == "" || gitUserIsCredential(user) {
		return ""
	}

	separator := strings.LastIndexByte(remote, ':')
	if separator <= 0 || separator == len(remote)-1 {
		return ""
	}

	host := remote[:separator]
	path := remote[separator+1:]

	parsedHost, err := url.Parse("//" + host)
	if err != nil || parsedHost.Host == "" || parsedHost.User != nil || parsedHost.Path != "" {
		return ""
	}

	sanitized := user + "@" + host + ":" + path
	if hasQuery && query != "" {
		sanitized += "?" + query
	}

	if hasFragment {
		sanitized += "#" + fragment
	}

	return sanitized
}

// gitUserIsCredential reports whether the user of a Git URL is a secret rather
// than an account name.
//
// git and other short account names are kept. A token used as the user is not
// stored on a replacement container, in an SCP-style URL or any other.
//
// Parameters:
//   - user: User component of a Git URL.
//
// Returns:
//   - bool: True when the user must not be persisted.
func gitUserIsCredential(user string) bool {
	if user == "" || strings.Contains(user, ":") {
		return true
	}

	lower := strings.ToLower(user)
	for _, marker := range []string{"token", "secret", "password", "passwd", "credential"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}

	for _, prefix := range []string{
		"ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_", "glpat-", "gldt-", "glrt-",
	} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}

	if len(user) >= 20 && isHexString(user) {
		return true
	}

	return false
}

// isHexString reports whether value contains only hexadecimal characters.
//
// Parameters:
//   - value: Candidate token.
//
// Returns:
//   - bool: True when every character is hexadecimal.
func isHexString(value string) bool {
	if value == "" {
		return false
	}

	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}

	return true
}

// MetadataURL sanitizes URL-like report metadata while preserving plain
// annotation values.
//
// Parameters:
//   - raw: Metadata value.
//
// Returns:
//   - string: Sanitized URL, or the original plain value.
func MetadataURL(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}

	if strings.Contains(value, "://") || strings.HasPrefix(value, "git@") || strings.Contains(value, "?") {
		return GitURL(value)
	}

	authority, _, _ := strings.Cut(value, "/")
	if strings.Contains(authority, "@") {
		return GitURL(value)
	}

	return value
}

// Remote replaces URL userinfo for logs.
//
// A remote that does not parse may still carry credentials, so its userinfo is
// masked by pattern.
//
// Parameters:
//   - remote: Git context URL that may contain credentials.
//
// Returns:
//   - string: Same URL with userinfo replaced by xxxxx.
func Remote(remote string) string {
	parsed, err := url.Parse(remote)
	if err != nil {
		return userinfoInError.ReplaceAllString(remote, redactedUserinfo)
	}

	if parsed.User == nil {
		return remote
	}

	if _, hasPassword := parsed.User.Password(); hasPassword {
		parsed.User = url.UserPassword("xxxxx", "xxxxx")
	} else {
		parsed.User = url.User("xxxxx")
	}

	return parsed.String()
}

// Userinfo replaces the credentialed remote and any leftover userinfo.
//
// The build request sends the remote as a query parameter. An error that echoes
// the request URL carries it query-escaped, so that form is replaced too.
//
// Parameters:
//   - msg: Error text.
//   - remote: Credentialed Git context URL.
//
// Returns:
//   - string: msg with credentials replaced by xxxxx.
func Userinfo(msg, remote string) string {
	if remote != "" {
		redacted := Remote(remote)
		msg = strings.ReplaceAll(msg, remote, redacted)
		msg = strings.ReplaceAll(msg, url.QueryEscape(remote), url.QueryEscape(redacted))
	}

	return userinfoInError.ReplaceAllString(msg, redactedUserinfo)
}
