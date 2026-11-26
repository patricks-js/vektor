package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"vektor/internal/update"
)

const (
	defaultReleaseAPIURL = "https://api.github.com/repos/patricks-js/vektor/releases/latest"
	updateCacheTTL       = 6 * time.Hour
)

type UpdateInfo struct {
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version,omitempty"`
	UpdateAvailable bool   `json:"update_available"`
	ReleaseURL      string `json:"release_url,omitempty"`
	CheckedAt       string `json:"checked_at,omitempty"`
}

type updateChecker struct {
	mu      sync.Mutex
	current string
	apiURL  string
	client  *http.Client
	cached  UpdateInfo
	expires time.Time
}

func newUpdateChecker(current string) *updateChecker {
	return &updateChecker{
		current: update.NormalizeVersion(current),
		apiURL:  defaultReleaseAPIURL,
		client:  &http.Client{Timeout: 4 * time.Second},
	}
}

func (u *updateChecker) Check(ctx context.Context) UpdateInfo {
	u.mu.Lock()
	if time.Now().Before(u.expires) && u.cached.CurrentVersion != "" {
		info := u.cached
		u.mu.Unlock()
		return info
	}
	u.mu.Unlock()

	info := UpdateInfo{
		CurrentVersion: u.current,
		CheckedAt:      time.Now().UTC().Format(time.RFC3339),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.apiURL, nil)
	if err == nil {
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("User-Agent", "vektord/"+u.current)
		resp, doErr := u.client.Do(req)
		if doErr == nil {
			defer func() {
				_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
				_ = resp.Body.Close()
			}()
			if resp.StatusCode == http.StatusOK {
				var release struct {
					TagName string `json:"tag_name"`
					HTMLURL string `json:"html_url"`
				}
				if json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&release) == nil && release.TagName != "" {
					latest := update.NormalizeVersion(release.TagName)
					info.LatestVersion = latest
					info.ReleaseURL = release.HTMLURL
					info.UpdateAvailable = update.IsNewerSemver(u.current, latest)
				}
			}
		}
	}

	ttl := updateCacheTTL
	if info.LatestVersion == "" {
		// Retry sooner if GitHub was unreachable.
		ttl = 15 * time.Minute
	}

	u.mu.Lock()
	u.cached = info
	u.expires = time.Now().Add(ttl)
	u.mu.Unlock()
	return info
}

func isNewerSemver(current, latest string) bool {
	return update.IsNewerSemver(current, latest)
}

func (a *API) update(w http.ResponseWriter, r *http.Request) {
	if _, ok := queryValues(w, r); !ok {
		return
	}
	if a.updates == nil {
		writeJSON(w, http.StatusOK, UpdateInfo{CurrentVersion: "dev"})
		return
	}
	writeJSON(w, http.StatusOK, a.updates.Check(r.Context()))
}
