package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/pflag"
	"go.yaml.in/yaml/v3"
)

// ErrHelp is returned by Load when --help was requested.
var ErrHelp = pflag.ErrHelp

type Config struct {
	// Inputs holds the files, directories or patterns to process: those
	// given with --input followed by the positional arguments or, when the
	// command line names none, the "input" config key.
	Inputs []string `yaml:"-"`

	OutputDir    string    `yaml:"output"`
	DeleteOrig   bool      `yaml:"delete_orig"`
	RenameOrig   bool      `yaml:"rename_orig"`
	ExtractPhoto bool      `yaml:"extract_photo"`
	ExtractVideo bool      `yaml:"extract_video"`
	Force        bool      `yaml:"force"`
	Log          LogConfig `yaml:"log"`

	// Recover is set when --recover was passed: what an interrupted run left
	// behind is cleaned up before anything is extracted.
	Recover bool `yaml:"-"`

	// ShowVersion is set when --version was passed.
	ShowVersion bool `yaml:"-"`

	// ConfigFile is the config file that was read, empty if there was none.
	ConfigFile string `yaml:"-"`

	// IgnoredConfigFiles are config files found in the default locations in
	// a format that is not read.
	IgnoredConfigFiles []string `yaml:"-"`
}

type LogConfig struct {
	File  string `yaml:"file"`
	Level string `yaml:"level"`
	// Format is one of auto, pretty, text, json.
	Format    string `yaml:"format"`
	NoConsole bool   `yaml:"no_console"`
}

const (
	appName   = "go-motion-photo"
	envPrefix = "GO_MOTION_PHOTO"
)

var (
	// configExts are the extensions of the config files that are read.
	configExts = []string{".yaml", ".yml", ".json"}

	// legacyExts are formats that earlier versions read. A config file in
	// one of them is pointed out rather than silently left unread.
	legacyExts = []string{".toml", ".hcl", ".tfvars", ".ini", ".properties", ".props", ".prop", ".env", ".dotenv"}
)

// inputKey is the config key of the input. Unlike the other keys it is not
// set by its flag, which may be given several times: see Config.Inputs.
const inputKey = "input"

// flagKeys maps each CLI flag to the config key it sets. The key also names
// the environment variable: log.level is read from GO_MOTION_PHOTO_LOG_LEVEL.
var flagKeys = map[string]string{
	"output":         "output",
	"delete-orig":    "delete_orig",
	"rename-orig":    "rename_orig",
	"extract-photo":  "extract_photo",
	"extract-video":  "extract_video",
	"force":          "force",
	"log-file":       "log.file",
	"log-level":      "log.level",
	"log-format":     "log.format",
	"no-console-log": "log.no_console",
}

// Load builds the configuration from args (without the program name), the
// environment and the config file. Precedence, highest first: CLI flags,
// environment variables, config file, defaults.
func Load(args []string) (*Config, error) {
	var file struct {
		Input  string `yaml:"input"`
		Config `yaml:",inline"`
	}
	cfg := &file.Config

	flags := pflag.NewFlagSet(appName, pflag.ContinueOnError)
	flags.Usage = func() {}
	inputs := flags.StringArrayP("input", "i", nil, "Input motion photo file or directory path (*.jpg, *.jpeg, *.heic)")
	flags.StringVarP(&cfg.OutputDir, "output", "o", ".", "Directory to save extracted files")
	flags.BoolVar(&cfg.DeleteOrig, "delete-orig", false, "Delete original file after successful extraction")
	flags.BoolVar(&cfg.RenameOrig, "rename-orig", false, "Rename original file and don't append _photo/_video to extracted files")
	flags.BoolVar(&cfg.ExtractPhoto, "extract-photo", true, "Extract photo part")
	flags.BoolVar(&cfg.ExtractVideo, "extract-video", true, "Extract video part")
	flags.BoolVarP(&cfg.Force, "force", "f", false, "Force overwrite existing files")
	flags.StringVar(&cfg.Log.File, "log-file", "", "Log to file")
	flags.StringVar(&cfg.Log.Level, "log-level", "info", "Log level (debug, info, warn, error)")
	flags.StringVar(&cfg.Log.Format, "log-format", "auto", "Console output format (auto, pretty, text, json)")
	flags.BoolVar(&cfg.Log.NoConsole, "no-console-log", false, "Disable console logging")
	flags.BoolVar(&cfg.Recover, "recover", false, "Clean up what an interrupted run left behind")
	flags.BoolVarP(&cfg.ShowVersion, "version", "V", false, "Print version and exit")
	verbose := flags.BoolP("verbose", "v", false, "Same as --log-level debug")
	quiet := flags.BoolP("quiet", "q", false, "Same as --log-level warn")
	configFile := flags.String("config", "", "Config file path (optional)")

	if err := flags.Parse(args); err != nil {
		return nil, withSuggestion(err, flags)
	}

	// The version is printed whatever the rest of the configuration is, so a
	// config file that cannot be read does not get in the way.
	if cfg.ShowVersion {
		return cfg, nil
	}

	// cfg now holds the defaults overlaid with the command line. The config
	// file and the environment rank in between, so they are applied on top
	// and the command line values are then put back.
	fromCLI := make(map[string]string)
	flags.Visit(func(f *pflag.Flag) {
		if _, ok := flagKeys[f.Name]; ok {
			fromCLI[f.Name] = f.Value.String()
		}
	})

	data, path, ignored, err := readConfigFile(*configFile)
	cfg.ConfigFile = path
	if err == nil {
		err = decodeConfig(path, data, &file)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	cfg.IgnoredConfigFiles = ignored

	if value := os.Getenv(envName(inputKey)); value != "" {
		file.Input = value
	}
	for flagName, key := range flagKeys {
		name := envName(key)
		value := os.Getenv(name)
		// A variable that the command line overrides is not read, so that it
		// cannot be what is wrong with the invocation.
		if _, overridden := fromCLI[flagName]; value == "" || overridden {
			continue
		}
		if err := flags.Set(flagName, value); err != nil {
			return nil, fmt.Errorf("invalid %s: %w", name, err)
		}
	}

	for flagName, value := range fromCLI {
		if err := flags.Set(flagName, value); err != nil {
			return nil, err
		}
	}

	switch {
	case *verbose && *quiet:
		return nil, errors.New("--verbose and --quiet cannot be used together")
	case *verbose:
		cfg.Log.Level = "debug"
	case *quiet:
		cfg.Log.Level = "warn"
	}

	switch {
	case len(*inputs) > 0:
		cfg.Inputs = append(*inputs, flags.Args()...)
	case flags.NArg() > 0:
		cfg.Inputs = flags.Args()
	case file.Input != "":
		cfg.Inputs = []string{file.Input}
	}

	return cfg, nil
}

// readConfigFile returns the contents and location of the config file at
// path, or of the first one found in the default locations when path is
// empty. Not finding one in the default locations is not an error; files
// found there in a format that is not read are returned in ignored.
func readConfigFile(path string) (data []byte, location string, ignored []string, err error) {
	if path != "" {
		if ext := filepath.Ext(path); !slices.Contains(configExts, strings.ToLower(ext)) {
			return nil, path, nil, fmt.Errorf("unsupported config format %q: use a .yaml, .yml or .json file", ext)
		}
		data, err := os.ReadFile(path)
		return data, path, nil, err
	}

	dirs := []string{"."}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".config", appName))
	}

	for _, dir := range dirs {
		for _, ext := range configExts {
			path := filepath.Join(dir, appName+ext)
			data, err := os.ReadFile(path)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return data, path, ignored, err
		}
		for _, ext := range legacyExts {
			path := filepath.Join(dir, appName+ext)
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				ignored = append(ignored, path)
			}
		}
	}

	return nil, "", ignored, nil
}

// decodeConfig parses data, the contents of the config file at path, into v.
func decodeConfig(path string, data []byte, v any) error {
	if strings.EqualFold(filepath.Ext(path), ".json") {
		// The YAML parser reads most JSON, but not all of its string escapes
		// (a path written as "\/out", for one). JSON is therefore parsed as
		// such and handed over as YAML, which the config keys are declared for.
		var doc any
		if err := json.Unmarshal(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")), &doc); err != nil {
			return err
		}
		var err error
		if data, err = yaml.Marshal(doc); err != nil {
			return err
		}
	}
	// Decoding comes first: it turns down what the keys could not be checked
	// in, anchors that contain themselves for one.
	if err := yaml.Unmarshal(data, v); err != nil {
		return err
	}
	return checkConfigKeys(data)
}

// checkConfigKeys returns an error for a key in the config file data that is
// not a config key, such as a misspelled one, which would otherwise be left
// unread without a word.
func checkConfigKeys(data []byte) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil || len(doc.Content) == 0 {
		// Not a document with keys to check.
		return nil
	}

	known := []string{inputKey}
	for _, key := range flagKeys {
		known = append(known, key)
	}
	slices.Sort(known)

	return checkConfigMapping(doc.Content[0], "", known, make(map[configMapping]bool))
}

// configMapping is a mapping of the config file as found under a key prefix.
type configMapping struct {
	node   *yaml.Node
	prefix string
}

// checkConfigMapping checks the keys of node, the mapping under the key
// prefix, against the dotted keys in known. checked holds the mappings that
// need no checking again: aliases may lead to one any number of times.
func checkConfigMapping(node *yaml.Node, prefix string, known []string, checked map[configMapping]bool) error {
	node = resolveAlias(node)
	if node.Kind != yaml.MappingNode {
		// Decoding reported what is there in place of a mapping.
		return nil
	}
	if checked[configMapping{node, prefix}] {
		return nil
	}
	checked[configMapping{node, prefix}] = true

	// Keys and values alternate in the content of a mapping.
	for i := 0; i+1 < len(node.Content); i += 2 {
		name, value := resolveAlias(node.Content[i]), resolveAlias(node.Content[i+1])

		// A merge key brings in the keys of one mapping or of several.
		if name.Tag == "!!merge" {
			merged := []*yaml.Node{value}
			if value.Kind == yaml.SequenceNode {
				merged = value.Content
			}
			for _, mapping := range merged {
				if err := checkConfigMapping(mapping, prefix, known, checked); err != nil {
					return err
				}
			}
			continue
		}

		key := prefix + name.Value

		// A key written with a dot is not one of a section: only log.level
		// spelled as level under log is read.
		isSection := !strings.Contains(name.Value, ".") &&
			slices.ContainsFunc(known, func(k string) bool { return strings.HasPrefix(k, key+".") })
		if isSection {
			if err := checkConfigMapping(value, key+".", known, checked); err != nil {
				return err
			}
			continue
		}
		if slices.Contains(known, key) && !strings.Contains(name.Value, ".") {
			continue
		}

		if section, option, ok := strings.Cut(name.Value, "."); ok && slices.Contains(known, key) {
			return fmt.Errorf("unknown key %q, write it as %q under %q", key, option, section)
		}
		best, bestDistance := "", 3
		for _, candidate := range known {
			if d := editDistance(key, candidate); d < bestDistance {
				best, bestDistance = candidate, d
			}
		}
		if best == "" {
			return fmt.Errorf("unknown key %q", key)
		}
		return fmt.Errorf("unknown key %q, did you mean %q?", key, best)
	}
	return nil
}

// resolveAlias returns the node that node stands for, if it is an alias.
func resolveAlias(node *yaml.Node) *yaml.Node {
	if node.Kind == yaml.AliasNode && node.Alias != nil {
		return node.Alias
	}
	return node
}

// withSuggestion adds the closest flag name to an unknown flag error.
func withSuggestion(err error, flags *pflag.FlagSet) error {
	var unknown *pflag.NotExistError
	if !errors.As(err, &unknown) || unknown.GetSpecifiedShortnames() != "" {
		return err
	}

	name := unknown.GetSpecifiedName()
	best, bestDistance := "", 3
	flags.VisitAll(func(f *pflag.Flag) {
		if d := editDistance(name, f.Name); d < bestDistance {
			best, bestDistance = f.Name, d
		}
	})
	if best == "" {
		return err
	}
	return fmt.Errorf("%w, did you mean --%s?", err, best)
}

// editDistance is the Levenshtein distance between a and b.
func editDistance(a, b string) int {
	row := make([]int, len(b)+1)
	for j := range row {
		row[j] = j
	}
	for i := 1; i <= len(a); i++ {
		diagonal := row[0]
		row[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			diagonal, row[j] = row[j], min(row[j]+1, row[j-1]+1, diagonal+cost)
		}
	}
	return row[len(b)]
}

func envName(key string) string {
	return envPrefix + "_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
}
