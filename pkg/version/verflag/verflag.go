package verflag

import (
	"fmt"
	"os"
	"strconv"

	flag "github.com/spf13/pflag"

	"github.com/clusterpedia-io/clusterpedia/pkg/version"
)

// copy from k8s.io/component-base/version/verflag/verflag.go

type versionValue int

const (
	VersionFalse versionValue = 0
	VersionTrue  versionValue = 1
	VersionRaw   versionValue = 2
)

const strRawVersion string = "raw"

func (v *versionValue) IsBoolFlag() bool {
	return true
}

func (v *versionValue) Get() interface{} {
	return versionValue(*v)
}

func (v *versionValue) Set(s string) error {
	if s == strRawVersion {
		*v = VersionRaw
		return nil
	}
	boolVal, err := strconv.ParseBool(s)
	if boolVal {
		*v = VersionTrue
	} else {
		*v = VersionFalse
	}
	return err
}

func (v *versionValue) String() string {
	if *v == VersionRaw {
		return strRawVersion
	}
	return fmt.Sprintf("%v", bool(*v == VersionTrue))
}

// The type of the flag as required by the pflag.Value interface
func (v *versionValue) Type() string {
	return "version"
}

func VersionVar(p *versionValue, name string, value versionValue, usage string) {
	*p = value
	flag.Var(p, name, usage)
	// "--version" will be treated as "--version=true"
	flag.Lookup(name).NoOptDefVal = "true"
}

func Version(name string, value versionValue, usage string) *versionValue {
	p := new(versionValue)
	VersionVar(p, name, value, usage)
	return p
}

const versionFlagName = "version"

var (
	programName = "Clusterpedia"
	versionFlag = Version(versionFlagName, VersionFalse, "Print version information and quit")
)

func AddFlags(fs *flag.FlagSet) {
	fs.AddFlag(flag.Lookup(versionFlagName))
}

func PrintAndExitIfRequested() {
	switch *versionFlag {
	case VersionRaw:
		fmt.Printf("%#v\n", version.Get())
	case VersionTrue:
		fmt.Printf("%s %s\n", programName, version.Get())
	default:
		return
	}

	os.Exit(0)
}
