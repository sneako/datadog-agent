package checks

import (
	"fmt"

	"github.com/DataDog/datadog-agent/pkg/check"
	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("datadog-agent")

// Catalog keeps track of Go checks by name
var catalog = make(map[string]check.Check)

// RegisterCheck adds a check to the catalog
func RegisterCheck(name string, c check.Check) {
	catalog[name] = c
}

// GoCheckLoader is a specific loader for checks living in this package
type GoCheckLoader struct {
}

// NewGoCheckLoader creates a loader for go checks
// for the time being it does basically nothing
func NewGoCheckLoader() *GoCheckLoader {
	return &GoCheckLoader{}
}

// Load returns a list of checks, one for every configuration instance found in `config`
func (gl *GoCheckLoader) Load(config check.Config) ([]check.Check, error) {
	checks := []check.Check{}

	c, found := catalog[config.Name]
	if !found {
		msg := fmt.Sprintf("Check %s not found in Catalog", config.Name)
		log.Warning(msg)
		return checks, fmt.Errorf(msg)
	}

	for _, instance := range config.Instances {
		newCheck := c
		newCheck.Configure(instance)
		checks = append(checks, newCheck)
	}

	return checks, nil
}
