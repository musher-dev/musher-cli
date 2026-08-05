package main

import "testing"

func TestAuthCommandRegistersExpectedSubcommands(t *testing.T) {
	cmd := newAuthCmd()

	want := map[string]bool{
		"login":  false,
		"logout": false,
		"status": false,
	}

	for _, subcommand := range cmd.Commands() {
		if _, ok := want[subcommand.Name()]; ok {
			want[subcommand.Name()] = true
		}
	}

	for name, found := range want {
		if !found {
			t.Fatalf("missing auth subcommand %q", name)
		}
	}
}

func TestRootCommandRegistersAuthGroupInsteadOfFlatAuthVerbs(t *testing.T) {
	cmd := newRootCmd()

	var (
		foundAuth   bool
		foundLogin  bool
		foundLogout bool
		foundWhoami bool
	)

	for _, subcommand := range cmd.Commands() {
		switch subcommand.Name() {
		case "auth":
			foundAuth = true
		case "login":
			foundLogin = true
		case "logout":
			foundLogout = true
		case "whoami":
			foundWhoami = true
		}
	}

	if !foundAuth {
		t.Fatal("expected root auth command to be registered")
	}

	if foundLogin || foundLogout || foundWhoami {
		t.Fatalf("unexpected flat auth commands registered: login=%v logout=%v whoami=%v", foundLogin, foundLogout, foundWhoami)
	}
}

func TestAuthCommandRunsHelpByDefault(t *testing.T) {
	cmd := newAuthCmd()
	cmd.SetArgs(nil)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("auth help execution failed: %v", err)
	}
}
