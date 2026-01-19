package enrichment

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	digestPrefix = "digest "
	basicPrefix  = "basic "
)

func (p *YaCyProvider) doRequest(req *http.Request) (*http.Response, error) {
	if p.username == "" || p.password == "" {
		return p.httpClient.Do(req)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}

	challenge := findDigestChallenge(resp.Header.Values("Www-Authenticate"))
	if challenge == "" {
		if hasBasicChallenge(resp.Header.Values("Www-Authenticate")) {
			_ = resp.Body.Close()

			retry, err := cloneRequest(req)
			if err != nil {
				return nil, err
			}

			retry.SetBasicAuth(p.username, p.password)

			return p.httpClient.Do(retry)
		}

		return resp, nil
	}

	_ = resp.Body.Close()

	authHeader, err := buildDigestAuthHeader(req, p.username, p.password, challenge)
	if err != nil {
		return resp, nil
	}

	retry, err := cloneRequest(req)
	if err != nil {
		return nil, err
	}

	retry.Header.Set("Authorization", authHeader)

	return p.httpClient.Do(retry)
}

func cloneRequest(req *http.Request) (*http.Request, error) {
	clone := req.Clone(req.Context())
	if req.Body == nil {
		return clone, nil
	}

	if req.GetBody == nil {
		return nil, errors.New("request body is not replayable")
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
		return "", errors.New("missing digest realm/nonce")
	}

	uri := req.URL.RequestURI()
	cnonce := randomHex(8)
	nc := "00000001"

	ha1 := md5Hex(fmt.Sprintf("%s:%s:%s", username, realm, password))
	if strings.EqualFold(algorithm, "MD5-sess") {
		ha1 = md5Hex(fmt.Sprintf("%s:%s:%s", ha1, nonce, cnonce))
	}

	ha2 := md5Hex(fmt.Sprintf("%s:%s", req.Method, uri))

	var response string
	if qop != "" {
		response = md5Hex(fmt.Sprintf("%s:%s:%s:%s:%s:%s", ha1, nonce, nc, cnonce, qop, ha2))
	} else {
		response = md5Hex(fmt.Sprintf("%s:%s:%s", ha1, nonce, ha2))
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
		if part == "auth" {
			return "auth"
		}
	}

	return ""
}

func parseAuthParams(raw string) map[string]string {
	params := make(map[string]string)
	i := 0
	for i < len(raw) {
		for i < len(raw) && (raw[i] == ' ' || raw[i] == ',') {
			i++
		}

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
			i++
			valStart := i
			for i < len(raw) && raw[i] != '"' {
				i++
			}
			val = raw[valStart:i]
			if i < len(raw) && raw[i] == '"' {
				i++
			}
		} else {
			valStart := i
			for i < len(raw) && raw[i] != ',' {
				i++
			}
			val = strings.TrimSpace(raw[valStart:i])
		}

		if key != "" {
			params[key] = val
		}
	}

	return params
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "deadbeef"
	}

	return hex.EncodeToString(buf)
}

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}
