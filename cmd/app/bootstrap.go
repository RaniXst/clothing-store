package main

import (
	"bufio"
	"context"
	"crypto/subtle"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/RaniXst/clothing-store/internal/auth"
	"github.com/RaniXst/clothing-store/internal/config"
	"github.com/RaniXst/clothing-store/internal/database"
	"github.com/RaniXst/clothing-store/internal/user"
	"github.com/RaniXst/clothing-store/migrations"
)

func bootstrapAdmin() error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("bootstrap-admin requires an interactive terminal; password is never accepted as an argument")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx := context.Background()
	db, err := database.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := migrations.Apply(ctx, db); err != nil {
		return err
	}
	repository := user.NewSQLRepository(db)
	service := user.NewService(repository, auth.PasswordHasher{})

	reader := bufio.NewReader(os.Stdin)
	readField := func(label string) (string, error) {
		if _, err := fmt.Fprint(os.Stderr, label); err != nil {
			return "", err
		}
		value, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(value), nil
	}
	name, err := readField("Administrator name: ")
	if err != nil {
		return err
	}
	email, err := readField("Administrator email: ")
	if err != nil {
		return err
	}
	phone, err := readField("Administrator phone: ")
	if err != nil {
		return err
	}
	if _, err := fmt.Fprint(os.Stderr, "Password (hidden): "); err != nil {
		return err
	}
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	_, _ = fmt.Fprintln(os.Stderr)
	if err != nil {
		return fmt.Errorf("read password securely: %w", err)
	}
	defer clear(password)
	if _, err := fmt.Fprint(os.Stderr, "Confirm password (hidden): "); err != nil {
		return err
	}
	confirmation, err := term.ReadPassword(int(os.Stdin.Fd()))
	_, _ = fmt.Fprintln(os.Stderr)
	if err != nil {
		return fmt.Errorf("read password confirmation securely: %w", err)
	}
	defer clear(confirmation)
	if subtle.ConstantTimeCompare(password, confirmation) != 1 {
		return fmt.Errorf("passwords do not match")
	}
	if _, err := service.BootstrapAdmin(ctx, name, email, phone, string(password)); err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, "Initial administrator created.")
	return err
}
