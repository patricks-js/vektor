package update

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

const (
	DefaultRepo       = "patricks-js/vektor"
	DefaultAPIBaseURL = "https://api.github.com"

	maxReleaseJSONBytes = 512 * 1024
	maxChecksumsBytes   = 256 * 1024
	maxAssetBytes       = 256 * 1024 * 1024
)

// Asset represents a downloadable file attached to a GitHub Release.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// Release represents a GitHub Release returned by the Releases API.
type Release struct {
	TagName    string  `json:"tag_name"`
	HTMLURL    string  `json:"html_url"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

// Version returns the release version without a leading "v".
func (r Release) Version() string {
	return NormalizeVersion(r.TagName)
}

// FindAsset looks up an asset by exact filename.
func (r Release) FindAsset(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

// FetchLatestRelease queries GitHub Releases for the latest stable release.
func FetchLatestRelease(ctx context.Context, client *http.Client, apiBaseURL, repo, userAgent string) (Release, error) {
	base := strings.TrimRight(apiBaseURL, "/")
	if base == "" {
		base = DefaultAPIBaseURL
	}
	if repo == "" {
		repo = DefaultRepo
	}
	endpoint := fmt.Sprintf("%s/repos/%s/releases/latest", base, repo)
	return fetchRelease(ctx, client, endpoint, userAgent)
}

// FetchReleaseByTag queries GitHub Releases for a specific SemVer version (with or without leading "v").
func FetchReleaseByTag(ctx context.Context, client *http.Client, apiBaseURL, repo, version, userAgent string) (Release, error) {
	ver := NormalizeVersion(version)
	if !IsValidSemver(ver) {
		return Release{}, fmt.Errorf("versão inválida %q (esperado MAJOR.MINOR.PATCH, ex.: 0.9.2)", version)
	}
	base := strings.TrimRight(apiBaseURL, "/")
	if base == "" {
		base = DefaultAPIBaseURL
	}
	if repo == "" {
		repo = DefaultRepo
	}
	endpoint := fmt.Sprintf("%s/repos/%s/releases/tags/v%s", base, repo, ver)
	return fetchRelease(ctx, client, endpoint, userAgent)
}

func fetchRelease(ctx context.Context, client *http.Client, endpoint, userAgent string) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}

	resp, err := client.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("consultar GitHub Releases: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusNotFound {
		return Release{}, fmt.Errorf("release não encontrada (%s)", endpoint)
	}
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("GitHub Releases retornou HTTP %d", resp.StatusCode)
	}

	var rel Release
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxReleaseJSONBytes)).Decode(&rel); err != nil {
		return Release{}, fmt.Errorf("decodificar resposta da release: %w", err)
	}
	if rel.TagName == "" {
		return Release{}, errors.New("resposta da release sem tag_name")
	}
	return rel, nil
}

// ParseSHA256SUMS parses standard sha256sum output ("<64-hex>  <filename>").
func ParseSHA256SUMS(r io.Reader) (map[string]string, error) {
	sums := make(map[string]string)
	scanner := bufio.NewScanner(io.LimitReader(r, maxChecksumsBytes))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		hash := strings.ToLower(fields[0])
		if len(hash) != 64 {
			continue
		}
		if _, err := hex.DecodeString(hash); err != nil {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		name = strings.TrimPrefix(name, "./")
		sums[name] = hash
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("ler SHA256SUMS.txt: %w", err)
	}
	if len(sums) == 0 {
		return nil, errors.New("nenhum checksum válido encontrado em SHA256SUMS.txt")
	}
	return sums, nil
}

// VerifyFileSHA256 computes the SHA-256 digest of path and compares it with expectedHex.
func VerifyFileSHA256(path, expectedHex string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("abrir arquivo para verificação SHA-256: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("calcular SHA-256: %w", err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	want := strings.ToLower(strings.TrimSpace(expectedHex))
	if got != want {
		return fmt.Errorf("checksum SHA-256 inválido: esperado %s, obtido %s", want, got)
	}
	return nil
}

// ValidateDownloadURL ensures download URLs stay on trusted GitHub release hosts
// (or the custom test host when apiBaseURL is overridden in tests).
func ValidateDownloadURL(rawURL, apiBaseURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("URL de download inválida: %w", err)
	}
	host := strings.ToLower(u.Hostname())
	if apiBaseURL != "" && apiBaseURL != DefaultAPIBaseURL {
		baseURL, err := url.Parse(apiBaseURL)
		if err == nil && strings.EqualFold(baseURL.Hostname(), host) {
			return nil
		}
	}
	if u.Scheme != "https" {
		return fmt.Errorf("URL de download deve usar HTTPS: %s", rawURL)
	}
	if host == "github.com" ||
		host == "objects.githubusercontent.com" ||
		host == "release-assets.githubusercontent.com" ||
		strings.HasSuffix(host, ".githubusercontent.com") {
		return nil
	}
	return fmt.Errorf("host de download não confiável: %s", host)
}
