package servicenowpackage

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestNormalizeIsDeterministicAndValidatesContract(t *testing.T) {
	entries := validEntries()
	secondEntries := validEntries()
	secondEntries["update/sys_module_bom.xml"] = []byte(strings.ReplaceAll(string(secondEntries["update/sys_module_bom.xml"]), "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"))
	secondEntries["update/sys_module_bom.xml"] = []byte(strings.ReplaceAll(string(secondEntries["update/sys_module_bom.xml"]), "2026-09-02T01:02:03.000Z", "2026-09-03T04:05:06.000Z"))
	secondEntries["package_inventory.csv"] = inventory(secondEntries)
	firstInput := filepath.Join(t.TempDir(), "first-sdk.zip")
	secondInput := filepath.Join(t.TempDir(), "second-sdk.zip")
	writeFixtureZIP(t, firstInput, entries, time.Date(2026, 9, 2, 1, 2, 3, 0, time.UTC), false)
	writeFixtureZIP(t, secondInput, secondEntries, time.Date(2026, 9, 3, 4, 5, 6, 0, time.UTC), true)

	firstOutput := filepath.Join(t.TempDir(), "first.zip")
	secondOutput := filepath.Join(t.TempDir(), "second.zip")
	first, err := Normalize(firstInput, firstOutput)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Normalize(secondInput, secondOutput)
	if err != nil {
		t.Fatal(err)
	}
	firstBytes, _ := os.ReadFile(firstOutput)
	secondBytes, _ := os.ReadFile(secondOutput)
	if !bytes.Equal(firstBytes, secondBytes) || first.SHA256 != second.SHA256 {
		t.Fatal("normalized ServiceNow packages differ")
	}
	if first.Scope != Scope || first.AppVersion != AppVersion || first.Artifact != ArtifactName || first.Entries != len(entries) {
		t.Fatalf("metadata = %#v", first)
	}
	reader, err := zip.OpenReader(firstOutput)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	for _, file := range reader.File {
		if !strings.HasPrefix(file.Name, "/") || !file.ModTime().Equal(fixedTime) {
			t.Fatalf("noncanonical ZIP header: %q at %s", file.Name, file.ModTime())
		}
	}
}

func TestNormalizeRejectsContractDriftAndBadInventory(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(map[string][]byte)
		want   string
	}{
		{
			name: "missing table",
			mutate: func(entries map[string][]byte) {
				delete(entries, "dictionary/"+Scope+"_task.xml")
				entries["package_inventory.csv"] = inventory(entries)
			},
			want: "tables =",
		},
		{
			name: "role drift",
			mutate: func(entries map[string][]byte) {
				entries["update/sys_user_role_0.xml"] = []byte("<record><name>" + Scope + ".arbitrary</name></record>")
				entries["package_inventory.csv"] = inventory(entries)
			},
			want: "roles =",
		},
		{
			name: "digest mismatch",
			mutate: func(entries map[string][]byte) {
				entries["scope/sys_app_0.xml"] = append(entries["scope/sys_app_0.xml"], ' ')
			},
			want: "digest mismatch",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			entries := validEntries()
			test.mutate(entries)
			input := filepath.Join(t.TempDir(), "input.zip")
			writeFixtureZIP(t, input, entries, time.Now(), false)
			_, err := Normalize(input, filepath.Join(t.TempDir(), "output.zip"))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Normalize() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestNormalizeRejectsUnsafeDuplicateAndOverlargeEntries(t *testing.T) {
	for _, test := range []struct {
		name  string
		files []fixtureFile
		want  string
	}{
		{name: "unsafe", files: []fixtureFile{{name: "/../escape", body: []byte("x")}}, want: "unsafe"},
		{name: "duplicate", files: []fixtureFile{{name: "/same", body: []byte("x")}, {name: "same", body: []byte("x")}}, want: "duplicate"},
		{name: "overlarge", files: []fixtureFile{{name: "/large", body: make([]byte, maxEntryBytes+1)}}, want: "exceeds bounds"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := filepath.Join(t.TempDir(), "input.zip")
			writeFilesZIP(t, input, test.files)
			_, err := Normalize(input, filepath.Join(t.TempDir(), "output.zip"))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Normalize() error = %v, want %q", err, test.want)
			}
		})
	}
}

type fixtureFile struct {
	name string
	body []byte
}

func validEntries() map[string][]byte {
	entries := make(map[string][]byte)
	for _, table := range expectedTables {
		entries["dictionary/"+table+".xml"] = []byte("<database><element label=\"Topo\"/></database>")
	}
	for index, role := range expectedRoles {
		entries[fmt.Sprintf("update/sys_user_role_%d.xml", index)] = []byte("<record><name>" + role + "</name></record>")
	}
	for index, resource := range expectedResources {
		parts := strings.SplitN(resource, " ", 2)
		entries[fmt.Sprintf("update/sys_ws_operation_%d.xml", index)] = []byte("<record><active>true</active><http_method>" + parts[0] + "</http_method><relative_path>" + parts[1] + "</relative_path></record>")
	}
	entries["scope/sys_app_0.xml"] = []byte("<record><name>Nischoy Topo</name><scope>" + Scope + "</scope><version>" + AppVersion + "</version></record>")
	entries["update/sys_module_bom.xml"] = []byte(`<record><path>` + Scope + `/@nischoy/topo-servicenow-control-plane/` + AppVersion + `/bom.json</path><content><![CDATA[{"serialNumber": "urn:uuid:11111111-1111-4111-8111-111111111111", "metadata": {"timestamp": "2026-09-02T01:02:03.000Z"}}]]></content></record>`)
	entries["package_inventory.csv"] = inventory(entries)
	return entries
}

func inventory(entries map[string][]byte) []byte {
	var names []string
	for name := range entries {
		if name != "package_inventory.csv" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var body strings.Builder
	fmt.Fprintf(&body, "#build=%s\n#appVersion=%s\n#type=scoped\n#version=1.0.0\n", Scope, AppVersion)
	for _, name := range names {
		digest := sha256.Sum256(entries[name])
		fmt.Fprintf(&body, "%s;%s\n", name, hex.EncodeToString(digest[:]))
	}
	return []byte(body.String())
}

func writeFixtureZIP(t *testing.T, filename string, entries map[string][]byte, modified time.Time, reverse bool) {
	t.Helper()
	var names []string
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	if reverse {
		for left, right := 0, len(names)-1; left < right; left, right = left+1, right-1 {
			names[left], names[right] = names[right], names[left]
		}
	}
	files := make([]fixtureFile, 0, len(names))
	for _, name := range names {
		files = append(files, fixtureFile{name: "/" + name, body: entries[name]})
	}
	writeFilesZIPAt(t, filename, files, modified)
}

func writeFilesZIP(t *testing.T, filename string, files []fixtureFile) {
	t.Helper()
	writeFilesZIPAt(t, filename, files, time.Now())
}

func writeFilesZIPAt(t *testing.T, filename string, files []fixtureFile, modified time.Time) {
	t.Helper()
	output, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(output)
	for _, item := range files {
		header := &zip.FileHeader{Name: item.name, Method: zip.Deflate}
		header.SetModTime(modified)
		stream, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := stream.Write(item.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
}
