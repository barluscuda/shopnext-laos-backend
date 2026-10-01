package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"shopnext-laos/internal/application"
	"shopnext-laos/internal/bootstrap"
	"shopnext-laos/internal/domain"
	"strings"
)

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
	password, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && password == "" {
		return err
	}
	rt, err := bootstrap.Open(context.Background())
	if err != nil {
		return err
	}
	defer rt.Close()
	user, err := rt.Service.CreateStaff(context.Background(), domain.Actor{}, application.StaffInput{Email: *email, Name: *name, Role: "OWNER", Password: strings.TrimRight(password, "\r\n")}, true, "")
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
