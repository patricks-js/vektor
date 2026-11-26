package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeOps struct {
	euid          int
	method        InstallMethod
	backupCalled  bool
	debInstalled  string
	tarInstalled  string
	backupErr     error
	installDebErr error
}

func (f *fakeOps) Geteuid() int { return f.euid }
func (f *fakeOps) DetectInstallMethod() InstallMethod {
	if f.method == "" {
		return MethodDeb
	}
	return f.method
}
func (f *fakeOps) BackupSQLite(_ context.Context, dataDir, targetVersion string) (string, error) {
	f.backupCalled = true
	if f.backupErr != nil {
		return "", f.backupErr
	}
	return filepath.Join(dataDir, "vektor.db.pre-"+targetVersion+".bak"), nil
}
func (f *fakeOps) InstallDeb(_ context.Context, debPath string, _, _ io.Writer) error {
	f.debInstalled = debPath
	return f.installDebErr
}
func (f *fakeOps) InstallTarball(_ context.Context, tarballPath, _ string, _, _ io.Writer) error {
	f.tarInstalled = tarballPath
	return nil
}

func TestSemver(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"0.9.1", "0.9.2", true},
		{"v0.9.1", "v0.9.2", true},
		{"0.9.2", "0.9.2", false},
		{"0.9.3", "0.9.2", false},
		{"0.9.2-rc1", "0.9.2", true},
		{"0.9.2", "0.9.2-rc1", false},
		{"dev", "0.9.2", false},
	}
	for _, tc := range cases {
		if got := IsNewerSemver(tc.current, tc.latest); got != tc.want {
			t.Errorf("IsNewerSemver(%q, %q) = %v, want %v", tc.current, tc.latest, got, tc.want)
		}
	}
}

func TestParseSHA256SUMS(t *testing.T) {
	h1 := strings.Repeat("a", 64)
	h2 := strings.Repeat("b", 64)
	raw := fmt.Sprintf("# comment\n%s  vektor_0.9.2_amd64.deb\n%s *./vektor_0.9.2_linux_amd64.tar.gz\nbadline\n", h1, h2)
	sums, err := ParseSHA256SUMS(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseSHA256SUMS error: %v", err)
	}
	if sums["vektor_0.9.2_amd64.deb"] != h1 {
		t.Fatalf("unexpected deb hash: %q", sums["vektor_0.9.2_amd64.deb"])
	}
	if sums["vektor_0.9.2_linux_amd64.tar.gz"] != h2 {
		t.Fatalf("unexpected tar hash: %q", sums["vektor_0.9.2_linux_amd64.tar.gz"])
	}
}

func TestValidateDownloadURL(t *testing.T) {
	if err := ValidateDownloadURL("https://github.com/patricks-js/vektor/releases/download/v0.9.2/vektor_0.9.2_amd64.deb", DefaultAPIBaseURL); err != nil {
		t.Fatalf("expected github.com to be allowed: %v", err)
	}
	if err := ValidateDownloadURL("https://objects.githubusercontent.com/some/path", DefaultAPIBaseURL); err != nil {
		t.Fatalf("expected objects.githubusercontent.com to be allowed: %v", err)
	}
	if err := ValidateDownloadURL("http://github.com/patricks-js/vektor/x", DefaultAPIBaseURL); err == nil {
		t.Fatal("expected http scheme to be rejected")
	}
	if err := ValidateDownloadURL("https://evil.example.com/vektor.deb", DefaultAPIBaseURL); err == nil {
		t.Fatal("expected untrusted host to be rejected")
	}
}

func newMockReleaseServer(t *testing.T, version string, debPayload, tarPayload []byte, corruptHash bool) *httptest.Server {
	t.Helper()
	debName := fmt.Sprintf("vektor_%s_amd64.deb", version)
	tarName := fmt.Sprintf("vektor_%s_linux_amd64.tar.gz", version)

	debSum := sha256.Sum256(debPayload)
	tarSum := sha256.Sum256(tarPayload)
	debHex := hex.EncodeToString(debSum[:])
	tarHex := hex.EncodeToString(tarSum[:])
	if corruptHash {
		debHex = strings.Repeat("0", 64)
	}
	sumsBody := fmt.Sprintf("%s  %s\n%s  %s\n", debHex, debName, tarHex, tarName)

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/patricks-js/vektor/releases/latest",
			"/repos/patricks-js/vektor/releases/tags/v" + version:
			rel := Release{
				TagName: "v" + version,
				HTMLURL: srv.URL + "/releases/tag/v" + version,
				Assets: []Asset{
					{Name: debName, BrowserDownloadURL: srv.URL + "/dl/" + debName, Size: int64(len(debPayload))},
					{Name: tarName, BrowserDownloadURL: srv.URL + "/dl/" + tarName, Size: int64(len(tarPayload))},
					{Name: "SHA256SUMS.txt", BrowserDownloadURL: srv.URL + "/dl/SHA256SUMS.txt", Size: int64(len(sumsBody))},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(rel)
		case "/dl/SHA256SUMS.txt":
			_, _ = w.Write([]byte(sumsBody))
		case "/dl/" + debName:
			_, _ = w.Write(debPayload)
		case "/dl/" + tarName:
			_, _ = w.Write(tarPayload)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRunCheckOnlyAndDryRun(t *testing.T) {
	srv := newMockReleaseServer(t, "0.9.2", []byte("deb-bytes"), []byte("tar-bytes"), false)
	var out bytes.Buffer
	ops := &fakeOps{euid: 1000, method: MethodDeb}

	err := Run(context.Background(), Options{
		CurrentVersion: "0.9.1",
		CheckOnly:      true,
		Arch:           "amd64",
		APIBaseURL:     srv.URL,
		Ops:            ops,
		Stdout:         &out,
		Stderr:         &out,
	})
	if err != nil {
		t.Fatalf("CheckOnly failed: %v", err)
	}
	if !strings.Contains(out.String(), "v0.9.2") || !strings.Contains(out.String(), "sudo vektord upgrade") {
		t.Fatalf("unexpected check output: %s", out.String())
	}

	out.Reset()
	err = Run(context.Background(), Options{
		CurrentVersion: "0.9.1",
		DryRun:         true,
		Arch:           "amd64",
		APIBaseURL:     srv.URL,
		Ops:            ops,
		Stdout:         &out,
		Stderr:         &out,
	})
	if err != nil {
		t.Fatalf("DryRun failed: %v", err)
	}
	if ops.debInstalled != "" || ops.backupCalled {
		t.Fatal("DryRun must not touch system")
	}
}

func TestRunRequiresRoot(t *testing.T) {
	srv := newMockReleaseServer(t, "0.9.2", []byte("deb-bytes"), []byte("tar-bytes"), false)
	var out bytes.Buffer
	err := Run(context.Background(), Options{
		CurrentVersion: "0.9.1",
		Yes:            true,
		Arch:           "amd64",
		APIBaseURL:     srv.URL,
		Ops:            &fakeOps{euid: 1000, method: MethodDeb},
		Stdout:         &out,
		Stderr:         &out,
	})
	if err == nil || !strings.Contains(err.Error(), "root") {
		t.Fatalf("expected root error, got %v", err)
	}
}

func TestRunDebUpgradeSuccess(t *testing.T) {
	srv := newMockReleaseServer(t, "0.9.2", []byte("valid-deb-content"), []byte("tar"), false)
	var out bytes.Buffer
	ops := &fakeOps{euid: 0, method: MethodDeb}

	err := Run(context.Background(), Options{
		CurrentVersion: "0.9.1",
		Yes:            true,
		Arch:           "amd64",
		DataDir:        t.TempDir(),
		APIBaseURL:     srv.URL,
		Ops:            ops,
		Stdout:         &out,
		Stderr:         &out,
	})
	if err != nil {
		t.Fatalf("upgrade failed: %v\noutput: %s", err, out.String())
	}
	if !ops.backupCalled {
		t.Fatal("expected BackupSQLite to be called")
	}
	if !strings.HasSuffix(ops.debInstalled, "vektor_0.9.2_amd64.deb") {
		t.Fatalf("expected deb to be installed, got %q", ops.debInstalled)
	}
}

func TestRunRejectsBadChecksum(t *testing.T) {
	srv := newMockReleaseServer(t, "0.9.2", []byte("tampered-deb"), []byte("tar"), true)
	var out bytes.Buffer
	ops := &fakeOps{euid: 0, method: MethodDeb}

	err := Run(context.Background(), Options{
		CurrentVersion: "0.9.1",
		Yes:            true,
		Arch:           "amd64",
		DataDir:        t.TempDir(),
		APIBaseURL:     srv.URL,
		Ops:            ops,
		Stdout:         &out,
		Stderr:         &out,
	})
	if err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("expected SHA-256 mismatch error, got %v", err)
	}
	if ops.debInstalled != "" || ops.backupCalled {
		t.Fatal("must not backup or install when checksum fails")
	}
}

func TestRunAlreadyUpToDate(t *testing.T) {
	srv := newMockReleaseServer(t, "0.9.2", []byte("deb"), []byte("tar"), false)
	var out bytes.Buffer
	ops := &fakeOps{euid: 0, method: MethodDeb}

	err := Run(context.Background(), Options{
		CurrentVersion: "0.9.2",
		Yes:            true,
		Arch:           "amd64",
		APIBaseURL:     srv.URL,
		Ops:            ops,
		Stdout:         &out,
		Stderr:         &out,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ops.debInstalled != "" {
		t.Fatal("should not reinstall when already up to date")
	}
}

func TestBackupSQLiteVacuumInto(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "vektor.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT); INSERT INTO t VALUES (1, 'ok');`); err != nil {
		t.Fatalf("init sqlite: %v", err)
	}
	_ = db.Close()

	bakPath, err := (realSystemOps{}).BackupSQLite(context.Background(), dir, "0.9.2")
	if err != nil {
		t.Fatalf("BackupSQLite failed: %v", err)
	}
	if bakPath != filepath.Join(dir, "vektor.db.pre-0.9.2.bak") {
		t.Fatalf("unexpected backup path: %s", bakPath)
	}

	bakDB, err := sql.Open("sqlite", bakPath)
	if err != nil {
		t.Fatalf("open backup db: %v", err)
	}
	defer bakDB.Close()
	var val string
	if err := bakDB.QueryRow(`SELECT val FROM t WHERE id = 1`).Scan(&val); err != nil || val != "ok" {
		t.Fatalf("backup db missing data: val=%q err=%v", val, err)
	}
}

func TestExtractTarballAndAtomicReplace(t *testing.T) {
	tmp := t.TempDir()
	tarPath := filepath.Join(tmp, "vektor.tar.gz")

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range map[string]string{
		"./vektord":    "new-vektord",
		"./vektor-ctl": "new-ctl",
		"./README.md":  "ignored",
	} {
		hdr := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(body))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	_ = tw.Close()
	_ = gz.Close()
	if err := os.WriteFile(tarPath, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	stage := filepath.Join(tmp, "stage")
	if err := os.MkdirAll(stage, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := extractTarballBinaries(tarPath, stage); err != nil {
		t.Fatalf("extractTarballBinaries failed: %v", err)
	}

	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(binDir, "vektord")
	if err := os.WriteFile(target, []byte("old-vektord"), 0o755); err != nil {
		t.Fatal(err)
	}

	bak, err := atomicReplace(filepath.Join(stage, "vektord"), target)
	if err != nil {
		t.Fatalf("atomicReplace failed: %v", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "new-vektord" {
		t.Fatalf("expected new-vektord, got %q", string(got))
	}
	rollbackBinary(bak, target)
	got, _ = os.ReadFile(target)
	if string(got) != "old-vektord" {
		t.Fatalf("expected rollback to old-vektord, got %q", string(got))
	}
}
