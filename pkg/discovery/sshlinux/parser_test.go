package sshlinux

import (
	"reflect"
	"testing"
)

func TestParseOSRelease(t *testing.T) {
	name, version := ParseOSRelease("NAME=\"Ubuntu\"\nVERSION_ID=\"24.04\"\n")
	if name != "Ubuntu" || version != "24.04" {
		t.Fatalf("got name=%q version=%q", name, version)
	}
}

func TestParseInterfaces(t *testing.T) {
	interfaces, err := ParseInterfaces(`[{"ifindex":1,"ifname":"eth0","address":"02:54:00:00:00:01","addr_info":[{"local":"10.1.0.2","prefixlen":24}]}]`)
	if err != nil {
		t.Fatal(err)
	}
	want := []Interface{{Index: 1, Name: "eth0", MAC: "02:54:00:00:00:01", Addresses: []string{"10.1.0.2/24"}}}
	if !reflect.DeepEqual(interfaces, want) {
		t.Fatalf("got %#v, want %#v", interfaces, want)
	}
	if _, err := ParseInterfaces(`[{"broken":`); err == nil {
		t.Fatal("expected malformed interface JSON to fail")
	}
}

func TestParseLinesNormalizesServicesAndDeduplicates(t *testing.T) {
	got := ParseServices("sshd.service loaded active running SSH\nnginx.service loaded active running nginx\nsshd.service duplicate\n")
	want := []string{"sshd", "nginx"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestOnlyAuditedCommandsAreAllowed(t *testing.T) {
	if !IsAllowedCommand(CommandHostname) {
		t.Fatal("audited hostname command was rejected")
	}
	if IsAllowedCommand("whoami") || IsAllowedCommand("hostname; id") {
		t.Fatal("arbitrary command was accepted")
	}
}
