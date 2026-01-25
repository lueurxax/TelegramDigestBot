package enrichment

import (
	"crypto/md5"  //nolint:gosec // MD5 is required for HTTP Digest Authentication (RFC 2617)
	"crypto/rand" //nolint:gosec // crypto/rand is secure
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"strings"
)

var (
	errRequestBodyNotReplayable = errors.New("request body is not replayable")
	errMissingDigestRealmNonce  = errors.New("missing digest realm/nonce")
	errYaCyRequestFailed        = errors.New("yacy request failed")
)

const (
	digestPrefix          = "digest "
	basicPrefix           = "basic "
	headerWwwAuthenticate = "Www-Authenticate"
	fmtThreeStrings       = "%s:%s:%s"
	fmtErrWrap            = "%w: %w"
	authQop               = "auth"
	cnonceLen             = 8
)

func (p *YaCyProvider) doRequest(req *http.Request) (*http.Response, error) {
	if p.username == "" || p.password == "" {
		resp, err := p.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf(fmtErrWrap, errYaCyRequestFailed, err)
		}

		return resp, nil
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf(fmtErrWrap, errYaCyRequestFailed, err)
	}

	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}

	return p.handleUnauthorized(req, resp)
}

func (p *YaCyProvider) handleUnauthorized(req *http.Request, resp *http.Response) (*http.Response, error) {
	challenge := findDigestChallenge(resp.Header.Values(headerWwwAuthenticate))
	if challenge == "" {
		if hasBasicChallenge(resp.Header.Values(headerWwwAuthenticate)) {
			return p.doBasicAuthRetry(req, resp)
		}

		return resp, nil
	}

	_ = resp.Body.Close()

	authHeader, err := buildDigestAuthHeader(req, p.username, p.password, challenge)
	if err != nil {
		return resp, nil //nolint:nilerr // if digest auth header build fails, return original 401
	}

	return p.doDigestAuthRetry(req, authHeader)
}

func (p *YaCyProvider) doBasicAuthRetry(req *http.Request, resp *http.Response) (*http.Response, error) {
	_ = resp.Body.Close()

	retry, err := cloneRequest(req)
	if err != nil {
		return nil, err
	}

	retry.SetBasicAuth(p.username, p.password)

	resp, err = p.httpClient.Do(retry)
	if err != nil {
		return nil, fmt.Errorf(fmtErrWrap, errYaCyRequestFailed, err)
	}

	return resp, nil
}

func (p *YaCyProvider) doDigestAuthRetry(req *http.Request, authHeader string) (*http.Response, error) {
	retry, err := cloneRequest(req)
	if err != nil {
		return nil, err
	}

	retry.Header.Set("Authorization", authHeader)

	resp, err := p.httpClient.Do(retry)
	if err != nil {
		return nil, fmt.Errorf(fmtErrWrap, errYaCyRequestFailed, err)
	}

	return resp, nil
}

func cloneRequest(req *http.Request) (*http.Request, error) {
	clone := req.Clone(req.Context())
	if req.Body == nil {
		return clone, nil
	}

	if req.GetBody == nil {
		return nil, errRequestBodyNotReplayable
	}

	body, err := req.GetBody()
	if err != nil {
		return nil, fmt.Errorf("get body: %w", err)
	}

	clone.Body = body

	return clone, nil
}

func hasBasicChallenge(headers []string) bool {
	for _, h := range headers {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(h)), basicPrefix) {
			return true
		}
	}

	return false
}

func findDigestChallenge(headers []string) string {
	for _, h := range headers {
		trimmed := strings.TrimSpace(h)
		if strings.HasPrefix(strings.ToLower(trimmed), digestPrefix) {
			return strings.TrimSpace(trimmed[len(digestPrefix):])
		}
	}

	return ""
}

func buildDigestAuthHeader(req *http.Request, username, password, challenge string) (string, error) {
	params := parseAuthParams(challenge)
	realm := params["realm"]
	nonce := params["nonce"]
	opaque := params["opaque"]
	algorithm := params["algorithm"]
	qop := selectQop(params["qop"])

	if realm == "" || nonce == "" {
		return "", errMissingDigestRealmNonce
	}

	uri := req.URL.RequestURI()
	cnonce := randomHex(cnonceLen)
	nc := "00000001"

	ha1 := digestHash(fmt.Sprintf(fmtThreeStrings, username, realm, password), algorithm)
	if strings.HasSuffix(strings.ToUpper(algorithm), "-SESS") {
		ha1 = digestHash(fmt.Sprintf(fmtThreeStrings, ha1, nonce, cnonce), algorithm)
	}

	ha2 := digestHash(fmt.Sprintf("%s:%s", req.Method, uri), algorithm)

	var response string
	if qop != "" {
		response = digestHash(fmt.Sprintf("%s:%s:%s:%s:%s:%s", ha1, nonce, nc, cnonce, qop, ha2), algorithm)
	} else {
		response = digestHash(fmt.Sprintf(fmtThreeStrings, ha1, nonce, ha2), algorithm)
	}

	parts := []string{
		fmt.Sprintf(`username="%s"`, username),
		fmt.Sprintf(`realm="%s"`, realm),
		fmt.Sprintf(`nonce="%s"`, nonce),
		fmt.Sprintf(`uri="%s"`, uri),
		fmt.Sprintf(`response="%s"`, response),
	}

	if algorithm != "" {
		parts = append(parts, fmt.Sprintf(`algorithm="%s"`, algorithm))
	}

	if opaque != "" {
		parts = append(parts, fmt.Sprintf(`opaque="%s"`, opaque))
	}

	if qop != "" {
		parts = append(parts,
			fmt.Sprintf(`qop=%s`, qop),
			fmt.Sprintf(`nc=%s`, nc),
			fmt.Sprintf(`cnonce="%s"`, cnonce),
		)
	}

	return "Digest " + strings.Join(parts, ", "), nil
}

func selectQop(qop string) string {
	qop = strings.ToLower(qop)

	for _, part := range strings.Split(qop, ",") {
		part = strings.TrimSpace(part)
		if part == authQop {
			return authQop
		}
	}

	return ""
}

func parseAuthParams(raw string) map[string]string {
	params := make(map[string]string)
	i := 0

	for i < len(raw) {
		i = skipSpaceAndComma(raw, i)

		start := i
		for i < len(raw) && raw[i] != '=' && raw[i] != ',' {
			i++
		}

		if i >= len(raw) || raw[i] != '=' {
			break
		}

		key := strings.ToLower(strings.TrimSpace(raw[start:i]))
		i++ // skip '='

		if i >= len(raw) {
			break
		}

		var val string

		if raw[i] == '"' {
			val, i = parseQuotedValue(raw, i)
		} else {
			val, i = parseUnquotedValue(raw, i)
		}

		if key != "" {
			params[key] = val
		}
	}

	return params
}

func skipSpaceAndComma(raw string, i int) int {
	for i < len(raw) && (raw[i] == ' ' || raw[i] == ',') {
		i++
	}

	return i
}

func parseQuotedValue(raw string, i int) (string, int) {
	i++ // skip opening quote

	valStart := i

	for i < len(raw) && raw[i] != '"' {
		i++
	}

	val := raw[valStart:i]

	if i < len(raw) && raw[i] == '"' {
		i++
	}

	return val, i
}

func parseUnquotedValue(raw string, i int) (string, int) {
	valStart := i

	for i < len(raw) && raw[i] != ',' {
		i++
	}

	val := strings.TrimSpace(raw[valStart:i])

	return val, i
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "deadbeef"
	}

	return hex.EncodeToString(buf)
}

// digestHash computes the hash for HTTP Digest Authentication.
// Uses SHA-256 for RFC 7616 algorithms, falls back to MD5 for RFC 2617.
func digestHash(s string, algorithm string) string {
	var h hash.Hash

	if strings.HasPrefix(strings.ToUpper(algorithm), "SHA-256") {
		h = sha256.New()
	} else {
		// MD5 is required for HTTP Digest Authentication (RFC 2617)
		//nolint:gosec // Protocol requirement, not used for password storage
		h = md5.New()
	}

	h.Write([]byte(s))

	return hex.EncodeToString(h.Sum(nil))
}
