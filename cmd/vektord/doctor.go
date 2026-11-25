package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"vektor/internal/config"
	"vektor/internal/ctl"
	"vektor/internal/database"
	"vektor/internal/diagnostics"
	"vektor/internal/hostmgmt"
)

func runDoctor(ctx context.Context, cfg config.Config, ctlSocket string, asJSON bool, stdout io.Writer) error {
	var dbPing func(context.Context) error
	if db, err := database.Open(ctx, cfg); err == nil {
		defer db.Close()
		dbPing = db.Ping
	} else {
		openErr := err
		dbPing = func(context.Context) error { return openErr }
	}

	runner := &diagnostics.Runner{
		DBPing:        dbPing,
		Ctl:           ctl.NewClient(ctlSocket),
		Mgmt:          hostmgmt.New(cfg.ProcRoot),
		CtlSocketPath: ctlSocket,
	}
	report := runner.Run(ctx)

	if asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(stdout, "Vektor Doctor (%s) — status geral: %s\n\n", version, report.Status)
		for _, c := range report.Checks {
			tag := "[OK]  "
			switch c.Status {
			case diagnostics.StatusWarn:
				tag = "[WARN]"
			case diagnostics.StatusFail:
				tag = "[FAIL]"
			}
			fmt.Fprintf(stdout, "%s %s: %s\n", tag, c.Title, c.Summary)
			if c.Detail != "" {
				fmt.Fprintf(stdout, "       Detalhe: %s\n", c.Detail)
			}
			if c.Hint != "" {
				fmt.Fprintf(stdout, "       Dica:    %s\n", c.Hint)
			}
			if c.Command != "" {
				fmt.Fprintf(stdout, "       Comando: %s\n", c.Command)
			}
		}
	}

	if report.Status == diagnostics.StatusFail {
		return errors.New("doctor detected one or more failing checks")
	}
	return nil
}
