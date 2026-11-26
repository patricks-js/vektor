package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"vektor/internal/update"
)

func runUpgrade(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	var opts update.Options
	opts.CurrentVersion = version
	opts.Stdin = stdin
	opts.Stdout = stdout
	opts.Stderr = stderr

	fs := flag.NewFlagSet("vektord upgrade", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.BoolVar(&opts.CheckOnly, "check", false, "Apenas verifica se há nova versão disponível (não requer root)")
	fs.StringVar(&opts.TargetVersion, "version", "", "Versão específica para instalar (ex.: 0.9.2)")
	fs.BoolVar(&opts.DryRun, "dry-run", false, "Mostra o plano de atualização sem alterar o sistema")
	fs.BoolVar(&opts.Yes, "yes", false, "Pula a confirmação interativa")
	fs.BoolVar(&opts.Yes, "y", false, "Atalho para --yes")
	fs.BoolVar(&opts.Force, "force", false, "Permite reinstalar a mesma versão ou fazer downgrade")
	fs.BoolVar(&opts.NoBackup, "no-backup", false, "Não cria backup do banco SQLite antes de atualizar")
	fs.StringVar(&opts.DataDir, "data-dir", envOrDefault("VEKTOR_DATA_DIR", "/var/lib/vektor"), "Diretório de dados contendo vektor.db para backup")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: sudo vektord upgrade [flags]")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() > 0 {
		return errors.New("argumentos inválidos; veja vektord upgrade --help")
	}

	return update.Run(ctx, opts)
}
