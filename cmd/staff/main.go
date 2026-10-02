package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"shopnext-laos/internal/application"
	"shopnext-laos/internal/bootstrap"
	"shopnext-laos/internal/domain"
	"strings"
	"time"

	"golang.org/x/term"
)

func readPassword() (string, error) {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprint(os.Stderr, "Owner password (at least 10 characters): ")
		password, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", fmt.Errorf("read password: %w", err)
		}
		return string(password), nil
	}
	password, err := bufio.NewReader(io.LimitReader(os.Stdin, 1027)).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read password: %w", err)
	}
	return strings.TrimRight(password, "\r\n"), nil
}

func run() error {
	flags := flag.NewFlagSet("staff", flag.ContinueOnError)
	email := flags.String("email", "", "first owner email")
	name := flags.String("name", "", "first owner display name")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}
	if *email == "" || *name == "" || flags.NArg() != 0 {
		return fmt.Errorf("usage: staff --email EMAIL --name NAME; provide password on stdin")
	}
	password, err := readPassword()
	if err != nil {
		return err
	}
	if len(password) < 10 || len(password) > 1024 {
		return fmt.Errorf("owner password must be between 10 and 1024 bytes; provide one line on stdin")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rt, err := bootstrap.Open(ctx)
	if err != nil {
		return err
	}
	defer rt.Close()
	user, err := rt.Service.CreateStaff(ctx, domain.Actor{}, application.StaffInput{Email: *email, Name: *name, Role: "OWNER", Password: password}, true, "")
	if err != nil {
		return err
	}
	fmt.Println("Created first OWNER:", user.Email)
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
