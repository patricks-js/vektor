package update

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

// InstallMethod describes how Vektor was installed on the host.
type InstallMethod string

const (
	MethodDeb     InstallMethod = "deb"
	MethodTarball InstallMethod = "tarball"
)

// Options configures a CLI upgrade run.
type Options struct {
	CurrentVersion string
	TargetVersion  string
	CheckOnly      bool
	DryRun         bool
	Yes            bool
	Force          bool
	NoBackup       bool
	DataDir        string
	Arch           string
	BinDir         string
	APIBaseURL     string
	Repo           string
	HTTPClient     *http.Client
	Ops            SystemOps
	Stdin          io.Reader
	Stdout         io.Writer
	Stderr         io.Writer
}

// SystemOps abstracts host-level side effects for testability.
type SystemOps interface {
	Geteuid() int
	DetectInstallMethod() InstallMethod
	BackupSQLite(ctx context.Context, dataDir, targetVersion string) (string, error)
	InstallDeb(ctx context.Context, debPath string, stdout, stderr io.Writer) error
	InstallTarball(ctx context.Context, tarballPath, binDir string, stdout, stderr io.Writer) error
}

// Run executes the vektord upgrade workflow.
func Run(ctx context.Context, opts Options) error {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.Stdin == nil {
		opts.Stdin = os.Stdin
	}
	if opts.Repo == "" {
		opts.Repo = DefaultRepo
	}
	if opts.APIBaseURL == "" {
		opts.APIBaseURL = DefaultAPIBaseURL
	}
	if opts.Arch == "" {
		opts.Arch = runtime.GOARCH
	}
	if opts.BinDir == "" {
		opts.BinDir = "/usr/bin"
	}
	if opts.Ops == nil {
		opts.Ops = realSystemOps{}
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{
			Timeout: 2 * time.Minute,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return errors.New("muitos redirecionamentos HTTP")
				}
				return ValidateDownloadURL(req.URL.String(), opts.APIBaseURL)
			},
		}
	}

	current := NormalizeVersion(opts.CurrentVersion)
	if current == "" {
		current = "dev"
	}
	ua := "vektord/" + current

	var (
		rel Release
		err error
	)
	if opts.TargetVersion != "" {
		rel, err = FetchReleaseByTag(ctx, opts.HTTPClient, opts.APIBaseURL, opts.Repo, opts.TargetVersion, ua)
	} else {
		rel, err = FetchLatestRelease(ctx, opts.HTTPClient, opts.APIBaseURL, opts.Repo, ua)
	}
	if err != nil {
		return err
	}

	target := rel.Version()
	if !IsValidSemver(target) {
		return fmt.Errorf("release retornou versão inválida: %q", rel.TagName)
	}

	if opts.CheckOnly {
		fmt.Fprintf(opts.Stdout, "Versão atual:      %s\n", formatVersion(current))
		fmt.Fprintf(opts.Stdout, "Versão disponível: v%s\n", target)
		if rel.HTMLURL != "" {
			fmt.Fprintf(opts.Stdout, "Notas da release:  %s\n", rel.HTMLURL)
		}
		if IsNewerSemver(current, target) || (current == "dev" && target != "") {
			fmt.Fprintf(opts.Stdout, "\nAtualização disponível. Execute:\n  sudo vektord upgrade\n")
		} else {
			fmt.Fprintf(opts.Stdout, "\nVektor já está atualizado.\n")
		}
		return nil
	}

	if !opts.Force && opts.TargetVersion == "" {
		if current == target {
			fmt.Fprintf(opts.Stdout, "Vektor já está na versão mais recente (v%s).\n", target)
			return nil
		}
		if current != "dev" && !IsNewerSemver(current, target) {
			fmt.Fprintf(opts.Stdout, "Versão instalada (v%s) já é mais recente que v%s. Use --force para sobrescrever.\n", current, target)
			return nil
		}
	}

	if opts.Arch != "amd64" && opts.Arch != "arm64" {
		return fmt.Errorf("arquitetura não suportada para upgrade automático: %s (suportadas: amd64, arm64)", opts.Arch)
	}

	method := opts.Ops.DetectInstallMethod()
	assetName := expectedAssetName(method, target, opts.Arch)
	asset, ok := rel.FindAsset(assetName)
	if !ok {
		return fmt.Errorf("artefato %q não encontrado na release v%s", assetName, target)
	}
	checksumsAsset, ok := rel.FindAsset("SHA256SUMS.txt")
	if !ok {
		return fmt.Errorf("arquivo SHA256SUMS.txt não encontrado na release v%s", target)
	}

	if err := ValidateDownloadURL(asset.BrowserDownloadURL, opts.APIBaseURL); err != nil {
		return err
	}
	if err := ValidateDownloadURL(checksumsAsset.BrowserDownloadURL, opts.APIBaseURL); err != nil {
		return err
	}

	dataDir := resolveDataDir(opts.DataDir)

	fmt.Fprintf(opts.Stdout, "Vektor upgrade: %s -> v%s\n", formatVersion(current), target)
	fmt.Fprintf(opts.Stdout, "  Método:     %s (%s)\n", method, opts.Arch)
	fmt.Fprintf(opts.Stdout, "  Artefato:   %s\n", asset.Name)
	fmt.Fprintf(opts.Stdout, "  Download:   %s\n", asset.BrowserDownloadURL)
	if !opts.NoBackup {
		dbPath := filepath.Join(dataDir, "vektor.db")
		if _, err := os.Stat(dbPath); err == nil {
			fmt.Fprintf(opts.Stdout, "  Backup DB:  %s\n", backupPathFor(dataDir, target))
		}
	}

	if opts.DryRun {
		fmt.Fprintf(opts.Stdout, "\n[dry-run] Nenhuma alteração foi aplicada.\n")
		return nil
	}

	if opts.Ops.Geteuid() != 0 {
		return errors.New("atualização requer privilégios de root (execute com: sudo vektord upgrade)")
	}

	if !opts.Yes {
		fmt.Fprintf(opts.Stdout, "\nProsseguir com a atualização? [s/N]: ")
		reader := bufio.NewReader(opts.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer != "s" && answer != "sim" && answer != "y" && answer != "yes" {
			fmt.Fprintf(opts.Stdout, "Atualização cancelada.\n")
			return nil
		}
	}

	tmpDir, err := os.MkdirTemp("", "vektor-upgrade-*")
	if err != nil {
		return fmt.Errorf("criar diretório temporário: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	fmt.Fprintf(opts.Stdout, "Baixando SHA256SUMS.txt...\n")
	checksumsPath := filepath.Join(tmpDir, "SHA256SUMS.txt")
	if err := downloadFile(ctx, opts.HTTPClient, checksumsAsset.BrowserDownloadURL, ua, checksumsPath, maxChecksumsBytes); err != nil {
		return fmt.Errorf("baixar SHA256SUMS.txt: %w", err)
	}
	fSums, err := os.Open(checksumsPath)
	if err != nil {
		return err
	}
	sums, err := ParseSHA256SUMS(fSums)
	_ = fSums.Close()
	if err != nil {
		return err
	}
	expectedHash, ok := sums[asset.Name]
	if !ok {
		return fmt.Errorf("checksum para %q não encontrado em SHA256SUMS.txt", asset.Name)
	}

	fmt.Fprintf(opts.Stdout, "Baixando %s...\n", asset.Name)
	assetPath := filepath.Join(tmpDir, asset.Name)
	if err := downloadFile(ctx, opts.HTTPClient, asset.BrowserDownloadURL, ua, assetPath, maxAssetBytes); err != nil {
		return fmt.Errorf("baixar %s: %w", asset.Name, err)
	}

	fmt.Fprintf(opts.Stdout, "Verificando integridade SHA-256...\n")
	if err := VerifyFileSHA256(assetPath, expectedHash); err != nil {
		return err
	}

	var backupPath string
	if !opts.NoBackup {
		backupPath, err = opts.Ops.BackupSQLite(ctx, dataDir, target)
		if err != nil {
			return fmt.Errorf("backup do SQLite: %w", err)
		}
		if backupPath != "" {
			fmt.Fprintf(opts.Stdout, "Backup do banco salvo em: %s\n", backupPath)
		}
	}

	fmt.Fprintf(opts.Stdout, "Aplicando atualização (%s)...\n", method)
	switch method {
	case MethodDeb:
		if err := opts.Ops.InstallDeb(ctx, assetPath, opts.Stdout, opts.Stderr); err != nil {
			return fmt.Errorf("instalar pacote .deb: %w", err)
		}
	case MethodTarball:
		if err := opts.Ops.InstallTarball(ctx, assetPath, opts.BinDir, opts.Stdout, opts.Stderr); err != nil {
			return fmt.Errorf("instalar binários do tarball: %w", err)
		}
	default:
		return fmt.Errorf("método de instalação desconhecido: %s", method)
	}

	fmt.Fprintf(opts.Stdout, "\nVektor atualizado com sucesso para v%s.\n", target)
	if backupPath != "" {
		fmt.Fprintf(opts.Stdout, "Backup do banco preservado em: %s\n", backupPath)
	}
	return nil
}

func expectedAssetName(method InstallMethod, version, arch string) string {
	if method == MethodDeb {
		return fmt.Sprintf("vektor_%s_%s.deb", version, arch)
	}
	return fmt.Sprintf("vektor_%s_linux_%s.tar.gz", version, arch)
}

func formatVersion(v string) string {
	if v == "" || v == "dev" {
		return "dev"
	}
	return "v" + NormalizeVersion(v)
}

func resolveDataDir(configured string) string {
	if configured != "" {
		return configured
	}
	if _, err := os.Stat("/var/lib/vektor/vektor.db"); err == nil {
		return "/var/lib/vektor"
	}
	return "/var/lib/vektor"
}

func backupPathFor(dataDir, targetVersion string) string {
	return filepath.Join(dataDir, fmt.Sprintf("vektor.db.pre-%s.bak", targetVersion))
}

func downloadFile(ctx context.Context, client *http.Client, rawURL, userAgent, destPath string, limit int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d ao baixar %s", resp.StatusCode, rawURL)
	}

	out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()

	n, err := io.Copy(out, io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return err
	}
	if n > limit {
		return fmt.Errorf("arquivo excede o limite de segurança de %d bytes", limit)
	}
	return out.Close()
}

type realSystemOps struct{}

func (realSystemOps) Geteuid() int {
	return os.Geteuid()
}

func (realSystemOps) DetectInstallMethod() InstallMethod {
	if _, err := exec.LookPath("dpkg-query"); err != nil {
		return MethodTarball
	}
	out, err := exec.Command("dpkg-query", "-W", "-f=${Status}", "vektor").Output()
	if err == nil && strings.Contains(string(out), "install ok installed") {
		return MethodDeb
	}
	return MethodTarball
}

func (realSystemOps) BackupSQLite(ctx context.Context, dataDir, targetVersion string) (string, error) {
	srcPath := filepath.Join(dataDir, "vektor.db")
	info, err := os.Stat(srcPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}

	dstPath := backupPathFor(dataDir, targetVersion)
	if _, err := os.Stat(dstPath); err == nil {
		dstPath = filepath.Join(dataDir, fmt.Sprintf("vektor.db.pre-%s-%d.bak", targetVersion, time.Now().UTC().Unix()))
	}

	// Prefer SQLite VACUUM INTO for a consistent online snapshot (includes WAL frames).
	if err := vacuumInto(ctx, srcPath, dstPath); err != nil {
		// Fallback to direct file copy if SQLite cannot open the file.
		if copyErr := copyFile(srcPath, dstPath, 0o640); copyErr != nil {
			return "", fmt.Errorf("vacuum (%v) e cópia (%v) falharam", err, copyErr)
		}
	}

	_ = os.Chmod(dstPath, 0o640)
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		_ = os.Chown(dstPath, int(st.Uid), int(st.Gid))
	}
	return dstPath, nil
}

func vacuumInto(ctx context.Context, srcPath, dstPath string) error {
	db, err := sql.Open("sqlite", srcPath)
	if err != nil {
		return err
	}
	defer db.Close()

	qCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Escape single quotes in path for SQLite VACUUM INTO literal.
	escaped := strings.ReplaceAll(dstPath, "'", "''")
	_, err = db.ExecContext(qCtx, fmt.Sprintf("VACUUM INTO '%s'", escaped))
	return err
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func (realSystemOps) InstallDeb(ctx context.Context, debPath string, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, "dpkg", "-i", debPath)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

func (realSystemOps) InstallTarball(ctx context.Context, tarballPath, binDir string, stdout, stderr io.Writer) error {
	stageDir, err := os.MkdirTemp("", "vektor-extract-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stageDir)

	if err := extractTarballBinaries(tarballPath, stageDir); err != nil {
		return err
	}

	newVektord := filepath.Join(stageDir, "vektord")
	newCtl := filepath.Join(stageDir, "vektor-ctl")
	if _, err := os.Stat(newVektord); err != nil {
		return errors.New("binário vektord ausente no tarball")
	}

	wasActive := isSystemdActive("vektord")

	targetVektord := filepath.Join(binDir, "vektord")
	bakVektord, err := atomicReplace(newVektord, targetVektord)
	if err != nil {
		return fmt.Errorf("substituir %s: %w", targetVektord, err)
	}

	var bakCtl string
	targetCtl := filepath.Join(binDir, "vektor-ctl")
	if _, err := os.Stat(newCtl); err == nil {
		bakCtl, err = atomicReplace(newCtl, targetCtl)
		if err != nil {
			rollbackBinary(bakVektord, targetVektord)
			return fmt.Errorf("substituir %s: %w", targetCtl, err)
		}
	}

	if _, err := exec.LookPath("systemctl"); err == nil {
		_ = runCmd(ctx, stdout, stderr, "systemctl", "daemon-reload")
		_ = runCmd(ctx, stdout, stderr, "systemctl", "try-restart", "vektor-ctl.socket")
		if err := runCmd(ctx, stdout, stderr, "systemctl", "try-restart", "vektord"); err != nil {
			rollbackBinary(bakVektord, targetVektord)
			rollbackBinary(bakCtl, targetCtl)
			_ = runCmd(ctx, stdout, stderr, "systemctl", "try-restart", "vektor-ctl.socket")
			_ = runCmd(ctx, stdout, stderr, "systemctl", "try-restart", "vektord")
			return fmt.Errorf("falha ao reiniciar vektord (rollback executado): %w", err)
		}
		if wasActive && !waitForSystemdActive("vektord", 5*time.Second) {
			rollbackBinary(bakVektord, targetVektord)
			rollbackBinary(bakCtl, targetCtl)
			_ = runCmd(ctx, stdout, stderr, "systemctl", "try-restart", "vektor-ctl.socket")
			_ = runCmd(ctx, stdout, stderr, "systemctl", "try-restart", "vektord")
			return errors.New("vektord não ficou ativo após atualização (rollback executado)")
		}
	}

	if bakVektord != "" {
		_ = os.Remove(bakVektord)
	}
	if bakCtl != "" {
		_ = os.Remove(bakCtl)
	}
	return nil
}

func extractTarballBinaries(tarballPath, dstDir string) error {
	f, err := os.Open(tarballPath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("abrir gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("ler tarball: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		clean := filepath.Clean(strings.TrimPrefix(hdr.Name, "./"))
		if clean != "vektord" && clean != "vektor-ctl" {
			continue
		}
		outPath := filepath.Join(dstDir, clean)
		out, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, io.LimitReader(tr, maxAssetBytes)); err != nil {
			_ = out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
	}
	return nil
}

func atomicReplace(src, dst string) (backupPath string, err error) {
	dir := filepath.Dir(dst)
	tmpNew := filepath.Join(dir, fmt.Sprintf(".%s.new.%d", filepath.Base(dst), os.Getpid()))
	if err := copyFile(src, tmpNew, 0o755); err != nil {
		return "", err
	}
	defer os.Remove(tmpNew)

	if _, statErr := os.Stat(dst); statErr == nil {
		backupPath = dst + ".bak"
		if err := copyFile(dst, backupPath, 0o755); err != nil {
			return "", fmt.Errorf("criar backup temporário %s: %w", backupPath, err)
		}
	}

	if err := os.Rename(tmpNew, dst); err != nil {
		if backupPath != "" {
			_ = os.Remove(backupPath)
		}
		return "", err
	}
	return backupPath, nil
}

func rollbackBinary(bak, dst string) {
	if bak == "" {
		return
	}
	_ = os.Rename(bak, dst)
}

func isSystemdActive(unit string) bool {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false
	}
	return exec.Command("systemctl", "is-active", "--quiet", unit).Run() == nil
}

func waitForSystemdActive(unit string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if isSystemdActive(unit) {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}

func runCmd(ctx context.Context, stdout, stderr io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}
