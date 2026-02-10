// This program generates pkg/rpmdb_testcase_test.go by running rpm queries
// inside Docker containers against committed testdata rpmdb files.
//
// Usage:
//
//	go run ./cmd/generate-testcases
package main

import (
	"bytes"
	"fmt"
	"go/format"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// listFixture defines a fixture that generates a []*PackageInfo list via rpm -qa.
type listFixture struct {
	varName            string   // Go variable name
	testdataDir        string   // directory under pkg/testdata/
	dbFile             string   // DB filename within testdataDir
	readerImage        string   // Docker image that can read this DB format
	hasModularitylabel bool     // use %{MODULARITYLABEL} instead of hardcoded ""
	comments           []string // comment lines above the fixture (documenting original image/setup)
}

// singlePkgFixture defines per-package InstalledFiles and InstalledFileNames data.
type singlePkgFixture struct {
	pkgName          string // rpm package name
	testdataDir      string
	dbFile           string
	readerImage      string
	filesVarName     string // e.g. CentOS5PythonInstalledFiles
	fileNamesVarName string // e.g. CentOS5PythonInstalledFileNames
	fileNamesComment string // comment above InstalledFileNames
	filesComment     string // comment above InstalledFiles
}

var listFixtures = []listFixture{
	{
		varName:     "CentOS5Plain",
		testdataDir: "centos5-plain",
		dbFile:      "Packages",
		readerImage: "centos:7",
		comments: []string{
			"docker run --rm -it centos:5 bash",
		},
	},
	{
		varName:     "CentOS6Plain",
		testdataDir: "centos6-plain",
		dbFile:      "Packages",
		readerImage: "centos:7",
		comments: []string{
			"docker run --rm -it centos:6 bash",
		},
	},
	{
		varName:     "CentOS6DevTools",
		testdataDir: "centos6-devtools",
		dbFile:      "Packages",
		readerImage: "centos:7",
		comments: []string{
			"docker run --rm -it centos:6 bash",
			`yum groupinstall -y "Development tools"`,
		},
	},
	{
		varName:     "CentOS6Many",
		testdataDir: "centos6-many",
		dbFile:      "Packages",
		readerImage: "centos:7",
		comments: []string{
			"docker run --rm -it centos:6 bash",
			`yum groupinstall -y "Development tools"`,
			`yum install -y rpm-build yum-utils rpmdevtools libffi-devel openssl-devel`,
			"rpmdev-setuptree",
			`yum install -y zlib-devel bzip2-devel ncurses-devel sqlite-devel readline-devel tk-devel gdbm-devel db4-devel libpcap-devel xz-devel expat-devel`,
		},
	},
	{
		varName:     "CentOS7Plain",
		testdataDir: "centos7-plain",
		dbFile:      "Packages",
		readerImage: "centos:7",
		comments: []string{
			"docker run --rm -it centos:7 bash",
		},
	},
	{
		varName:     "CentOS7DevTools",
		testdataDir: "centos7-devtools",
		dbFile:      "Packages",
		readerImage: "centos:7",
		comments: []string{
			"docker run --rm -it centos:7 bash",
			`yum groupinstall -y "Development tools"`,
		},
	},
	{
		varName:     "CentOS7Many",
		testdataDir: "centos7-many",
		dbFile:      "Packages",
		readerImage: "centos:7",
		comments: []string{
			"docker run --rm -it centos:7 bash",
			`yum groupinstall -y "Development tools"`,
			`yum install -y rpm-build yum-utils rpmdevtools libffi-devel openssl-devel`,
			"rpmdev-setuptree",
			`yum install -y zlib-devel bzip2-devel ncurses-devel sqlite-devel readline-devel tk-devel gdbm-devel db4-devel libpcap-devel xz-devel expat-devel`,
			`yum install -y net-tools bc`,
		},
	},
	{
		varName:     "CentOS7Httpd24",
		testdataDir: "centos7-httpd24",
		dbFile:      "Packages",
		readerImage: "centos:7",
		comments: []string{
			"docker run --rm -it centos/httpd-24-centos7 bash",
		},
	},
	{
		varName:     "CentOS7Python35",
		testdataDir: "centos7-python35",
		dbFile:      "Packages",
		readerImage: "centos:7",
		comments: []string{
			"docker run --rm -it centos/python-35-centos7 bash",
		},
	},
	{
		varName:            "CentOS8Modularitylabel",
		testdataDir:        "centos8-modularitylabel",
		dbFile:             "Packages",
		readerImage:        "rockylinux:9",
		hasModularitylabel: true,
		comments: []string{
			"docker run --rm -it centos:8 bash",
			"yum module install -y container-tools",
			`yum groupinstall -y "Development tools"`,
			`yum -y install nodejs podman-docker`,
		},
	},
	{
		varName:     "UBI8s390x",
		testdataDir: "ubi8-s390x",
		dbFile:      "Packages",
		readerImage: "centos:7",
		comments: []string{
			"docker run --platform s390x --rm -it registry.access.redhat.com/ubi8/ubi bash",
		},
	},
	{
		varName:     "SLE15WithNDB",
		testdataDir: "sle15-bci",
		dbFile:      "Packages.db",
		readerImage: "opensuse/leap:15.5",
		comments: []string{
			"docker run --rm -it registry.suse.com/bci/bci-minimal:15.3 bash",
		},
	},
	{
		varName:     "Fedora35WithSQLite3",
		testdataDir: "fedora35",
		dbFile:      "rpmdb.sqlite",
		readerImage: "rockylinux:9",
		comments: []string{
			"docker run --rm -it fedora:35 bash",
		},
	},
	{
		varName:     "Fedora35PlusMongoDBWithSQLite3",
		testdataDir: "fedora35-plus-mongo",
		dbFile:      "rpmdb.sqlite",
		readerImage: "rockylinux:9",
		comments: []string{
			"docker run --rm -it fedora:35 bash",
			"dnf -y install mongodb-cli",
		},
	},
	{
		varName:     "Rockylinux9WithSQLite3",
		testdataDir: "rockylinux-9",
		dbFile:      "rpmdb.sqlite",
		readerImage: "rockylinux:9",
		comments: []string{
			"docker run --rm -it rockylinux:9 bash",
		},
	},
}

var singlePkgFixtures = []singlePkgFixture{
	{
		pkgName:          "python",
		testdataDir:      "centos5-plain",
		dbFile:           "Packages",
		readerImage:      "centos:7",
		filesVarName:     "CentOS5PythonInstalledFiles",
		fileNamesVarName: "CentOS5PythonInstalledFileNames",
		fileNamesComment: "rpm -ql python --dbpath /path/to/testdata/centos5-plain | awk '{printf \"\\\"%s\\\",\\n\", $1}'",
		filesComment:     "",
	},
	{
		pkgName:          "glibc",
		testdataDir:      "centos6-plain",
		dbFile:           "Packages",
		readerImage:      "centos:7",
		filesVarName:     "CentOS6GlibcInstalledFiles",
		fileNamesVarName: "CentOS6GlibcInstalledFileNames",
		fileNamesComment: "rpm -ql glibc --dbpath /path/to/testdata/centos6-plain | awk '{printf \"\\\"%s\\\",\\n\", $1}'",
		filesComment:     "",
	},
	{
		pkgName:          "nodejs",
		testdataDir:      "centos8-modularitylabel",
		dbFile:           "Packages",
		readerImage:      "centos:7",
		filesVarName:     "CentOS8NodejsInstalledFiles",
		fileNamesVarName: "CentOS8NodejsInstalledFileNames",
		fileNamesComment: "",
		filesComment:     "",
	},
	{
		pkgName:          "curl",
		testdataDir:      "cbl-mariner-2.0",
		dbFile:           "rpmdb.sqlite",
		readerImage:      "rockylinux:9",
		filesVarName:     "Mariner2CurlInstalledFiles",
		fileNamesVarName: "Mariner2CurlInstalledFileNames",
		fileNamesComment: "",
		filesComment:     "",
	},
	{
		pkgName:          "libuuid",
		testdataDir:      "libuuid",
		dbFile:           "Packages",
		readerImage:      "centos:7",
		filesVarName:     "LibuuidInstalledFiles",
		fileNamesVarName: "LibuuidInstalledFileNames",
		fileNamesComment: "",
		filesComment:     "",
	},
	{
		pkgName:          "hostname",
		testdataDir:      "rockylinux-9",
		dbFile:           "rpmdb.sqlite",
		readerImage:      "rockylinux:9",
		filesVarName:     "Rockylinux9HostnameFiles",
		fileNamesVarName: "Rockylinux9HostnameFileNames",
		fileNamesComment: "",
		filesComment:     "",
	},
}

func main() {
	log.SetFlags(0)

	// Find project root by looking for go.mod
	projectRoot = findProjectRoot()
	log.Printf("Project root: %s", projectRoot)

	var buf bytes.Buffer

	// Static header
	buf.WriteString(`package rpmdb

func intRef(i ...int) *int {
	if len(i) == 0 {
		return nil
	}
	return &i[0]
}

type commonPackageInfo struct {
	Epoch           *int
	Name            string
	Version         string
	Release         string
	Arch            string
	SourceRpm       string
	Size            int
	License         string
	Vendor          string
	Modularitylabel string
	Summary         string
	SigMD5          string
}

func toPackageInfo(pkgs []*commonPackageInfo) []*PackageInfo {
	pkgList := make([]*PackageInfo, 0, len(pkgs))
	for _, p := range pkgs {
		pkgList = append(pkgList, &PackageInfo{
			Epoch:           p.Epoch,
			Name:            p.Name,
			Version:         p.Version,
			Release:         p.Release,
			Arch:            p.Arch,
			SourceRpm:       p.SourceRpm,
			Size:            p.Size,
			License:         p.License,
			Vendor:          p.Vendor,
			Modularitylabel: p.Modularitylabel,
			Summary:         p.Summary,
			SigMD5:          p.SigMD5,
		})
	}

	return pkgList
}

var (
`)

	// Generate list fixtures
	for i, f := range listFixtures {
		log.Printf("Generating list fixture %d/%d: %s", i+1, len(listFixtures), f.varName)

		lines, err := generateListFixture(f)
		if err != nil {
			log.Fatalf("Failed to generate %s: %v", f.varName, err)
		}

		// Write comments
		for _, c := range f.comments {
			fmt.Fprintf(&buf, "\t// %s\n", c)
		}
		// Write the rpm queryformat comment (matches the original style)
		if f.hasModularitylabel {
			fmt.Fprintf(&buf, "\t// rpm -qa --queryformat \"\\{%%{EPOCH}, \\\"%%{NAME}\\\", \\\"%%{VERSION}\\\", \\\"%%{RELEASE}\\\", \\\"%%{ARCH}\\\", \\\"%%{SOURCERPM}\\\", %%{SIZE}, \\\"%%{LICENSE}\\\", \\\"%%{VENDOR}\\\", \\\"%%{MODULARITYLABEL}\\\", \\\"%%{SUMMARY}\\\", \\\"%%{SIGMD5}\\\"\\},\\n\" | sed \"s/^{(none)/{intRef()/g\" | sed -r 's/^\\{([0-9]+),/{intRef(\\1),/' | sed 's/\"(none)\"/\"\"/g' | sed \"s/(none)/0/g\"\n")
		} else {
			fmt.Fprintf(&buf, "\t// rpm -qa --queryformat \"\\{%%{EPOCH}, \\\"%%{NAME}\\\", \\\"%%{VERSION}\\\", \\\"%%{RELEASE}\\\", \\\"%%{ARCH}\\\", \\\"%%{SOURCERPM}\\\", %%{SIZE}, \\\"%%{LICENSE}\\\", \\\"%%{VENDOR}\\\", \\\"\\\", \\\"%%{SUMMARY}\\\", \\\"%%{SIGMD5}\\\"\\},\\n\" | sed \"s/^{(none)/{intRef()/g\" | sed -r 's/^\\{([0-9]+),/{intRef(\\1),/' | sed \"s/(none)/0/g\"\n")
		}

		fmt.Fprintf(&buf, "\t%s = func() []*PackageInfo {\n", f.varName)
		buf.WriteString("\t\tpkgs := []*commonPackageInfo{\n")
		for _, line := range lines {
			fmt.Fprintf(&buf, "\t\t\t%s\n", line)
		}
		buf.WriteString("\t\t}\n\n")
		buf.WriteString("\t\treturn toPackageInfo(pkgs)\n")
		buf.WriteString("\t}\n\n")
	}

	// Generate single-package InstalledFileNames
	for i, f := range singlePkgFixtures {
		log.Printf("Generating InstalledFileNames %d/%d: %s", i+1, len(singlePkgFixtures), f.fileNamesVarName)

		names, err := generateInstalledFileNames(f)
		if err != nil {
			log.Fatalf("Failed to generate %s: %v", f.fileNamesVarName, err)
		}

		if f.fileNamesComment != "" {
			fmt.Fprintf(&buf, "\t// %s\n", f.fileNamesComment)
		}
		fmt.Fprintf(&buf, "\t%s = []string{\n", f.fileNamesVarName)
		for _, name := range names {
			fmt.Fprintf(&buf, "\t\t%q,\n", name)
		}
		buf.WriteString("\t}\n\n")
	}

	// Generate single-package InstalledFiles
	for i, f := range singlePkgFixtures {
		log.Printf("Generating InstalledFiles %d/%d: %s", i+1, len(singlePkgFixtures), f.filesVarName)

		files, err := generateInstalledFiles(f)
		if err != nil {
			log.Fatalf("Failed to generate %s: %v", f.filesVarName, err)
		}

		fmt.Fprintf(&buf, "\t%s = []FileInfo{\n", f.filesVarName)
		for _, line := range files {
			fmt.Fprintf(&buf, "\t\t%s\n", line)
		}
		buf.WriteString("\t}\n\n")
	}

	// Close var block
	buf.WriteString(")\n")

	// Format with gofmt
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		// Write unformatted for debugging
		badPath := filepath.Join(projectRoot, "pkg", "rpmdb_testcase_test.go.bad")
		os.WriteFile(badPath, buf.Bytes(), 0644)
		log.Fatalf("gofmt failed (unformatted output written to %s): %v", badPath, err)
	}

	outPath := filepath.Join(projectRoot, "pkg", "rpmdb_testcase_test.go")
	err = os.WriteFile(outPath, formatted, 0644)
	if err != nil {
		log.Fatalf("Failed to write output: %v", err)
	}

	log.Printf("Wrote %s (%d bytes)", outPath, len(formatted))
}

// findProjectRoot walks up from the current directory looking for go.mod.
func findProjectRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		log.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			log.Fatal("could not find project root (no go.mod found)")
		}
		dir = parent
	}
}

// projectRoot is set by main() before any generation starts.
var projectRoot string

// runDocker executes an rpm command inside a Docker container with the testdata mounted.
func runDocker(readerImage, testdataDir, script string) (string, error) {
	srcPath := filepath.Join(projectRoot, "pkg", "testdata", testdataDir)

	cmd := exec.Command("docker", "run", "--rm",
		"-e", "LANG=C.UTF-8",
		"-v", fmt.Sprintf("%s:/src:ro", srcPath),
		readerImage,
		"bash", "-c", script,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("docker run failed: %v\nstderr: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

// generateListFixture queries rpm with tab-separated output and formats as Go struct literals.
func generateListFixture(f listFixture) ([]string, error) {
	// Use tab-separated fields to avoid issues with embedded quotes in field values.
	// Fields: EPOCH, NAME, VERSION, RELEASE, ARCH, SOURCERPM, SIZE, LICENSE, VENDOR, MODULARITYLABEL, SUMMARY, SIGMD5
	var qf string
	if f.hasModularitylabel {
		qf = `%{EPOCH}\t%{NAME}\t%{VERSION}\t%{RELEASE}\t%{ARCH}\t%{SOURCERPM}\t%{SIZE}\t%{LICENSE}\t%{VENDOR}\t%{MODULARITYLABEL}\t%{SUMMARY}\t%{SIGMD5}\n`
	} else {
		qf = `%{EPOCH}\t%{NAME}\t%{VERSION}\t%{RELEASE}\t%{ARCH}\t%{SOURCERPM}\t%{SIZE}\t%{LICENSE}\t%{VENDOR}\t\t%{SUMMARY}\t%{SIGMD5}\n`
	}

	script := fmt.Sprintf(
		`mkdir -p /rpmdb && cp /src/* /rpmdb/ && rpm -qa --dbpath /rpmdb --qf '%s'`,
		qf,
	)

	output, err := runDocker(f.readerImage, f.testdataDir, script)
	if err != nil {
		return nil, err
	}

	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fields := strings.SplitN(line, "\t", 12)
		if len(fields) != 12 {
			return nil, fmt.Errorf("expected 12 tab-separated fields, got %d: %q", len(fields), line)
		}

		epoch := fields[0]
		name := fields[1]
		version := fields[2]
		release := fields[3]
		arch := fields[4]
		sourceRpm := fields[5]
		size := fields[6]
		license := fields[7]
		vendor := fields[8]
		modularitylabel := fields[9]
		summary := fields[10]
		sigMD5 := fields[11]

		// Format epoch
		var epochStr string
		if epoch == "(none)" {
			epochStr = "intRef()"
		} else {
			epochStr = fmt.Sprintf("intRef(%s)", epoch)
		}

		// Handle (none) values: numeric fields become 0, string fields become ""
		if size == "(none)" {
			size = "0"
		}
		noneToEmpty := func(s string) string {
			if s == "(none)" {
				return ""
			}
			return s
		}
		arch = noneToEmpty(arch)
		sourceRpm = noneToEmpty(sourceRpm)
		license = noneToEmpty(license)
		vendor = noneToEmpty(vendor)
		summary = noneToEmpty(summary)
		sigMD5 = noneToEmpty(sigMD5)
		modularitylabel = noneToEmpty(modularitylabel)

		goLine := fmt.Sprintf(
			`{%s, %q, %q, %q, %q, %q, %s, %q, %q, %q, %q, %q},`,
			epochStr, name, version, release, arch, sourceRpm, size, license, vendor, modularitylabel, summary, sigMD5,
		)
		lines = append(lines, goLine)
	}

	if len(lines) == 0 {
		return nil, fmt.Errorf("no output from rpm for %s", f.varName)
	}

	return lines, nil
}

// needsRebuildDB returns true if the database format requires index rebuild for name-based queries.
func needsRebuildDB(dbFile string) bool {
	return dbFile == "Packages" || dbFile == "Packages.db"
}

// generateInstalledFileNames runs rpm -ql to get file names for a package.
func generateInstalledFileNames(f singlePkgFixture) ([]string, error) {
	rebuildCmd := ""
	if needsRebuildDB(f.dbFile) {
		rebuildCmd = "rpm --rebuilddb --dbpath /rpmdb && "
	}
	script := fmt.Sprintf(
		`mkdir -p /rpmdb && cp /src/* /rpmdb/ && %srpm -ql %s --dbpath /rpmdb`,
		rebuildCmd, f.pkgName,
	)

	output, err := runDocker(f.readerImage, f.testdataDir, script)
	if err != nil {
		return nil, err
	}

	var names []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		names = append(names, line)
	}

	return names, nil
}

// generateInstalledFiles runs rpm query to get detailed file info for a package.
func generateInstalledFiles(f singlePkgFixture) ([]string, error) {
	rebuildCmd := ""
	if needsRebuildDB(f.dbFile) {
		rebuildCmd = "rpm --rebuilddb --dbpath /rpmdb && "
	}
	// Query all file metadata in a single rpm call, tab-separated
	script := fmt.Sprintf(
		`mkdir -p /rpmdb && cp /src/* /rpmdb/ && %srpm -q %s --dbpath /rpmdb --qf '[%%{FILENAMES}\t%%{FILESIZES}\t%%{FILEDIGESTS}\t%%{FILEMODES}\t%%{FILEFLAGS}\t%%{FILEUSERNAME}\t%%{FILEGROUPNAME}\n]'`,
		rebuildCmd, f.pkgName,
	)

	output, err := runDocker(f.readerImage, f.testdataDir, script)
	if err != nil {
		return nil, err
	}

	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fields := strings.SplitN(line, "\t", 7)
		if len(fields) != 7 {
			return nil, fmt.Errorf("expected 7 tab-separated fields, got %d: %q", len(fields), line)
		}

		path := fields[0]
		size, err := strconv.ParseInt(fields[1], 10, 32)
		if err != nil {
			return nil, fmt.Errorf("bad size %q for %s: %v", fields[1], path, err)
		}
		digest := fields[2]
		mode, err := strconv.ParseUint(fields[3], 10, 16)
		if err != nil {
			return nil, fmt.Errorf("bad mode %q for %s: %v", fields[3], path, err)
		}
		flags, err := strconv.ParseInt(fields[4], 10, 32)
		if err != nil {
			return nil, fmt.Errorf("bad flags %q for %s: %v", fields[4], path, err)
		}
		username := fields[5]
		groupname := fields[6]

		goLine := fmt.Sprintf(
			`{Path: %q, Mode: 0x%x, Digest: %q, Size: %d, Username: %q, Groupname: %q, Flags: %d},`,
			path, mode, digest, size, username, groupname, flags,
		)
		lines = append(lines, goLine)
	}

	return lines, nil
}
