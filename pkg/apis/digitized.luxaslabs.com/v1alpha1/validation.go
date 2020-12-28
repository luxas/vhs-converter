package v1alpha1

import (
	"strconv"
	"strings"

	"github.com/fluxcd/go-git-providers/validation"
	utilvalidation "k8s.io/apimachinery/pkg/util/validation"
)

func (p *Project) Validate() error {
	v := validation.New("Recorder")
	if len(utilvalidation.IsDNS1123Label(p.Name)) != 0 {
		v.Invalid(p.Name, "metadata", "name")
	}
	return v.Error()
}

var knownRecorderActions = map[RecorderAction]struct{}{
	RecorderActionNone:    {},
	RecorderActionPreview: {},
	RecorderActionRecord:  {},
}

func isValidRecorderAction(ra RecorderAction) (ok bool) {
	_, ok = knownRecorderActions[ra]
	return
}

func (r *Recorder) Validate() error {
	v := validation.New("Recorder")
	if !isValidRecorderAction(r.Spec.Action) {
		v.Invalid(r.Spec.Action, "spec", "action")
	}
	if r.Spec.Cassette != "" && !isValidCassetteName(r.Spec.Cassette) {
		v.Invalid(r.Spec.Cassette, "spec", "cassette")
	}
	if (r.Spec.Action == RecorderActionPreview || r.Spec.Action == RecorderActionRecord) && r.Spec.Cassette == "" {
		v.Required("spec", "cassette")
	}
	return v.Error()
}

var knownCassetteTypes = map[CassetteType]struct{}{
	CassetteTypeVideo8: {},
	CassetteTypeHi8:    {},
	CassetteTypeVHS:    {},
	CassetteTypeVHSC:   {},
}

func isValidCassetteType(ct CassetteType) (ok bool) {
	_, ok = knownCassetteTypes[ct]
	return
}

func isValidCassetteName(idx string) bool {
	if len(idx) != 3 {
		return false
	}
	_, err := strconv.ParseUint(idx, 10, 16)
	return err == nil
}

func isValidClipName(name string) bool {
	parts := strings.Split(name, "-")
	return len(parts) == 2 && isValidCassetteName(parts[0]) && isValidCassetteName(parts[1])
}

/*func mutuallyExclusive(v validation.Validator, name string, f1, f2 []string) {
	// TODO: Add in a StructName() call to Validator
	f1Path := strings.Join(append([]string{name}, f1...), ".")
	f2Path := strings.Join(append([]string{name}, f1...), ".")
	v.Append(fmt.Errorf("Fields %s and %s are mutually exclusive", f1Path, f2Path), true, f1...)
}*/

func (c *Cassette) Validate() error {
	structName := "Cassette"
	v := validation.New(structName)
	if !isValidCassetteName(c.Name) {
		v.Invalid(c.Name, "metadata", "name")
	}
	// Namespace matching a Project is implemented as a hook
	if c.Spec.Description == "" {
		v.Required("spec", "description")
	}
	if c.Spec.MaxPlayTime == 0 {
		v.Required("spec", "maxPlayTime")
	}
	if !isValidCassetteType(c.Spec.Type) {
		v.Invalid(c.Spec.Type, "spec", "type")
	}
	// Validating that only one of these are set
	/*if c.Spec.ShouldPreview && c.Spec.ShouldRecord {
		mutuallyExclusive(v, structName, []string{"spec", "shouldPreview"}, []string{"spec", "shouldRecord"})
	}
	if c.Spec.ShouldPreview && c.Spec.ShouldMerge {
		mutuallyExclusive(v, structName, []string{"spec", "shouldPreview"}, []string{"spec", "shouldMerge"})
	}
	if c.Spec.ShouldRecord && c.Spec.ShouldMerge {
		mutuallyExclusive(v, structName, []string{"spec", "shouldRecord"}, []string{"spec", "shouldMerge"})
	}*/
	return v.Error()
}

func (c *Clip) Validate() error {
	v := validation.New("Clip")
	// Namespace matching a Project is implemented as a hook
	if !isValidClipName(c.Name) {
		v.Invalid(c.Name, "metadata", "name")
	}
	return v.Error()
}

// TODO: Add validation for MergeCassetteAction
