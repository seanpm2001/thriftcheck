// Copyright 2021 Pinterest
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/kkyr/fig"
	"github.com/pinterest/thriftcheck"
	"github.com/pinterest/thriftcheck/checks"
	"rsc.io/getopt"
)

// Config represents all of the configurable values.
type Config struct {
	Includes []string `fig:"includes"`
	Checks   struct {
		Enabled  []string `fig:"enabled"`
		Disabled []string `fix:"disabled"`

		Enum struct {
			Size struct {
				Warning int `fig:"warning"`
				Error   int `fig:"error"`
			}
		}

		Include struct {
			Restricted map[string]string `fig:"restricted"`
		}

		Namespace struct {
			Patterns map[string]string `fig:"patterns"`
		}
	}
}

// Includes accumlates include path strings for a repeated command line flag.
type Includes []string

func (i *Includes) String() string {
	return strings.Join(*i, " ")
}

// Set adds a new value using a flag.Var-compatible interface.
func (i *Includes) Set(value string) error {
	*i = append(*i, value)
	return nil
}

var (
	version       = "dev"
	revision      = "dev"
	includes      Includes
	configFile    = flag.String("c", "thriftcheck.toml", "configuration file path")
	helpFlag      = flag.Bool("h", false, "show command help")
	listFlag      = flag.Bool("l", false, "list all available checks and exit")
	stdinFilename = flag.String("stdin-filename", "stdin", "filename used when piping from stdin")
	verboseFlag   = flag.Bool("v", false, "enable verbose (debugging) output")
	versionFlag   = flag.Bool("version", false, "print the version and exit")
)

func init() {
	flag.Var(&includes, "I", "include path (can be specified multiple times)")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: thriftcheck [options] [file ...]\n")
		getopt.PrintDefaults()
	}
	getopt.Aliases(
		"I", "include",
		"c", "config",
		"h", "help",
		"l", "list",
		"v", "verbose")
}

func isFlagSet(name string) bool {
	set := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

func loadConfig(cfg *Config) error {
	if err := fig.Load(cfg, fig.File(*configFile)); err != nil {
		// Ignore FileNotFound when we're using the default configuration file.
		if errors.Is(err, fig.ErrFileNotFound) && !isFlagSet("c") {
			return nil
		}
		return err
	}
	return nil
}

func lint(l *thriftcheck.Linter, filenames []string) (thriftcheck.Messages, error) {
	if len(filenames) == 1 && filenames[0] == "-" {
		return l.Lint(os.Stdin, *stdinFilename)
	}
	return l.LintFiles(filenames)
}

func main() {
	// Parse command line flags
	getopt.Parse()
	if *helpFlag {
		flag.Usage()
		os.Exit(0)
	}
	if *versionFlag {
		fmt.Fprintf(flag.CommandLine.Output(), "thriftcheck %s (%s)\n", version, revision)
		os.Exit(0)
	}

	// Load the (optional) configuration file
	var cfg Config
	if err := loadConfig(&cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1 << uint(thriftcheck.Error))
	}
	if len(includes) > 0 {
		cfg.Includes = includes
	}

	// Build the set of checks we'll use for the linter
	checks := &thriftcheck.Checks{
		checks.CheckEnumSize(cfg.Checks.Enum.Size.Warning, cfg.Checks.Enum.Size.Error),
		checks.CheckIncludeExists(),
		checks.CheckIncludeRestricted(cfg.Checks.Include.Restricted),
		checks.CheckMapKeyType(),
		checks.CheckNamespacePattern(cfg.Checks.Namespace.Patterns),
		checks.CheckSetValueType(),
	}
	if *listFlag {
		fmt.Println(strings.Join(checks.SortedNames(), "\n"))
		os.Exit(0)
	}
	if len(cfg.Checks.Disabled) > 0 {
		checks = checks.Without(cfg.Checks.Disabled)
	}
	if len(cfg.Checks.Enabled) > 0 {
		checks = checks.With(cfg.Checks.Enabled)
	}
	if len(flag.Args()) == 0 {
		flag.Usage()
		os.Exit(0)
	}

	// Build the set of linter options
	options := []thriftcheck.Option{
		thriftcheck.WithIncludes(cfg.Includes),
	}
	if *verboseFlag {
		logger := log.New(os.Stderr, "", log.Ltime|log.Lmicroseconds|log.Lshortfile)
		options = append(options, thriftcheck.WithLogger(logger))
	}

	// Create the linter and run it over the input files
	linter := thriftcheck.NewLinter(*checks, options...)
	messages, err := lint(linter, flag.Args())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1 << uint(thriftcheck.Error))
	}

	// Print any messages reported by the linter
	status := 0
	for _, m := range messages {
		fmt.Println(m)
		status |= 1 << uint(m.Severity)
	}
	os.Exit(status)
}
