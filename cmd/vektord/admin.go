package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/term"

	"vektor/internal/auth"
)

func runAdmin(ctx context.Context, command string, sessions *auth.Store, stdin io.Reader, stdout, stderr io.Writer) error {
	if command == "revoke-sessions" {
		if err := sessions.RevokeAll(ctx); err != nil {
			return fmt.Errorf("revoke sessions: %w", err)
		}
		_, err := fmt.Fprintln(stdout, "Administrative sessions revoked")
		return err
	}

	password, err := readPassword(stdin, stderr)
	defer clear(password)
	if err != nil {
		return err
	}
	if err := sessions.Bootstrap(ctx, password); err != nil {
		return fmt.Errorf("bootstrap administrator: %w", err)
	}
	_, err = fmt.Fprintln(stdout, "Administrator created (username: admin)")
	return err
}

func readPassword(stdin io.Reader, stderr io.Writer) ([]byte, error) {
	if f, ok := stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		read := func(prompt string) ([]byte, error) {
			fmt.Fprint(stderr, prompt)
			line, err := term.ReadPassword(int(f.Fd()))
			fmt.Fprintln(stderr)
			return line, err
		}
		password, err := read("Administrator password: ")
		if err != nil {
			clear(password)
			return nil, err
		}
		confirmation, err := read("Confirm password: ")
		defer clear(confirmation)
		if err != nil || !bytes.Equal(password, confirmation) {
			clear(password)
			if err != nil {
				return nil, err
			}
			return nil, errors.New("passwords do not match")
		}
		return password, nil
	}

	data, err := io.ReadAll(io.LimitReader(stdin, auth.MaxPasswordBytes+3))
	if err != nil {
		clear(data)
		return nil, err
	}
	password := bytes.TrimSuffix(data, []byte("\n"))
	if len(password) != len(data) {
		password = bytes.TrimSuffix(password, []byte("\r"))
	}
	if len(password) > auth.MaxPasswordBytes || bytes.ContainsAny(password, "\r\n") {
		clear(data)
		return nil, errors.New("standard input must contain one password line with at most 1024 bytes")
	}
	return password, nil
}
