// Package diagnostics runs on-demand, actionable health checks for the Linux host,
// SQLite persistence, NTP clock discipline, DNS resolution, journald access,
// vektor-ctl privilege helper, and Incus virtualization.
package diagnostics

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"vektor/internal/ctl"
	"vektor/internal/hostmgmt"
	"vektor/internal/incus"
	"vektor/internal/observability"
)

type Status string

const (
	StatusOK   Status = "ok"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
)

type Check struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Status  Status `json:"status"`
	Summary string `json:"summary"`
	Detail  string `json:"detail,omitempty"`
	Hint    string `json:"hint,omitempty"`
	Command string `json:"command,omitempty"`
}

type Report struct {
	Status      Status  `json:"status"`
	GeneratedAt string  `json:"generated_at"`
	Checks      []Check `json:"checks"`
}

type Runner struct {
	Health          *observability.HealthTracker
	DBPing          func(context.Context) error
	Ctl             *ctl.Client
	Mgmt            *hostmgmt.Manager
	ResolvConfPath  string
	CtlSocketPath   string
	IncusSocketPath string

	// Test hooks; left nil in production.
	readAdjtimex   func() (int64, error)
	unitState      func(ctx context.Context, unit string) (loadState, activeState, subState string, ok bool)
	unitLogs       func(ctx context.Context, unit string, lines int, priority string) ([]hostmgmt.LogEntry, error)
	lookPath       func(file string) (string, error)
	lstat          func(path string) (os.FileInfo, error)
	stat           func(path string) (os.FileInfo, error)
	readFile       func(path string) ([]byte, error)
	ctlIncusStatus func(ctx context.Context) (incus.Status, error)
}

func (r *Runner) resolvPath() string {
	if r.ResolvConfPath != "" {
		return r.ResolvConfPath
	}
	return "/etc/resolv.conf"
}

func (r *Runner) ctlSocket() string {
	if r.CtlSocketPath != "" {
		return r.CtlSocketPath
	}
	return ctl.DefaultSocketPath
}

func (r *Runner) incusSocket() string {
	if r.IncusSocketPath != "" {
		return r.IncusSocketPath
	}
	return incus.DefaultSocketPath
}

func (r *Runner) getAdjtimex() (int64, error) {
	if r.readAdjtimex != nil {
		return r.readAdjtimex()
	}
	var timex unix.Timex
	if _, err := unix.Adjtimex(&timex); err != nil {
		return 0, err
	}
	return timex.Maxerror, nil
}

func (r *Runner) getUnitState(ctx context.Context, unit string) (string, string, string, bool) {
	if r.unitState != nil {
		return r.unitState(ctx, unit)
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return "", "", "", false
	}
	cCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cCtx, "systemctl", "show", "-p", "LoadState", "-p", "ActiveState", "-p", "SubState", "--value", unit)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", "", "", false
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) < 3 {
		return "", "", "", false
	}
	return strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1]), strings.TrimSpace(lines[2]), true
}

func (r *Runner) getUnitLogs(ctx context.Context, unit string, lines int, priority string) ([]hostmgmt.LogEntry, error) {
	if r.unitLogs != nil {
		return r.unitLogs(ctx, unit, lines, priority)
	}
	if r.Mgmt == nil {
		return nil, hostmgmt.ErrUnavailable
	}
	cCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return r.Mgmt.ServiceLogs(cCtx, unit, lines, priority)
}

func (r *Runner) findBinary(name string) bool {
	if r.lookPath != nil {
		_, err := r.lookPath(name)
		return err == nil
	}
	_, err := exec.LookPath(name)
	return err == nil
}

func (r *Runner) doLstat(path string) (os.FileInfo, error) {
	if r.lstat != nil {
		return r.lstat(path)
	}
	return os.Lstat(path)
}

func (r *Runner) doStat(path string) (os.FileInfo, error) {
	if r.stat != nil {
		return r.stat(path)
	}
	return os.Stat(path)
}

func (r *Runner) doReadFile(path string) ([]byte, error) {
	if r.readFile != nil {
		return r.readFile(path)
	}
	return os.ReadFile(path)
}

func (r *Runner) queryIncusStatus(ctx context.Context) (incus.Status, error) {
	if r.ctlIncusStatus != nil {
		return r.ctlIncusStatus(ctx)
	}
	if r.Ctl == nil {
		return incus.Status{}, ctl.ErrUnavailable
	}
	cCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	resp, err := r.Ctl.Do(cCtx, ctl.Request{Action: "incus.status"})
	if err != nil {
		return incus.Status{}, err
	}
	if !resp.OK {
		return incus.Status{Available: false, Reason: resp.Error}, nil
	}
	var st incus.Status
	if err := json.Unmarshal(resp.Data, &st); err != nil {
		return incus.Status{Available: false, Reason: "Malformed Incus status response"}, nil
	}
	return st, nil
}

// Run executes all diagnostic checks and aggregates the overall status.
func (r *Runner) Run(ctx context.Context) Report {
	checks := []Check{
		r.checkClock(ctx),
		r.checkDNS(ctx),
		r.checkDatabase(ctx),
		r.checkJournald(ctx),
		r.checkCtl(ctx),
		r.checkIncus(ctx),
	}
	overall := StatusOK
	for _, c := range checks {
		if c.Status == StatusFail {
			overall = StatusFail
			break
		}
		if c.Status == StatusWarn && overall == StatusOK {
			overall = StatusWarn
		}
	}
	return Report{
		Status:      overall,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Checks:      checks,
	}
}

func (r *Runner) checkClock(ctx context.Context) Check {
	c := Check{ID: "clock", Title: "Relógio e NTP"}
	var clockErr string
	var jumps int64
	if r.Health != nil {
		snap := r.Health.Snapshot()
		clockErr = snap.ClockError
		jumps = snap.WallClockJumpCount
	}

	maxErrUs, adjErr := r.getAdjtimex()
	maxErrSec := float64(maxErrUs) / 1_000_000.0
	ntpService, ntpActive := r.detectNTPService(ctx)

	switch {
	case clockErr == "waiting for the synchronized wall clock to stabilize":
		c.Status = StatusWarn
		c.Summary = "Relógio sincronizado, aguardando estabilização (30s)"
		c.Detail = fmt.Sprintf("Erro máximo atual: %.2fs · saltos detectados: %d", maxErrSec, jumps)
		c.Hint = "Após um salto de relógio (> 5s), o Vektor aguarda 30s estáveis antes de retomar escritas históricas."
		return c
	case clockErr == "wall clock is behind previously trusted history":
		c.Status = StatusFail
		c.Summary = "Relógio do sistema está atrasado em relação ao histórico salvo"
		c.Detail = fmt.Sprintf("Erro máximo: %.2fs · %s", maxErrSec, clockErr)
		c.Hint = "Ajuste a data/hora do servidor via NTP para evitar sobrescrever séries históricas."
		c.Command = "timedatectl status && sudo timedatectl set-ntp true"
		return c
	case adjErr != nil:
		c.Status = StatusWarn
		c.Summary = "Não foi possível consultar adjtimex do kernel"
		c.Detail = adjErr.Error()
		c.Command = "timedatectl status"
		return c
	case maxErrUs < 0 || maxErrUs >= 16_000_000 || clockErr != "":
		c.Status = StatusFail
		c.Summary = "Relógio dessincronizado — escritas históricas pausadas"
		if ntpService != "" {
			state := "inativo"
			if ntpActive {
				state = "ativo, mas sem sincronizar"
			}
			c.Detail = fmt.Sprintf("adjtimex maxerror = %.2fs (limite: 16s) · %s está %s", maxErrSec, ntpService, state)
		} else {
			c.Detail = fmt.Sprintf("adjtimex maxerror = %.2fs (limite: 16s) · nenhum serviço NTP detectado", maxErrSec)
		}
		if !ntpActive {
			c.Hint = "Ative e reinicie o cliente NTP do sistema (systemd-timesyncd ou chrony)."
		} else {
			c.Hint = "O cliente NTP está ativo mas não conseguiu sincronizar. Verifique DNS, rede e porta UDP 123."
		}
		if ntpService == "chrony.service" || ntpService == "chronyd.service" {
			c.Command = "sudo systemctl restart " + ntpService + " && chronyc tracking"
		} else {
			c.Command = "sudo timedatectl set-ntp true && sudo systemctl restart systemd-timesyncd && timedatectl timesync-status"
		}
		return c
	default:
		c.Status = StatusOK
		c.Summary = fmt.Sprintf("Relógio sincronizado (erro máximo: %.2fs)", maxErrSec)
		if ntpService != "" && ntpActive {
			c.Detail = fmt.Sprintf("Cliente NTP ativo: %s", ntpService)
		}
		return c
	}
}

func (r *Runner) detectNTPService(ctx context.Context) (string, bool) {
	for _, unit := range []string{"systemd-timesyncd.service", "chrony.service", "chronyd.service"} {
		load, active, _, ok := r.getUnitState(ctx, unit)
		if !ok || load == "not-found" || load == "" {
			continue
		}
		return unit, active == "active"
	}
	return "", false
}

func (r *Runner) checkDNS(ctx context.Context) Check {
	c := Check{ID: "dns", Title: "Resolução DNS"}
	path := r.resolvPath()

	linfo, lerr := r.doLstat(path)
	if lerr != nil {
		c.Status = StatusFail
		c.Summary = fmt.Sprintf("%s não encontrado", path)
		c.Detail = lerr.Error()
		c.Hint = "Restaure o link simbólico do systemd-resolved para /etc/resolv.conf."
		c.Command = "sudo ln -sf /run/systemd/resolve/stub-resolv.conf /etc/resolv.conf && sudo systemctl restart systemd-resolved"
		return c
	}
	if _, serr := r.doStat(path); serr != nil {
		c.Status = StatusFail
		c.Summary = fmt.Sprintf("Symlink %s quebrado", path)
		if linfo != nil && linfo.Mode()&os.ModeSymlink != 0 {
			c.Detail = fmt.Sprintf("O destino do link simbólico %s não existe (%v).", path, serr)
		} else {
			c.Detail = serr.Error()
		}
		c.Hint = "Um serviço de rede ou VPN pode ter corrompido /etc/resolv.conf. Restaure o symlink do systemd-resolved."
		c.Command = "sudo ln -sf /run/systemd/resolve/stub-resolv.conf /etc/resolv.conf && sudo systemctl restart systemd-resolved"
		return c
	}

	raw, err := r.doReadFile(path)
	if err != nil {
		c.Status = StatusFail
		c.Summary = fmt.Sprintf("Não foi possível ler %s", path)
		c.Detail = err.Error()
		return c
	}
	var servers []string
	usesStub := false
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "nameserver ") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				servers = append(servers, fields[1])
				if fields[1] == "127.0.0.53" {
					usesStub = true
				}
			}
		}
	}
	if len(servers) == 0 {
		c.Status = StatusWarn
		c.Summary = fmt.Sprintf("Nenhum nameserver configurado em %s", path)
		c.Hint = "Sem servidores DNS, NTP e downloads de imagens do Incus falharão."
		c.Command = "resolvectl status"
		return c
	}
	if usesStub {
		load, active, sub, ok := r.getUnitState(ctx, "systemd-resolved.service")
		if ok && load != "not-found" && active != "active" {
			c.Status = StatusFail
			c.Summary = "systemd-resolved não está ativo"
			c.Detail = fmt.Sprintf("%s aponta para 127.0.0.53, mas systemd-resolved.service está %s (%s).", path, active, sub)
			c.Hint = "Reinicie o systemd-resolved para restaurar a resolução de nomes."
			c.Command = "sudo systemctl restart systemd-resolved && resolvectl status"
			return c
		}
	}
	c.Status = StatusOK
	c.Summary = fmt.Sprintf("DNS configurado (%s)", strings.Join(servers, ", "))
	return c
}

func (r *Runner) checkDatabase(ctx context.Context) Check {
	c := Check{ID: "database", Title: "SQLite e coletores"}
	if r.DBPing != nil {
		if err := r.DBPing(ctx); err != nil {
			c.Status = StatusFail
			c.Summary = "Falha ao acessar o banco SQLite"
			c.Detail = err.Error()
			c.Hint = "Verifique permissões e espaço em disco no diretório de dados (/var/lib/vektor)."
			c.Command = "df -h /var/lib/vektor && journalctl -u vektord -n 30 --no-pager"
			return c
		}
	}
	if r.Health != nil {
		snap := r.Health.Snapshot()
		if !snap.DBOK {
			c.Status = StatusFail
			c.Summary = "Erro de escrita ou manutenção no SQLite"
			c.Detail = snap.LastDBError
			c.Hint = "Verifique espaço em disco e logs do vektord."
			c.Command = "journalctl -u vektord -n 30 --no-pager"
			return c
		}
		if snap.CollectorErrorCount > 0 || snap.RollupLagS > 600 {
			c.Status = StatusWarn
			c.Summary = fmt.Sprintf("Coleta com %d erro(s) acumulado(s)", snap.CollectorErrorCount)
			var details []string
			if snap.RollupLagS > 600 {
				details = append(details, fmt.Sprintf("atraso de agregação: %ds", snap.RollupLagS))
			}
			for name, count := range snap.CollectorErrorsByName {
				if last, ok := snap.CollectorLastErrors[name]; ok && last.Message != "" {
					details = append(details, fmt.Sprintf("%s (%dx): %s", name, count, last.Message))
				} else {
					details = append(details, fmt.Sprintf("%s (%dx)", name, count))
				}
			}
			c.Detail = strings.Join(details, " · ")
			c.Hint = "Verifique os logs do vektord para inspecionar falhas recorrentes nos coletores."
			c.Command = "journalctl -u vektord -p warning -n 20 --no-pager"
			return c
		}
	}
	c.Status = StatusOK
	c.Summary = "Banco SQLite e coletores operando normalmente"
	return c
}

func (r *Runner) checkJournald(ctx context.Context) Check {
	c := Check{ID: "journald", Title: "Logs do sistema (journald)"}
	_, err := r.getUnitLogs(ctx, "vektord.service", 1, "")
	if err == nil {
		c.Status = StatusOK
		c.Summary = "Leitura do journald disponível"
		return c
	}
	if errors.Is(err, hostmgmt.ErrPermission) {
		c.Status = StatusWarn
		c.Summary = "Sem permissão para ler logs do journald"
		c.Detail = err.Error()
		c.Hint = "Adicione o usuário do serviço ao grupo systemd-journal para visualizar logs de serviços na interface."
		c.Command = "sudo usermod -aG systemd-journal vektor && sudo systemctl restart vektord"
		return c
	}
	if errors.Is(err, hostmgmt.ErrUnavailable) {
		c.Status = StatusWarn
		c.Summary = "journalctl indisponível neste ambiente"
		return c
	}
	c.Status = StatusWarn
	c.Summary = "Falha ao consultar journald"
	c.Detail = err.Error()
	return c
}

func (r *Runner) checkCtl(ctx context.Context) Check {
	c := Check{ID: "ctl", Title: "Helper privilegiado (vektor-ctl)"}
	sockPath := r.ctlSocket()
	_, err := r.doStat(sockPath)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			c.Status = StatusFail
			c.Summary = "Sem permissão para acessar o socket vektor-ctl"
			c.Detail = fmt.Sprintf("%s: %v", sockPath, err)
			c.Hint = "Verifique se /run/vektor tem permissão 0755 e se o socket pertence ao grupo vektor."
			c.Command = "sudo chmod 0755 /run/vektor && sudo systemctl restart vektor-ctl.socket"
			return c
		}
		load, active, sub, ok := r.getUnitState(ctx, "vektor-ctl.socket")
		c.Status = StatusWarn
		c.Summary = "Socket vektor-ctl.socket não está ativo"
		if ok && load != "not-found" && load != "" {
			c.Detail = fmt.Sprintf("vektor-ctl.socket está %s (%s); %s ausente.", active, sub, sockPath)
		} else {
			c.Detail = fmt.Sprintf("Socket %s não encontrado.", sockPath)
		}
		c.Hint = "Sem o helper, ações administrativas (serviços, energia do host e Incus) ficam desabilitadas."
		c.Command = "sudo systemctl enable --now vektor-ctl.socket"
		return c
	}
	c.Status = StatusOK
	c.Summary = fmt.Sprintf("Socket %s disponível", sockPath)
	return c
}

func (r *Runner) checkIncus(ctx context.Context) Check {
	c := Check{ID: "incus", Title: "Virtualização (Incus)"}
	st, err := r.queryIncusStatus(ctx)
	if err == nil && st.Available {
		c.Status = StatusOK
		c.Summary = fmt.Sprintf("Incus %s ativo (%d instâncias, %d em execução)", st.ServerVersion, st.InstanceCount, st.RunningCount)
		return c
	}

	// Determine whether Incus is actually installed on this host.
	hasBin := r.findBinary("incus") || r.findBinary("incusd")
	svcLoad, svcActive, svcSub, svcOK := r.getUnitState(ctx, "incus.service")
	sockLoad, sockActive, sockSub, sockOK := r.getUnitState(ctx, "incus.socket")
	hasUnit := (svcOK && svcLoad != "" && svcLoad != "not-found") || (sockOK && sockLoad != "" && sockLoad != "not-found")

	if !hasBin && !hasUnit {
		c.Status = StatusOK
		c.Summary = "Incus não instalado (virtualização opcional)"
		c.Hint = "Instale o Incus quando quiser gerenciar VMs e system containers pelo Vektor."
		c.Command = "sudo apt install incus && sudo incus admin init --minimal"
		return c
	}

	// Incus IS installed, but unavailable — find out why!
	c.Status = StatusFail

	// Check if vektor-ctl itself was the blocker.
	if errors.Is(err, ctl.ErrUnavailable) {
		c.Summary = "Incus instalado, mas o helper vektor-ctl.socket está indisponível"
		c.Detail = fmt.Sprintf("incus.service: %s (%s) · vektor-ctl.socket inacessível", defaultStr(svcActive, "desconhecido"), defaultStr(svcSub, "-"))
		c.Hint = "Ative o vektor-ctl.socket para que o Vektor consiga falar com o socket do Incus."
		c.Command = "sudo systemctl enable --now vektor-ctl.socket"
		return c
	}

	// Detect known conflicting daemons (e.g. LXD running side-by-side).
	var conflicts []string
	for _, other := range []string{"lxd.service", "snap.lxd.daemon.service", "libvirtd.service"} {
		_, active, _, ok := r.getUnitState(ctx, other)
		if ok && active == "active" {
			conflicts = append(conflicts, other)
		}
	}

	// Fetch recent error/warning logs from incus.service to surface silent startup conflicts.
	var logSnippet string
	if entries, logErr := r.getUnitLogs(ctx, "incus.service", 12, "warning"); logErr == nil && len(entries) > 0 {
		var msgs []string
		start := len(entries) - 3
		if start < 0 {
			start = 0
		}
		for _, e := range entries[start:] {
			msg := strings.TrimSpace(e.Message)
			if msg != "" {
				msgs = append(msgs, msg)
			}
		}
		logSnippet = strings.Join(msgs, " | ")
		if len(logSnippet) > 320 {
			logSnippet = logSnippet[:317] + "..."
		}
	}

	lowerLog := strings.ToLower(logSnippet + " " + st.Reason)
	switch {
	case svcOK && svcActive == "failed":
		c.Summary = "Incus instalado, mas incus.service falhou ao iniciar"
	case svcOK && svcActive != "active" && sockOK && sockActive != "active":
		c.Summary = "Incus instalado, mas o serviço está parado"
	default:
		c.Summary = "Incus instalado, mas o daemon não respondeu no socket"
	}

	var detailParts []string
	if svcOK && svcLoad != "not-found" {
		detailParts = append(detailParts, fmt.Sprintf("incus.service=%s/%s", svcActive, svcSub))
	}
	if sockOK && sockLoad != "not-found" {
		detailParts = append(detailParts, fmt.Sprintf("incus.socket=%s/%s", sockActive, sockSub))
	}
	if len(conflicts) > 0 {
		detailParts = append(detailParts, fmt.Sprintf("serviços possivelmente conflitantes ativos: %s", strings.Join(conflicts, ", ")))
	}
	if st.Reason != "" {
		detailParts = append(detailParts, st.Reason)
	}
	if logSnippet != "" {
		detailParts = append(detailParts, "últimos logs: "+logSnippet)
	}
	c.Detail = strings.Join(detailParts, " · ")

	switch {
	case strings.Contains(lowerLog, "address already in use") || strings.Contains(lowerLog, "dnsmasq"):
		c.Hint = "Conflito de porta/endereço (provavelmente dnsmasq, LXD ou outra bridge ocupando a porta 53/67). Verifique qual processo está usando a porta."
		c.Command = "sudo ss -tulpn | grep -E ':53 |:67 ' && journalctl -u incus -n 30 --no-pager"
	case len(conflicts) > 0:
		c.Hint = fmt.Sprintf("Detectado %s ativo no host, o que frequentemente conflita com bridges/sockets do Incus.", strings.Join(conflicts, ", "))
		c.Command = "journalctl -u incus -n 30 --no-pager"
	case svcOK && svcActive != "active":
		c.Hint = "Inicie o serviço do Incus e verifique os logs caso ele volte a falhar."
		c.Command = "sudo systemctl enable --now incus.socket incus.service && journalctl -u incus -n 30 --no-pager"
	default:
		c.Hint = fmt.Sprintf("Verifique o status do Incus e se o socket %s está acessível.", r.incusSocket())
		c.Command = "sudo systemctl status incus.socket incus.service && journalctl -u incus -n 30 --no-pager"
	}
	return c
}

func defaultStr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
